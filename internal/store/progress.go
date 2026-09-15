package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Progress is how far an episode has been watched.
type Progress struct {
	MediaID   int
	Episode   float64
	Position  time.Duration
	Duration  time.Duration
	Completed bool
	Provider  string
	Mode      string
	UpdatedAt time.Time
}

// SaveProgress records an episode's position. Once an episode is completed it
// stays completed, even if rewatched partway.
func (s *Store) SaveProgress(ctx context.Context, p Progress) error {
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = time.Now()
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO watch_progress (media_id, episode, position_ms, duration_ms, completed, provider, mode, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (media_id, episode) DO UPDATE SET
			position_ms = excluded.position_ms,
			duration_ms = excluded.duration_ms,
			completed   = max(watch_progress.completed, excluded.completed),
			provider    = excluded.provider,
			mode        = excluded.mode,
			updated_at  = excluded.updated_at`,
		p.MediaID, p.Episode, p.Position.Milliseconds(), p.Duration.Milliseconds(), p.Completed,
		p.Provider, p.Mode, p.UpdatedAt.UnixMilli())
	return err
}

const progressColumns = "media_id, episode, position_ms, duration_ms, completed, provider, mode, updated_at"

func scanProgress(row interface{ Scan(...any) error }) (Progress, error) {
	var p Progress
	var pos, dur, updated int64
	err := row.Scan(&p.MediaID, &p.Episode, &pos, &dur, &p.Completed, &p.Provider, &p.Mode, &updated)
	p.Position = time.Duration(pos) * time.Millisecond
	p.Duration = time.Duration(dur) * time.Millisecond
	p.UpdatedAt = time.UnixMilli(updated)
	return p, err
}

// EpisodeProgress returns one episode's progress, or nil when never watched.
func (s *Store) EpisodeProgress(ctx context.Context, mediaID int, episode float64) (*Progress, error) {
	p, err := scanProgress(s.DB.QueryRowContext(ctx,
		"SELECT "+progressColumns+" FROM watch_progress WHERE media_id = ? AND episode = ?", mediaID, episode))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ShowProgress lists a show's watched episodes in episode order.
func (s *Store) ShowProgress(ctx context.Context, mediaID int) ([]Progress, error) {
	return s.queryProgress(ctx,
		"SELECT "+progressColumns+" FROM watch_progress WHERE media_id = ? ORDER BY episode", mediaID)
}

// RecentShows returns the most recently watched episode of each show, newest first.
func (s *Store) RecentShows(ctx context.Context, limit int) ([]Progress, error) {
	return s.queryProgress(ctx, `
		SELECT `+progressColumns+` FROM watch_progress w
		WHERE updated_at = (SELECT max(updated_at) FROM watch_progress WHERE media_id = w.media_id)
		GROUP BY media_id
		ORDER BY updated_at DESC
		LIMIT ?`, limit)
}

func (s *Store) queryProgress(ctx context.Context, q string, args ...any) ([]Progress, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Progress
	for rows.Next() {
		p, err := scanProgress(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
