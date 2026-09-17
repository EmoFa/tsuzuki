package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ShowPrefs are per-show settings. Nil fields use the config.
type ShowPrefs struct {
	MediaID      int
	SubLanguages []string // nil: config's subtitles.languages
	SubShow      *bool    // nil: config's subtitles.show
	UpdatedAt    time.Time
}

func (s *Store) ShowPrefs(ctx context.Context, mediaID int) (*ShowPrefs, error) {
	p := ShowPrefs{MediaID: mediaID}
	var langs sql.NullString
	var show sql.NullBool
	var updated int64
	err := s.DB.QueryRowContext(ctx,
		"SELECT sub_languages, sub_show, updated_at FROM show_prefs WHERE media_id = ?", mediaID,
	).Scan(&langs, &show, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if langs.Valid {
		p.SubLanguages = []string{}
		if langs.String != "" {
			p.SubLanguages = strings.Split(langs.String, ",")
		}
	}
	if show.Valid {
		p.SubShow = &show.Bool
	}
	p.UpdatedAt = time.UnixMilli(updated)
	return &p, nil
}

func (s *Store) SaveShowPrefs(ctx context.Context, p ShowPrefs) error {
	var langs any
	if p.SubLanguages != nil {
		langs = strings.Join(p.SubLanguages, ",")
	}
	var show any
	if p.SubShow != nil {
		show = *p.SubShow
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO show_prefs (media_id, sub_languages, sub_show, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (media_id) DO UPDATE SET sub_languages = excluded.sub_languages,
			sub_show = excluded.sub_show, updated_at = excluded.updated_at`,
		p.MediaID, langs, show, time.Now().UnixMilli())
	return err
}

func (s *Store) DeleteShowPrefs(ctx context.Context, mediaID int) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM show_prefs WHERE media_id = ?", mediaID)
	return err
}
