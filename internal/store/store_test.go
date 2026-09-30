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

func TestReleasesRecordSeenBatch(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()

	// Empty batch is a no-op and must not open a transaction that errors.
	if err := s.Releases.RecordSeenBatch(ctx, nil, 7); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	if _, err := s.Releases.GetLatestSeen(ctx, "feed"); err != ErrNotFound {
		t.Fatalf("empty batch wrote rows: %v", err)
	}

	// An existing row must survive (OR IGNORE) and the rest be inserted
	// with the shared message id.
	if err := s.Releases.RecordSeen(ctx, "feed", "b", 1, "https://old.example/b"); err != nil {
		t.Fatalf("RecordSeen: %v", err)
	}
	rows := []SeenRelease{
		{Package: "feed", Version: "a", ReleaseURL: "https://example.com/a"},
		{Package: "feed", Version: "b", ReleaseURL: "https://example.com/b"},
		{Package: "feed", Version: "c"},
	}
	if err := s.Releases.RecordSeenBatch(ctx, rows, 42); err != nil {
		t.Fatalf("RecordSeenBatch: %v", err)
	}
	for _, v := range []string{"a", "b", "c"} {
		seen, err := s.Releases.HasSeen(ctx, "feed", v)
		if err != nil || !seen {
			t.Errorf("version %s not recorded (%v)", v, err)
		}
	}
	var msgID, url interface{}
	if err := s.db.QueryRowContext(ctx,
		`SELECT tg_message_id, release_url FROM releases_seen WHERE package = 'feed' AND version = 'b'`).
		Scan(&msgID, &url); err != nil {
		t.Fatalf("query b: %v", err)
	}
	if msgID != int64(1) || url != "https://old.example/b" {
		t.Errorf("OR IGNORE overwrote existing row: msg=%v url=%v", msgID, url)
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT tg_message_id, release_url FROM releases_seen WHERE package = 'feed' AND version = 'c'`).
		Scan(&msgID, &url); err != nil {
		t.Fatalf("query c: %v", err)
	}
	if msgID != int64(42) || url != nil {
		t.Errorf("row c: msg=%v url=%v, want 42 and NULL", msgID, url)
	}

	// Empty keys are rejected up front: SQLite NOT NULL accepts "", and a
	// ("pkg", "") row would permanently disable seeding for the package.
	if err := s.Releases.RecordSeen(ctx, "feed3", "", 0, ""); err == nil {
		t.Error("RecordSeen accepted an empty version")
	}
	if err := s.Releases.RecordSeenBatch(ctx, []SeenRelease{{Package: "feed3", Version: "1"}, {Package: "", Version: "2"}}, 0); err == nil {
		t.Error("RecordSeenBatch accepted an empty package")
	}
	if _, err := s.Releases.GetLatestSeen(ctx, "feed3"); err != ErrNotFound {
		t.Errorf("rejected batch left rows behind: %v", err)
	}

	// A cancelled context fails before BeginTx and leaves nothing behind.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	err := s.Releases.RecordSeenBatch(cancelled, []SeenRelease{{Package: "feed2", Version: "x"}, {Package: "feed2", Version: "y"}}, 0)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if _, err := s.Releases.GetLatestSeen(ctx, "feed2"); err != ErrNotFound {
		t.Errorf("failed batch left rows behind: %v", err)
	}
}

func TestReleasesHasSeen(t *testing.T) {
	t.Parallel()
	s := openMemory(t)
	ctx := context.Background()
	pkg := "@anthropic-ai/claude-code"

	has, err := s.Releases.HasSeen(ctx, pkg, "2.1.119")
	if err != nil {
		t.Fatalf("HasSeen on empty: %v", err)
	}
	if has {
		t.Error("HasSeen returned true for empty table")
	}

	if err := s.Releases.RecordSeen(ctx, pkg, "2.1.119", 1, ""); err != nil {
		t.Fatalf("RecordSeen: %v", err)
	}

	has, err = s.Releases.HasSeen(ctx, pkg, "2.1.119")
	if err != nil {
		t.Fatalf("HasSeen recorded: %v", err)
	}
	if !has {
		t.Error("HasSeen returned false for recorded version")
	}

	// A newer row for a different version must not mask the older one:
	// HasSeen looks up by (package, version), not "most recent".
	if err := s.Releases.RecordSeen(ctx, pkg, "2.1.120", 2, ""); err != nil {
		t.Fatalf("RecordSeen 2: %v", err)
	}
	has, err = s.Releases.HasSeen(ctx, pkg, "2.1.119")
	if err != nil {
		t.Fatalf("HasSeen older: %v", err)
	}
	if !has {
		t.Error("HasSeen returned false for older version after a newer one was recorded")
	}

	has, err = s.Releases.HasSeen(ctx, pkg, "2.1.121")
	if err != nil {
		t.Fatalf("HasSeen unknown: %v", err)
	}
	if has {
		t.Error("HasSeen returned true for unrecorded version")
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
