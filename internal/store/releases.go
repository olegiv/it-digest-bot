package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Releases is the repository for the releases_seen table.
type Releases struct{ db *sql.DB }

// Release is the DB representation of a posted upstream release.
type Release struct {
	Package     string
	Version     string
	PostedAt    time.Time
	TgMessageID sql.NullInt64
	ReleaseURL  sql.NullString
}

// ErrNotFound is returned when a lookup finds no matching row.
var ErrNotFound = errors.New("not found")

// GetLatestSeen returns the most recently recorded version for the given
// package. Returns ErrNotFound if no rows exist.
func (r *Releases) GetLatestSeen(ctx context.Context, pkg string) (*Release, error) {
	row := r.db.QueryRowContext(ctx, `
        SELECT package, version, posted_at, tg_message_id, release_url
          FROM releases_seen
         WHERE package = ?
      ORDER BY posted_at DESC
         LIMIT 1`, pkg)
	var rel Release
	err := row.Scan(&rel.Package, &rel.Version, &rel.PostedAt, &rel.TgMessageID, &rel.ReleaseURL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan releases_seen: %w", err)
	}
	return &rel, nil
}

// RecordSeen persists a newly-posted release. The (package, version) pair
// is the primary key so repeated inserts are a no-op via OR IGNORE.
func (r *Releases) RecordSeen(ctx context.Context, pkg, version string, tgMessageID int64, releaseURL string) error {
	_, err := r.db.ExecContext(ctx, `
        INSERT OR IGNORE INTO releases_seen
            (package, version, tg_message_id, release_url)
        VALUES (?, ?, ?, ?)`,
		pkg, version, nullInt64(tgMessageID), nullString(releaseURL))
	if err != nil {
		return fmt.Errorf("insert releases_seen: %w", err)
	}
	return nil
}

func nullInt64(v int64) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}

func nullString(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}
