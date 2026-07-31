package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

//go:embed migrations/*.up.sql migrations/*.down.sql
var embeddedMigrations embed.FS

const migrationAdvisoryLock int64 = 0x44504D4947524154

func ApplyMigrations(ctx context.Context, db *sql.DB) error {
	return withMigrationLock(ctx, db, func(ctx context.Context, conn *sql.Conn) error {
		if err := ensureMigrationTable(ctx, conn); err != nil {
			return err
		}
		names, err := migrationNames(".up.sql")
		if err != nil {
			return err
		}
		for _, name := range names {
			version := strings.TrimSuffix(name, ".up.sql")
			var exists bool
			if err := conn.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&exists); err != nil {
				return fmt.Errorf("check migration %s: %w", version, err)
			}
			if exists {
				continue
			}
			if err := applyMigration(ctx, conn, name, version); err != nil {
				return err
			}
		}
		return nil
	})
}

// ValidateMigrationState ensures an installed database exactly matches the
// migrations embedded in the running binary without changing database state.
func ValidateMigrationState(ctx context.Context, db *sql.DB) error {
	expectedNames, err := migrationNames(".up.sql")
	if err != nil {
		return err
	}
	expected := make(map[string]struct{}, len(expectedNames))
	for _, name := range expectedNames {
		expected[strings.TrimSuffix(name, ".up.sql")] = struct{}{}
	}

	rows, err := db.QueryContext(ctx, `SELECT version::text FROM schema_migrations ORDER BY version::text`)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()
	applied := make(map[string]struct{}, len(expected))
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied[version] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}

	var missing []string
	for version := range expected {
		if _, ok := applied[version]; !ok {
			missing = append(missing, version)
		}
	}
	var unknown []string
	for version := range applied {
		if _, ok := expected[version]; !ok {
			unknown = append(unknown, version)
		}
	}
	sort.Strings(missing)
	sort.Strings(unknown)
	if len(missing) != 0 || len(unknown) != 0 {
		return fmt.Errorf("migration state mismatch: missing=%v unknown=%v", missing, unknown)
	}
	return nil
}

func RollbackLastMigration(ctx context.Context, db *sql.DB) error {
	return withMigrationLock(ctx, db, func(ctx context.Context, conn *sql.Conn) error {
		if err := ensureMigrationTable(ctx, conn); err != nil {
			return err
		}
		var version string
		if err := conn.QueryRowContext(ctx, `
			SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1
		`).Scan(&version); err != nil {
			if err == sql.ErrNoRows {
				return nil
			}
			return fmt.Errorf("find last migration: %w", err)
		}
		name := version + ".down.sql"
		content, err := embeddedMigrations.ReadFile(path.Join("migrations", name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin rollback %s: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("rollback migration %s: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = $1`, version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("delete migration %s: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit rollback %s: %w", version, err)
		}
		return nil
	})
}

func withMigrationLock(ctx context.Context, db *sql.DB, fn func(context.Context, *sql.Conn) error) (resultErr error) {
	migrationCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	conn, err := db.Conn(migrationCtx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(migrationCtx, `SELECT pg_advisory_lock($1)`, migrationAdvisoryLock); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		unlockCtx, unlockCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer unlockCancel()
		if _, unlockErr := conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock($1)`, migrationAdvisoryLock); unlockErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("release migration lock: %w", unlockErr))
		}
	}()
	return fn(migrationCtx, conn)
}

func ensureMigrationTable(ctx context.Context, conn *sql.Conn) error {
	var dirtyColumn bool
	if err := conn.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = 'schema_migrations' AND column_name = 'dirty'
		)
	`).Scan(&dirtyColumn); err != nil {
		return fmt.Errorf("inspect schema_migrations: %w", err)
	}
	if dirtyColumn {
		return fmt.Errorf("schema_migrations is owned by golang-migrate; use the repository migration runner exclusively")
	}
	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	return nil
}

func migrationNames(suffix string) ([]string, error) {
	entries, err := embeddedMigrations.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), suffix) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func applyMigration(ctx context.Context, conn *sql.Conn, name, version string) error {
	content, err := embeddedMigrations.ReadFile(path.Join("migrations", name))
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("apply migration %s: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("record migration %s: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", version, err)
	}
	return nil
}
