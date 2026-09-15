package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// GetKV reads a value from the key/value table.
func (s *Store) GetKV(ctx context.Context, key string) (value string, updated time.Time, ok bool, err error) {
	var ts int64
	err = s.DB.QueryRowContext(ctx, "SELECT value, updated_at FROM kv WHERE key = ?", key).Scan(&value, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, false, nil
	}
	if err != nil {
		return "", time.Time{}, false, err
	}
	return value, time.Unix(ts, 0), true, nil
}

// PutKV writes a value, stamping it with the current time.
func (s *Store) PutKV(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO kv (key, value, updated_at) VALUES (?, ?, unixepoch())
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value)
	return err
}
