package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openMemory(t *testing.T) *Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// File-backed temp DB — simpler than the shared-memory trick and
	// each test gets its own isolated database.
	dsn := "file:" + filepath.Join(t.TempDir(), "test.db")
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		_ = s.Close()
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestMigrateCreatesTables(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	rows, err := s.DB().QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	defer rows.Close()

	want := map[string]bool{
		"articles_seen":     true,
		"posts_log":         true,
		"releases_seen":     true,
		"schema_migrations": true,
	}
	got := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		// ignore sqlite internal tables like sqlite_sequence
		if _, ok := want[n]; ok {
			got[n] = true
		}
	}
	for tbl := range want {
		if !got[tbl] {
			t.Errorf("missing table: %s", tbl)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()
	// Running again should be a no-op (no error, no duplicate rows).
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	var n int
	if err := s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("schema_migrations rows = %d, want 1", n)
	}
}

func TestReleasesLifecycle(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	_, err := s.Releases.GetLatestSeen(ctx, "@anthropic-ai/claude-code")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	if err := s.Releases.RecordSeen(ctx, "@anthropic-ai/claude-code",
		"2.1.114", 4242, "https://github.com/anthropics/claude-code/releases/tag/v2.1.114"); err != nil {
		t.Fatalf("RecordSeen: %v", err)
	}

	r, err := s.Releases.GetLatestSeen(ctx, "@anthropic-ai/claude-code")
	if err != nil {
		t.Fatalf("GetLatestSeen: %v", err)
	}
	if r.Version != "2.1.114" {
		t.Errorf("version = %q", r.Version)
	}
	if !r.TgMessageID.Valid || r.TgMessageID.Int64 != 4242 {
		t.Errorf("message id = %+v", r.TgMessageID)
	}

	// Recording the same version again is a no-op.
	if err := s.Releases.RecordSeen(ctx, "@anthropic-ai/claude-code",
		"2.1.114", 9999, "https://example.com"); err != nil {
		t.Fatalf("second RecordSeen: %v", err)
	}
	r2, err := s.Releases.GetLatestSeen(ctx, "@anthropic-ai/claude-code")
	if err != nil {
		t.Fatalf("GetLatestSeen: %v", err)
	}
	if r2.TgMessageID.Int64 != 4242 {
		t.Errorf("OR IGNORE did not preserve original row; got msgid %d", r2.TgMessageID.Int64)
	}
}

func TestArticlesSeenRoundtrip(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	seen, err := s.Articles.Seen(ctx, "abc")
	if err != nil {
		t.Fatalf("Seen: %v", err)
	}
	if seen {
		t.Error("Seen returned true for empty table")
	}

	if err := s.Articles.Record(ctx, Article{
		URLHash: "abc",
		URL:     "https://example.com/post",
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	seen, err = s.Articles.Seen(ctx, "abc")
	if err != nil {
		t.Fatalf("Seen: %v", err)
	}
	if !seen {
		t.Error("Seen returned false after Record")
	}
}

func TestPostsLog(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	id, err := s.Posts.Record(ctx, KindRelease, `{"v":"2.1.114"}`, 42)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero row id")
	}
	n, err := s.Posts.Count(ctx, KindRelease)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 1 {
		t.Errorf("count = %d, want 1", n)
	}
}
