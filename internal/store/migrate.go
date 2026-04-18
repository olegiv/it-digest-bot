package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/olegiv/it-digest-bot/migrations"
)

type migration struct {
	version int
	name    string
	sql     string
}

// Migrate applies all pending migrations in version order. Each migration
// file is applied inside its own transaction and recorded in the
// schema_migrations table on success.
func (s *Store) Migrate(ctx context.Context) error {
	return migrate(ctx, s.db, migrations.FS, ".")
}

func migrate(ctx context.Context, db *sql.DB, efs fs.FS, root string) error {
	if _, err := db.ExecContext(ctx, `
        CREATE TABLE IF NOT EXISTS schema_migrations (
            version     INTEGER PRIMARY KEY,
            applied_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
        );`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return err
	}

	all, err := loadMigrations(efs, root)
	if err != nil {
		return err
	}

	for _, m := range all {
		if applied[m.version] {
			continue
		}
		if err := applyOne(ctx, db, m); err != nil {
			return fmt.Errorf("apply %s: %w", m.name, err)
		}
	}
	return nil
}

func appliedVersions(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		out[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations: %w", err)
	}
	return out, nil
}

func loadMigrations(efs fs.FS, root string) ([]migration, error) {
	entries, err := fs.ReadDir(efs, root)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		v, err := parseVersion(e.Name())
		if err != nil {
			return nil, fmt.Errorf("parse migration name %q: %w", e.Name(), err)
		}
		path := e.Name()
		if root != "." && root != "" {
			path = root + "/" + e.Name()
		}
		b, err := fs.ReadFile(efs, path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		out = append(out, migration{version: v, name: e.Name(), sql: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	if err := checkDuplicateVersions(out); err != nil {
		return nil, err
	}
	return out, nil
}

func parseVersion(name string) (int, error) {
	prefix, _, ok := strings.Cut(name, "_")
	if !ok {
		return 0, errors.New("expected NNNN_name.sql")
	}
	return strconv.Atoi(prefix)
}

func checkDuplicateVersions(ms []migration) error {
	for i := 1; i < len(ms); i++ {
		if ms[i].version == ms[i-1].version {
			return fmt.Errorf("duplicate migration version %d (%s and %s)",
				ms[i].version, ms[i-1].name, ms[i].name)
		}
	}
	return nil
}

func applyOne(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("exec sql: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations(version) VALUES (?)`, m.version); err != nil {
		return fmt.Errorf("record version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
