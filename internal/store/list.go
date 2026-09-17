package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ListEntry is one show on the user's list.
type ListEntry struct {
	MediaID   int
	Status    string
	Progress  int
	Score     float64
	UpdatedAt time.Time
}

// PendingSync is a list change waiting to be saved remotely.
type PendingSync struct {
	MediaID   int
	Status    string
	Progress  int
	Score     *float64 // nil: leave the remote score unchanged
	Attempts  int
	LastError string
	QueuedAt  time.Time
}

func (s *Store) ListEntry(ctx context.Context, mediaID int) (*ListEntry, error) {
	e := ListEntry{MediaID: mediaID}
	var updated int64
	err := s.DB.QueryRowContext(ctx,
		"SELECT status, progress, score, updated_at FROM list_entries WHERE media_id = ?", mediaID,
	).Scan(&e.Status, &e.Progress, &e.Score, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.UpdatedAt = time.UnixMilli(updated)
	return &e, nil
}

func (s *Store) SaveListEntry(ctx context.Context, e ListEntry) error {
	if e.UpdatedAt.IsZero() {
		e.UpdatedAt = time.Now()
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO list_entries (media_id, status, progress, score, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (media_id) DO UPDATE SET status = excluded.status, progress = excluded.progress,
			score = excluded.score, updated_at = excluded.updated_at`,
		e.MediaID, e.Status, e.Progress, e.Score, e.UpdatedAt.UnixMilli())
	return err
}

func (s *Store) DeleteListEntry(ctx context.Context, mediaID int) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM list_entries WHERE media_id = ?", mediaID)
	return err
}

// ListEntries returns entries with status (all when empty), most recently updated first.
func (s *Store) ListEntries(ctx context.Context, status string) ([]ListEntry, error) {
	q, args := "SELECT media_id, status, progress, score, updated_at FROM list_entries", []any{}
	if status != "" {
		q, args = q+" WHERE status = ?", append(args, status)
	}
	rows, err := s.DB.QueryContext(ctx, q+" ORDER BY updated_at DESC", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ListEntry
	for rows.Next() {
		var e ListEntry
		var updated int64
		if err := rows.Scan(&e.MediaID, &e.Status, &e.Progress, &e.Score, &updated); err != nil {
			return nil, err
		}
		e.UpdatedAt = time.UnixMilli(updated)
		out = append(out, e)
	}
	return out, rows.Err()
}

// QueueSync records the latest state to push for a show, replacing any pending one.
// QueueSync records a change to send. score is sent only when not nil; a
// newer change without a score keeps a pending one's score.
func (s *Store) QueueSync(ctx context.Context, mediaID int, status string, progress int, score *float64) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO sync_queue (media_id, status, progress, score, queued_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (media_id) DO UPDATE SET status = excluded.status, progress = excluded.progress,
			score = COALESCE(excluded.score, sync_queue.score),
			attempts = 0, last_error = '', queued_at = excluded.queued_at`,
		mediaID, status, progress, score, time.Now().UnixMilli())
	return err
}

// SyncFailed notes a failed push attempt.
func (s *Store) SyncFailed(ctx context.Context, mediaID int, cause error) error {
	_, err := s.DB.ExecContext(ctx,
		"UPDATE sync_queue SET attempts = attempts + 1, last_error = ? WHERE media_id = ?", cause.Error(), mediaID)
	return err
}

// SyncDone removes a pushed change, unless a newer one was queued meanwhile.
func (s *Store) SyncDone(ctx context.Context, p PendingSync) error {
	_, err := s.DB.ExecContext(ctx,
		"DELETE FROM sync_queue WHERE media_id = ? AND queued_at = ?", p.MediaID, p.QueuedAt.UnixMilli())
	return err
}

func (s *Store) PendingSyncs(ctx context.Context) ([]PendingSync, error) {
	rows, err := s.DB.QueryContext(ctx,
		"SELECT media_id, status, progress, score, attempts, last_error, queued_at FROM sync_queue ORDER BY queued_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingSync
	for rows.Next() {
		var p PendingSync
		var queued int64
		var score sql.NullFloat64
		if err := rows.Scan(&p.MediaID, &p.Status, &p.Progress, &score, &p.Attempts, &p.LastError, &queued); err != nil {
			return nil, err
		}
		if score.Valid {
			p.Score = &score.Float64
		}
		p.QueuedAt = time.UnixMilli(queued)
		out = append(out, p)
	}
	return out, rows.Err()
}
