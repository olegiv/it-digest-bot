package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Posts is the repository for the posts_log table. Every message the bot
// sends is recorded here with its JSON payload for audit and recovery.
type Posts struct{ db *sql.DB }

// Kind is a tagged string to prevent typos in the `kind` column.
type Kind string

const (
	KindRelease Kind = "release"
	KindDigest  Kind = "digest"
)

// Record inserts a post-log row and returns the new row id.
func (p *Posts) Record(ctx context.Context, kind Kind, payloadJSON string, tgMessageID int64) (int64, error) {
	res, err := p.db.ExecContext(ctx, `
        INSERT INTO posts_log (kind, payload_json, tg_message_id)
        VALUES (?, ?, ?)`,
		string(kind), payloadJSON, nullInt64(tgMessageID))
	if err != nil {
		return 0, fmt.Errorf("insert posts_log: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}
	return id, nil
}

// Count returns the total number of posts of a given kind (useful for tests).
func (p *Posts) Count(ctx context.Context, kind Kind) (int, error) {
	var n int
	err := p.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM posts_log WHERE kind = ?`, string(kind)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count posts_log: %w", err)
	}
	return n, nil
}
