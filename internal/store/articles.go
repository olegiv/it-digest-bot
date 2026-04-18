package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Articles is the repository for the articles_seen table used by phase 2.
type Articles struct{ db *sql.DB }

// Article is a single row in articles_seen.
type Article struct {
	URLHash  string
	URL      string
	Title    sql.NullString
	Source   sql.NullString
	SeenAt   time.Time
	PostedAt sql.NullTime
}

// Seen returns true if an article with the given URL hash has been recorded.
func (a *Articles) Seen(ctx context.Context, urlHash string) (bool, error) {
	var n int
	err := a.db.QueryRowContext(ctx,
		`SELECT 1 FROM articles_seen WHERE url_hash = ? LIMIT 1`, urlHash).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query articles_seen: %w", err)
	}
	return true, nil
}

// Record inserts a seen article (idempotent on url_hash).
func (a *Articles) Record(ctx context.Context, art Article) error {
	_, err := a.db.ExecContext(ctx, `
        INSERT OR IGNORE INTO articles_seen
            (url_hash, url, title, source, posted_at)
        VALUES (?, ?, ?, ?, ?)`,
		art.URLHash, art.URL, art.Title, art.Source, art.PostedAt)
	if err != nil {
		return fmt.Errorf("insert articles_seen: %w", err)
	}
	return nil
}
