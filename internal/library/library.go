// Package library is cratecli's local memory: every dig you run, which
// records you've already seen, and (later) what you've saved and exported.
//
// It is a single SQLite file next to the config. Records are identified by
// crate.Record.Key (artist|title), with the Discogs release ID as a second
// way to recognise them.
package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, registers "sqlite"

	"github.com/williamdunstanmorris/cratecli/internal/config"
	"github.com/williamdunstanmorris/cratecli/internal/crate"
)

// keepDigs is how many past digs are kept for `last` and `save`.
const keepDigs = 200

// Library is an open library database.
type Library struct {
	db  *sql.DB
	now func() time.Time
}

// DefaultPath is <config dir>/cratecli/library.db, overridable with
// CRATECLI_LIBRARY.
func DefaultPath() (string, error) {
	if p := os.Getenv("CRATECLI_LIBRARY"); p != "" {
		return p, nil
	}
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "library.db"), nil
}

// Open opens (creating if needed) the library at path and brings its schema
// up to date.
func Open(path string) (*Library, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	l := &Library{db: db, now: time.Now}
	if err := l.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("library %s: %w", path, err)
	}
	return l, nil
}

// Close closes the database.
func (l *Library) Close() error { return l.db.Close() }

// migrations run in order; PRAGMA user_version records how many have run.
var migrations = []string{
	`CREATE TABLE records (
		key        TEXT PRIMARY KEY,
		release_id INTEGER NOT NULL DEFAULT 0,
		artist     TEXT NOT NULL,
		title      TEXT NOT NULL,
		track_only INTEGER NOT NULL DEFAULT 0,
		data       TEXT NOT NULL,  -- latest crate.Record as JSON
		updated_at TEXT NOT NULL
	);
	CREATE INDEX records_release ON records(release_id) WHERE release_id > 0;

	CREATE TABLE digs (
		id      INTEGER PRIMARY KEY AUTOINCREMENT,
		at      TEXT NOT NULL,
		command TEXT NOT NULL,
		tracked INTEGER NOT NULL  -- 1 if its records were marked seen
	);
	CREATE TABLE dig_items (
		dig_id INTEGER NOT NULL REFERENCES digs(id) ON DELETE CASCADE,
		pos    INTEGER NOT NULL,  -- 1-based, as numbered in the table output
		key    TEXT NOT NULL REFERENCES records(key),
		PRIMARY KEY (dig_id, pos)
	);

	CREATE TABLE seen (
		key        TEXT PRIMARY KEY REFERENCES records(key),
		release_id INTEGER NOT NULL DEFAULT 0,
		source     TEXT NOT NULL DEFAULT '',
		first_dig  INTEGER,  -- dig that first showed it (for undo)
		first_seen TEXT NOT NULL,
		last_seen  TEXT NOT NULL,
		times      INTEGER NOT NULL DEFAULT 1
	);
	CREATE INDEX seen_release ON seen(release_id) WHERE release_id > 0;
	CREATE INDEX seen_first_dig ON seen(first_dig);`,

	`CREATE TABLE saved (
		key      TEXT PRIMARY KEY REFERENCES records(key),
		saved_at TEXT NOT NULL,
		note     TEXT NOT NULL DEFAULT ''
	);`,

	`CREATE TABLE digest_runs (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		digest      TEXT NOT NULL,
		run_date    TEXT NOT NULL,  -- local date it was for, YYYY-MM-DD
		started_at  TEXT NOT NULL,
		finished_at TEXT NOT NULL,
		status      TEXT NOT NULL,  -- sent, failed, skipped, dry-run
		recipients  INTEGER NOT NULL DEFAULT 0,
		records     INTEGER NOT NULL DEFAULT 0,
		host        TEXT NOT NULL DEFAULT '',  -- which machine ran it
		message     TEXT NOT NULL DEFAULT ''   -- error or reason
	);
	CREATE INDEX digest_runs_day ON digest_runs(digest, run_date);`,

	`ALTER TABLE saved ADD COLUMN origin TEXT NOT NULL DEFAULT 'crate';  -- crate or youtube
	CREATE TABLE youtube_likes (
		video_id     TEXT PRIMARY KEY,
		title        TEXT NOT NULL,
		channel      TEXT NOT NULL DEFAULT '',
		record_key   TEXT NOT NULL REFERENCES records(key),
		first_synced TEXT NOT NULL,
		last_synced  TEXT NOT NULL,
		liked        INTEGER NOT NULL DEFAULT 1  -- 0 once a full sync no longer finds it
	);`,
}

