package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenMigratesAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "anitui.db")

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	ms, err := migrations()
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s.SchemaVersion(ctx); v != len(ms) {
		t.Fatalf("schema version = %d, want %d", v, len(ms))
	}
	if _, err := s.DB.ExecContext(ctx, "INSERT INTO kv(key, value) VALUES ('k', 'v')"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Reopening must not re-run migrations or lose data.
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var v string
	if err := s.DB.QueryRowContext(ctx, "SELECT value FROM kv WHERE key = 'k'").Scan(&v); err != nil || v != "v" {
		t.Fatalf("value=%q err=%v", v, err)
	}

	var mode string
	s.DB.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode)
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

func TestOpenRejectsNewerSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "anitui.db")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	s.DB.ExecContext(ctx, "PRAGMA user_version = 999")
	s.Close()

	if _, err := Open(ctx, path); err == nil {
		t.Fatal("expected error opening a database from a newer build")
	}
}
