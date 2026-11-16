package library

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Run statuses.
const (
	RunSent    = "sent"
	RunFailed  = "failed"
	RunSkipped = "skipped"
	RunDryRun  = "dry-run"
)

// DigestRun is one attempt to send a digest.
type DigestRun struct {
	ID         int64     `json:"id"`
	Digest     string    `json:"digest"`
	Date       string    `json:"date"` // the local day it was for, 2026-11-17
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Status     string    `json:"status"`
	Recipients int       `json:"recipients"`
	Records    int       `json:"records"`
	Host       string    `json:"host,omitempty"`
	Message    string    `json:"message,omitempty"`
}

// AddRun logs a digest run.
func (l *Library) AddRun(ctx context.Context, r DigestRun) (int64, error) {
	res, err := l.db.ExecContext(ctx, `
		INSERT INTO digest_runs(digest, run_date, started_at, finished_at, status, recipients, records, host, message)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Digest, r.Date, r.StartedAt.UTC().Format(time.RFC3339), r.FinishedAt.UTC().Format(time.RFC3339),
		r.Status, r.Recipients, r.Records, r.Host, r.Message)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SentOn reports whether digest was handled for date: sent, or skipped
// because there was nothing new. Failed and dry runs don't count.
func (l *Library) SentOn(ctx context.Context, digest, date string) (bool, error) {
	var ok bool
	err := l.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM digest_runs WHERE digest = ? AND run_date = ? AND status IN (?, ?))`,
		digest, date, RunSent, RunSkipped).Scan(&ok)
	return ok, err
}

// Runs lists recent runs, newest first. digest and date filter when set.
func (l *Library) Runs(ctx context.Context, digest, date string, limit int) ([]DigestRun, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := l.db.QueryContext(ctx, `
		SELECT id, digest, run_date, started_at, finished_at, status, recipients, records, host, message
		FROM digest_runs
		WHERE (?1 = '' OR digest = ?1) AND (?2 = '' OR run_date = ?2)
		ORDER BY id DESC LIMIT ?3`, digest, date, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DigestRun
	for rows.Next() {
		var r DigestRun
		var started, finished string
		if err := rows.Scan(&r.ID, &r.Digest, &r.Date, &started, &finished, &r.Status,
			&r.Recipients, &r.Records, &r.Host, &r.Message); err != nil {
			return nil, err
		}
		r.StartedAt, _ = time.Parse(time.RFC3339, started)
		r.FinishedAt, _ = time.Parse(time.RFC3339, finished)
		out = append(out, r)
	}
	return out, rows.Err()
}

// LastSent returns the most recent successful run of digest, if any.
func (l *Library) LastSent(ctx context.Context, digest string) (DigestRun, bool, error) {
	runs, err := l.Runs(ctx, digest, "", 20)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return DigestRun{}, false, err
	}
	for _, r := range runs {
		if r.Status == RunSent {
			return r, true, nil
		}
	}
	return DigestRun{}, false, nil
}