func (l *Library) migrate() error {
	var v int
	if err := l.db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	for i := v; i < len(migrations); i++ {
		tx, err := l.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (l *Library) stamp() string { return l.now().UTC().Format(time.RFC3339) }

// Unseen returns the records not seen before, in order, and how many were
// dropped. A record counts as seen if its key or its Discogs release ID was.
func (l *Library) Unseen(ctx context.Context, recs []crate.Record) ([]crate.Record, int, error) {
	stmt, err := l.db.PrepareContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM seen WHERE key = ?1 OR (?2 > 0 AND release_id = ?2))`)
	if err != nil {
		return nil, 0, err
	}
	defer stmt.Close()
	out := make([]crate.Record, 0, len(recs))
	for _, r := range recs {
		var seen bool
		if err := stmt.QueryRowContext(ctx, r.Key(), r.ReleaseID).Scan(&seen); err != nil {
			return nil, 0, err
		}
		if !seen {
			out = append(out, r)
		}
	}
	return out, len(recs) - len(out), nil
}

// Dig is one past run of a dig command.
type Dig struct {
	ID      int64     `json:"id"`
	At      time.Time `json:"at"`
	Command string    `json:"command"`
	Tracked bool      `json:"tracked"` // its records were marked seen
	Count   int       `json:"count"`
}

// RecordDig stores the records a command printed, numbered as shown, so
// they can be listed again or saved by number. With markSeen they also join
// the seen history.
func (l *Library) RecordDig(ctx context.Context, command string, recs []crate.Record, markSeen bool) (int64, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := l.stamp()
	res, err := tx.ExecContext(ctx, `INSERT INTO digs(at, command, tracked) VALUES (?, ?, ?)`, now, command, markSeen)
	if err != nil {
		return 0, err
	}
	digID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for i, r := range recs {
		key := r.Key()
		if key == "" {
			continue
		}
		if err := upsertRecord(ctx, tx, key, r, now); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO dig_items(dig_id, pos, key) VALUES (?, ?, ?)`, digID, i+1, key); err != nil {
			return 0, err
		}
		if !markSeen {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO seen(key, release_id, source, first_dig, first_seen, last_seen)
			VALUES (?1, ?2, ?3, ?4, ?5, ?5)
			ON CONFLICT(key) DO UPDATE SET
				last_seen = excluded.last_seen,
				times = times + 1,
				release_id = CASE WHEN excluded.release_id > 0 THEN excluded.release_id ELSE release_id END`,
			key, r.ReleaseID, r.Source, digID, now); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM digs WHERE id <= ?`, digID-keepDigs); err != nil {
		return 0, err
	}
	return digID, tx.Commit()
}

