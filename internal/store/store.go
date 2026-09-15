// Package store owns anitui's SQLite database and its schema migrations.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Store struct {
	DB *sql.DB
}

// Open opens (creating if needed) the database at path and applies pending
// migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	q := url.Values{}
	for _, p := range []string{"busy_timeout(5000)", "journal_mode(WAL)", "foreign_keys(1)", "synchronous(NORMAL)"} {
		q.Add("_pragma", p)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	// SQLite allows one writer; a single connection avoids SQLITE_BUSY between
	// our own goroutines.
	db.SetMaxOpenConns(1)

	s := &Store{DB: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating %s: %w", path, err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

// SchemaVersion returns the number of the last applied migration.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	err := s.DB.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v)
	return v, err
}

type migration struct {
	version int
	name    string
}

// migrations lists embedded files named NNNN_description.sql in order.
func migrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	var ms []migration
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(prefix)
		if !ok || err != nil {
			return nil, fmt.Errorf("bad migration filename %q", e.Name())
		}
		ms = append(ms, migration{v, e.Name()})
	}
	slices.SortFunc(ms, func(a, b migration) int { return a.version - b.version })
	for i, m := range ms {
		if m.version != i+1 {
			return nil, fmt.Errorf("migration %q: expected version %d", m.name, i+1)
		}
	}
	return ms, nil
}

// migrate applies each migration newer than PRAGMA user_version in its own
// transaction, bumping user_version alongside it.
func (s *Store) migrate(ctx context.Context) error {
	ms, err := migrations()
	if err != nil {
		return err
	}
	current, err := s.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	if current > len(ms) {
		return fmt.Errorf("database schema v%d is newer than this build (v%d); upgrade anitui", current, len(ms))
	}
	for _, m := range ms[current:] {
		body, err := migrationFS.ReadFile("migrations/" + m.name)
		if err != nil {
			return err
		}
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
