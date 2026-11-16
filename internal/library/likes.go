package library

import (
	"context"

	"github.com/williamdunstanmorris/cratecli/internal/crate"
)

// Like is a liked YouTube video and the record it became.
type Like struct {
	VideoID string
	Title   string
	Channel string
}

// HasLike reports whether a video was synced before.
func (l *Library) HasLike(ctx context.Context, videoID string) (bool, error) {
	var ok bool
	err := l.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM youtube_likes WHERE video_id = ?)`, videoID).Scan(&ok)
	return ok, err
}

// AddLike stores a newly synced like: the record, a saved entry (origin
// "youtube", unless it's already saved, e.g. via cratecli save) and the
// link between video and record. It reports whether the record was newly
// saved.
func (l *Library) AddLike(ctx context.Context, like Like, r crate.Record) (bool, error) {
	key := r.Key()
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	now := l.stamp()
	// Don't overwrite a richer record (e.g. one matched on Discogs) with
	// the bare like: only insert when new.
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM records WHERE key = ?)`, key).Scan(&exists); err != nil {
		return false, err
	}
	if !exists {
		if err := upsertRecord(ctx, tx, key, r, now); err != nil {
			return false, err
		}
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO saved(key, saved_at, note, origin) VALUES (?, ?, '', 'youtube') ON CONFLICT(key) DO NOTHING`, key, now)
	if err != nil {
		return false, err
	}
	added, _ := res.RowsAffected()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO seen(key, release_id, source, first_seen, last_seen) VALUES (?1, ?2, ?3, ?4, ?4)
		ON CONFLICT(key) DO NOTHING`, key, r.ReleaseID, r.Source, now); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO youtube_likes(video_id, title, channel, record_key, first_synced, last_synced)
		VALUES (?1, ?2, ?3, ?4, ?5, ?5)
		ON CONFLICT(video_id) DO UPDATE SET last_synced = excluded.last_synced, liked = 1`,
		like.VideoID, like.Title, like.Channel, key, now); err != nil {
		return false, err
	}
	return added == 1, tx.Commit()
}

// TouchLikes marks videos as still liked (seen in this sync).
func (l *Library) TouchLikes(ctx context.Context, ids []string) error {
	now := l.stamp()
	for _, id := range ids {
		if _, err := l.db.ExecContext(ctx, `UPDATE youtube_likes SET last_synced = ?, liked = 1 WHERE video_id = ?`, now, id); err != nil {
			return err
		}
	}
	return nil
}

// MarkUnliked flags likes that a full sync didn't find (you unliked them
// on YouTube) and returns how many. stillLiked is every video ID the sync
// saw. Their records stay saved.
func (l *Library) MarkUnliked(ctx context.Context, stillLiked []string) (int, error) {
	keep := make(map[string]bool, len(stillLiked))
	for _, id := range stillLiked {
		keep[id] = true
	}
	rows, err := l.db.QueryContext(ctx, `SELECT video_id FROM youtube_likes WHERE liked = 1`)
	if err != nil {
		return 0, err
	}
	var gone []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		if !keep[id] {
			gone = append(gone, id)
		}
	}
	rows.Close()
	for _, id := range gone {
		if _, err := l.db.ExecContext(ctx, `UPDATE youtube_likes SET liked = 0 WHERE video_id = ?`, id); err != nil {
			return 0, err
		}
	}
	return len(gone), nil
}

// LikeCount is how many synced likes there are.
func (l *Library) LikeCount(ctx context.Context) (int, error) {
	var n int
	err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM youtube_likes WHERE liked = 1`).Scan(&n)
	return n, err
}