func upsertRecord(ctx context.Context, tx *sql.Tx, key string, r crate.Record, now string) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO records(key, release_id, artist, title, track_only, data, updated_at)
		VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)
		ON CONFLICT(key) DO UPDATE SET
			release_id = CASE WHEN excluded.release_id > 0 THEN excluded.release_id ELSE release_id END,
			artist = excluded.artist, title = excluded.title, track_only = excluded.track_only,
			data = excluded.data, updated_at = excluded.updated_at`,
		key, r.ReleaseID, r.Artist, r.Title, r.TrackOnly, string(data), now)
	return err
}

// ErrNoDigs means nothing has been dug yet.
var ErrNoDigs = errors.New("no digs yet")

// LastDig returns the most recent dig and its records in printed order.
func (l *Library) LastDig(ctx context.Context) (Dig, []crate.Record, error) {
	return l.findDig(ctx, digQuery+` ORDER BY d.id DESC LIMIT 1`)
}

// GetDig returns a past dig and its records by ID.
func (l *Library) GetDig(ctx context.Context, id int64) (Dig, []crate.Record, error) {
	return l.findDig(ctx, digQuery+` WHERE d.id = ?`, id)
}

// Digs lists recent digs, newest first.
func (l *Library) Digs(ctx context.Context, limit int) ([]Dig, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := l.db.QueryContext(ctx, digQuery+` ORDER BY d.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Dig
	for rows.Next() {
		d, err := l.scanDig(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (l *Library) findDig(ctx context.Context, query string, args ...any) (Dig, []crate.Record, error) {
	d, err := l.scanDig(l.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Dig{}, nil, ErrNoDigs
	}
	if err != nil {
		return Dig{}, nil, err
	}
	rows, err := l.db.QueryContext(ctx, `
		SELECT r.data, r.track_only FROM dig_items i JOIN records r ON r.key = i.key
		WHERE i.dig_id = ? ORDER BY i.pos`, d.ID)
	if err != nil {
		return Dig{}, nil, err
	}
	defer rows.Close()
	var recs []crate.Record
	for rows.Next() {
		var data string
		var track bool
		if err := rows.Scan(&data, &track); err != nil {
			return Dig{}, nil, err
		}
		var r crate.Record
		if err := json.Unmarshal([]byte(data), &r); err != nil {
			return Dig{}, nil, err
		}
		r.TrackOnly = track
		recs = append(recs, r)
	}
	return d, recs, rows.Err()
}

// Ping checks the database is reachable (for health checks).
func (l *Library) Ping(ctx context.Context) error { return l.db.PingContext(ctx) }

const digQuery = `SELECT d.id, d.at, d.command, d.tracked,
	(SELECT COUNT(*) FROM dig_items i WHERE i.dig_id = d.id) FROM digs d`

type scanner interface{ Scan(...any) error }

func (l *Library) scanDig(row scanner) (Dig, error) {
	var d Dig
	var at string
	if err := row.Scan(&d.ID, &at, &d.Command, &d.Tracked, &d.Count); err != nil {
		return Dig{}, err
	}
	d.At, _ = time.Parse(time.RFC3339, at)
	return d, nil
}

// UndoSeen forgets the records first seen in the most recent dig that
// showed anything new, so they show up again. Calling it again goes one
// dig further back.
func (l *Library) UndoSeen(ctx context.Context) (Dig, int, error) {
	d, err := l.scanDig(l.db.QueryRowContext(ctx, digQuery+`
		WHERE d.tracked = 1 AND EXISTS(SELECT 1 FROM seen s WHERE s.first_dig = d.id)
		ORDER BY d.id DESC LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return Dig{}, 0, ErrNoDigs
	}
	if err != nil {
		return Dig{}, 0, err
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return Dig{}, 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM seen WHERE first_dig = ?`, d.ID)
	if err != nil {
		return Dig{}, 0, err
	}
	n, _ := res.RowsAffected()
	if _, err := tx.ExecContext(ctx, `UPDATE digs SET tracked = 0 WHERE id = ?`, d.ID); err != nil {
		return Dig{}, 0, err
	}
	return d, int(n), tx.Commit()
}

// ClearSeen forgets the whole seen history. Digs are kept.
func (l *Library) ClearSeen(ctx context.Context) (int, error) {
	res, err := l.db.ExecContext(ctx, `DELETE FROM seen`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	_, err = l.db.ExecContext(ctx, `UPDATE digs SET tracked = 0`)
	return int(n), err
}

// SourceCount is how many records were first seen from one source.
type SourceCount struct {
	Source string `json:"source"`
	Count  int    `json:"count"`
}

// Stats summarises the seen history.
type Stats struct {
	Seen      int           `json:"seen"`
	Today     int           `json:"today"`
	BySource  []SourceCount `json:"by_source"`
	RecentDig []Dig         `json:"recent_digs"`
}

// Stats returns seen totals, the busiest sources and recent digs.
func (l *Library) Stats(ctx context.Context) (Stats, error) {
	var s Stats
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM seen`).Scan(&s.Seen); err != nil {
		return s, err
	}
	midnight := time.Date(l.now().Year(), l.now().Month(), l.now().Day(), 0, 0, 0, 0, time.Local).UTC().Format(time.RFC3339)
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM seen WHERE first_seen >= ?`, midnight).Scan(&s.Today); err != nil {
		return s, err
	}
	rows, err := l.db.QueryContext(ctx, `SELECT source, COUNT(*) c FROM seen GROUP BY source ORDER BY c DESC, source LIMIT 15`)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var c SourceCount
		if err := rows.Scan(&c.Source, &c.Count); err != nil {
			rows.Close()
			return s, err
		}
		s.BySource = append(s.BySource, c)
	}
	rows.Close()
	rows, err = l.db.QueryContext(ctx, digQuery+` ORDER BY d.id DESC LIMIT 8`)
	if err != nil {
		return s, err
	}
	defer rows.Close()
	for rows.Next() {
		d, err := l.scanDig(rows)
		if err != nil {
			return s, err
		}
		s.RecentDig = append(s.RecentDig, d)
	}
	return s, rows.Err()
}

// SaveResult says what Save did with one record.
type SaveResult int

const (
	Added SaveResult = iota
	AlreadySaved
	NoteUpdated
)

// Save adds records to your saved crate, storing their latest data. A
// non-empty note replaces the note on records already saved. Saved records
// also count as seen.
func (l *Library) Save(ctx context.Context, recs []crate.Record, note string) ([]SaveResult, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := l.stamp()
	out := make([]SaveResult, len(recs))
	for i, r := range recs {
		key := r.Key()
		if key == "" {
			return nil, fmt.Errorf("can't save a record with no artist or title")
		}
		if err := upsertRecord(ctx, tx, key, r, now); err != nil {
			return nil, err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO saved(key, saved_at, note) VALUES (?, ?, ?) ON CONFLICT(key) DO NOTHING`, key, now, note)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			out[i] = AlreadySaved
			if note != "" {
				if _, err := tx.ExecContext(ctx, `UPDATE saved SET note = ? WHERE key = ?`, note, key); err != nil {
					return nil, err
				}
				out[i] = NoteUpdated
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO seen(key, release_id, source, first_seen, last_seen) VALUES (?1, ?2, ?3, ?4, ?4)
			ON CONFLICT(key) DO NOTHING`, key, r.ReleaseID, r.Source, now); err != nil {
			return nil, err
		}
	}
	return out, tx.Commit()
}

// Saved returns your saved records, newest first, with SavedAt and MyNote set.
func (l *Library) Saved(ctx context.Context) ([]crate.Record, error) {
	rows, err := l.db.QueryContext(ctx, `
		SELECT r.data, r.track_only, s.saved_at, s.note, s.origin
		FROM saved s JOIN records r ON r.key = s.key
		ORDER BY s.saved_at DESC, s.rowid ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []crate.Record
	for rows.Next() {
		var data, at, note, origin string
		var track bool
		if err := rows.Scan(&data, &track, &at, &note, &origin); err != nil {
			return nil, err
		}
		var r crate.Record
		if err := json.Unmarshal([]byte(data), &r); err != nil {
			return nil, err
		}
		r.TrackOnly, r.SavedAt, r.MyNote, r.Origin = track, at, note, origin
		out = append(out, r)
	}
	return out, rows.Err()
}

// Unsave removes records from the saved crate. They stay seen.
func (l *Library) Unsave(ctx context.Context, recs []crate.Record) (int, error) {
	n := 0
	for _, r := range recs {
		res, err := l.db.ExecContext(ctx, `DELETE FROM saved WHERE key = ?`, r.Key())
		if err != nil {
			return n, err
		}
		c, _ := res.RowsAffected()
		n += int(c)
	}
	return n, nil
}
