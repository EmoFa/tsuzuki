package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Mapping links an AniList media entry to a provider's show.
type Mapping struct {
	MediaID   int
	Provider  string
	ShowID    string
	ShowTitle string
	Manual    bool // chosen by the user; automatic matching must not replace it
	UpdatedAt time.Time
}

// Mapping returns the stored mapping, or nil when there is none.
func (s *Store) Mapping(ctx context.Context, mediaID int, provider string) (*Mapping, error) {
	m := Mapping{MediaID: mediaID, Provider: provider}
	var updated int64
	err := s.DB.QueryRowContext(ctx,
		"SELECT show_id, show_title, manual, updated_at FROM provider_mappings WHERE media_id = ? AND provider = ?",
		mediaID, provider,
	).Scan(&m.ShowID, &m.ShowTitle, &m.Manual, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.UpdatedAt = time.Unix(updated, 0)
	return &m, nil
}

func (s *Store) SaveMapping(ctx context.Context, m Mapping) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO provider_mappings (media_id, provider, show_id, show_title, manual, updated_at)
		VALUES (?, ?, ?, ?, ?, unixepoch())
		ON CONFLICT (media_id, provider) DO UPDATE SET show_id = excluded.show_id,
			show_title = excluded.show_title, manual = excluded.manual, updated_at = excluded.updated_at`,
		m.MediaID, m.Provider, m.ShowID, m.ShowTitle, m.Manual)
	return err
}

// DeleteMappings removes a media entry's mapping for one provider, or for all
// providers when provider is empty.
func (s *Store) DeleteMappings(ctx context.Context, mediaID int, provider string) error {
	q, args := "DELETE FROM provider_mappings WHERE media_id = ?", []any{mediaID}
	if provider != "" {
		q, args = q+" AND provider = ?", append(args, provider)
	}
	_, err := s.DB.ExecContext(ctx, q, args...)
	return err
}

// Mappings lists every stored mapping for a media entry.
func (s *Store) Mappings(ctx context.Context, mediaID int) ([]Mapping, error) {
	rows, err := s.DB.QueryContext(ctx,
		"SELECT provider, show_id, show_title, manual, updated_at FROM provider_mappings WHERE media_id = ? ORDER BY provider",
		mediaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Mapping
	for rows.Next() {
		m := Mapping{MediaID: mediaID}
		var updated int64
		if err := rows.Scan(&m.Provider, &m.ShowID, &m.ShowTitle, &m.Manual, &updated); err != nil {
			return nil, err
		}
		m.UpdatedAt = time.Unix(updated, 0)
		out = append(out, m)
	}
	return out, rows.Err()
}
