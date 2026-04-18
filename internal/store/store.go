// Package store owns the SQLite connection, runs schema migrations from an
// embedded FS, and exposes small typed repositories over the data model.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite" // register "sqlite" driver
)

// Store is a thin handle around *sql.DB that owns the repositories.
type Store struct {
	db       *sql.DB
	Releases *Releases
	Articles *Articles
	Posts    *Posts
}

// Open opens (or creates) the SQLite database at the given DSN.
// For production, pass a file path like "/var/lib/it-digest/state.db".
// For tests, pass "file::memory:?cache=shared&mode=memory" or similar.
func Open(ctx context.Context, dsn string) (*Store, error) {
	finalDSN := dsn
	if !strings.Contains(dsn, "?") {
		finalDSN += "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)"
	}
	db, err := sql.Open("sqlite", finalDSN)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	// SQLite writes are serialized; keep the pool small to avoid busy errors.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	s.Releases = &Releases{db: db}
	s.Articles = &Articles{db: db}
	s.Posts = &Posts{db: db}
	return s, nil
}

// DB exposes the underlying *sql.DB for advanced use (e.g. migrations).
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the underlying connection.
func (s *Store) Close() error { return s.db.Close() }
