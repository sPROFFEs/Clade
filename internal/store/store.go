// Package store owns the single PrAImate SQLite database. It is the
// only package in the tree that opens the DB file. Everything else —
// internal/core, the TUI, the GUI — talks to a *Store via typed query
// methods.
//
// Migrations live under migrations/ and are embedded into the binary.
// They run in lexicographic order on Open(); each file is wrapped in a
// transaction and recorded in schema_version.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store wraps the SQLite connection pool. It is safe for concurrent
// use; modernc.org/sqlite handles the single-writer constraint via a
// connection-level mutex.
type Store struct {
	db   *sql.DB
	path string
}

// Open opens (or creates) the database at path, applies any pending
// migrations, and returns a ready-to-use Store. The parent directory
// is created with 0o700 if missing.
//
// The DSN turns on WAL mode and foreign keys, which are off by default
// in SQLite.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("store.Open: empty path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store.Open: mkdir parent: %w", err)
	}

	// _pragma values are URL-encoded by the driver; '=' becomes %3D.
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store.Open: %w", err)
	}
	// modernc's sqlite serialises writes; a small pool is plenty.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)

	s := &Store{db: db, path: path}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store.Open: migrate: %w", err)
	}
	return s, nil
}

// Close flushes and closes the underlying database. Safe to call
// multiple times.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DB exposes the underlying *sql.DB for sibling packages that need to
// build their own queries. Use sparingly — most callers should add a
// typed method to this package instead.
func (s *Store) DB() *sql.DB { return s.db }

// Path returns the on-disk path the store was opened against.
func (s *Store) Path() string { return s.path }

// SchemaVersion returns the highest applied migration number, or 0 if
// no migrations have run.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&v)
	if err != nil {
		// If the table doesn't exist yet, treat as 0.
		if strings.Contains(err.Error(), "no such table") {
			return 0, nil
		}
		return 0, err
	}
	return v, nil
}

// migrate runs every embedded migration whose version number exceeds
// the current schema_version. Migrations are applied in lexicographic
// order; their filenames must start with a zero-padded integer.
func (s *Store) migrate(ctx context.Context) error {
	// Ensure schema_version exists.
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	current, err := s.SchemaVersion(ctx)
	if err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		v, err := parseMigrationVersion(name)
		if err != nil {
			return fmt.Errorf("migration %q: %w", name, err)
		}
		if v <= current {
			continue
		}
		body, err := fs.ReadFile(migrationsFS, "migrations/"+name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if err := s.applyMigration(ctx, v, name, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) applyMigration(ctx context.Context, version int, name, body string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx for %s: %w", name, err)
	}
	defer tx.Rollback() //nolint:errcheck // intentional: commit replaces it
	if _, err := tx.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("apply %s: %w", name, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version(version) VALUES (?)`, version); err != nil {
		return fmt.Errorf("record %s: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", name, err)
	}
	return nil
}

// parseMigrationVersion extracts the leading integer from filenames
// like "0001_init.sql".
func parseMigrationVersion(name string) (int, error) {
	base := name
	if i := strings.Index(base, "_"); i > 0 {
		base = base[:i]
	} else if i := strings.Index(base, "."); i > 0 {
		base = base[:i]
	}
	v, err := strconv.Atoi(base)
	if err != nil {
		return 0, fmt.Errorf("filename %q has no leading version", name)
	}
	return v, nil
}
