package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Progress is how far an episode has been watched.
type Progress struct {
	MediaID int
	Episode float64
	// Round is which watch-through this belongs to, from 1. Zero means the
	// show's current round when saving.
	Round     int
	Position  time.Duration
	Duration  time.Duration
	Completed bool
	Provider  string
	Mode      string
	UpdatedAt time.Time
}

// SaveProgress records an episode's position in the show's current round. Once
// an episode is completed it stays completed, even if rewatched partway.
func (s *Store) SaveProgress(ctx context.Context, p Progress) error {
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = time.Now()
	}
	if p.Round == 0 {
		round, err := s.Round(ctx, p.MediaID)
		if err != nil {
			return err
		}
		p.Round = round
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO watch_progress (media_id, episode, round, position_ms, duration_ms, completed, provider, mode, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (media_id, episode, round) DO UPDATE SET
			position_ms = excluded.position_ms,
			duration_ms = excluded.duration_ms,
			completed   = max(watch_progress.completed, excluded.completed),
			provider    = excluded.provider,
			mode        = excluded.mode,
			updated_at  = excluded.updated_at`,
		p.MediaID, p.Episode, p.Round, p.Position.Milliseconds(), p.Duration.Milliseconds(), p.Completed,
		p.Provider, p.Mode, p.UpdatedAt.UnixMilli())
	return err
}

// SetWatched marks an episode watched or not in the show's current round, for
// episodes watched elsewhere or wrongly recorded. Unmarking forgets the
// episode's position too.
func (s *Store) SetWatched(ctx context.Context, mediaID int, episode float64, watched bool) error {
	round, err := s.Round(ctx, mediaID)
	if err != nil {
		return err
	}
	if !watched {
		_, err := s.DB.ExecContext(ctx,
			"DELETE FROM watch_progress WHERE media_id = ? AND episode = ? AND round = ?", mediaID, episode, round)
		return err
	}
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO watch_progress (media_id, episode, round, position_ms, duration_ms, completed, provider, mode, updated_at)
		VALUES (?, ?, ?, 0, 0, 1, '', '', ?)
		ON CONFLICT (media_id, episode, round) DO UPDATE SET completed = 1, updated_at = excluded.updated_at`,
		mediaID, episode, round, time.Now().UnixMilli())
	return err
}

// Round is the show's current watch-through, 1 until it's rewatched.
func (s *Store) Round(ctx context.Context, mediaID int) (int, error) {
	round := 1
	err := s.DB.QueryRowContext(ctx, "SELECT round FROM show_rounds WHERE media_id = ?", mediaID).Scan(&round)
	if errors.Is(err, sql.ErrNoRows) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	return round, nil
}

// StartRound begins another watch-through, returning its number. Earlier
// rounds' progress stays in history.
func (s *Store) StartRound(ctx context.Context, mediaID int) (int, error) {
	round, err := s.Round(ctx, mediaID)
	if err != nil {
		return 0, err
	}
	round++
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO show_rounds (media_id, round, started_at) VALUES (?, ?, ?)
		ON CONFLICT (media_id) DO UPDATE SET round = excluded.round, started_at = excluded.started_at`,
		mediaID, round, time.Now().UnixMilli())
	if err != nil {
		return 0, err
	}
	return round, nil
}

// WatchedBefore reports the episodes completed in earlier rounds.
func (s *Store) WatchedBefore(ctx context.Context, mediaID int) (map[float64]bool, error) {
	round, err := s.Round(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx,
		"SELECT episode FROM watch_progress WHERE media_id = ? AND completed = 1 AND round < ?", mediaID, round)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[float64]bool{}
	for rows.Next() {
		var episode float64
		if err := rows.Scan(&episode); err != nil {
			return nil, err
		}
		out[episode] = true
	}
	return out, rows.Err()
}

const progressColumns = "media_id, episode, round, position_ms, duration_ms, completed, provider, mode, updated_at"

func scanProgress(row interface{ Scan(...any) error }) (Progress, error) {
	var p Progress
	var pos, dur, updated int64
	err := row.Scan(&p.MediaID, &p.Episode, &p.Round, &pos, &dur, &p.Completed, &p.Provider, &p.Mode, &updated)
	p.Position = time.Duration(pos) * time.Millisecond
	p.Duration = time.Duration(dur) * time.Millisecond
	p.UpdatedAt = time.UnixMilli(updated)
	return p, err
}

// EpisodeProgress returns one episode's progress in the current round, or nil
// when it hasn't been watched in this round.
func (s *Store) EpisodeProgress(ctx context.Context, mediaID int, episode float64) (*Progress, error) {
	round, err := s.Round(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	p, err := scanProgress(s.DB.QueryRowContext(ctx,
		"SELECT "+progressColumns+" FROM watch_progress WHERE media_id = ? AND episode = ? AND round = ?",
		mediaID, episode, round))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ShowProgress lists a show's watched episodes in the current round, in
// episode order.
func (s *Store) ShowProgress(ctx context.Context, mediaID int) ([]Progress, error) {
	round, err := s.Round(ctx, mediaID)
	if err != nil {
		return nil, err
	}
	return s.queryProgress(ctx,
		"SELECT "+progressColumns+" FROM watch_progress WHERE media_id = ? AND round = ? ORDER BY episode",
		mediaID, round)
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
