package database

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MigratePostgres runs pending migrations on a PostgreSQL database.
// Each migration is wrapped in a transaction so a partial apply cannot leave
// the DB in an intermediate state.
func MigratePostgres(ctx context.Context, pool *pgxpool.Pool, migrations fs.FS) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	return runMigrations(migrations, func(version, content string) error {
		var exists bool
		if err := conn.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)",
			version,
		).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return nil
		}

		slog.Info("Applying migration", "version", version)

		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", version, err)
		}
		defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

		if _, err := tx.Exec(ctx, content); err != nil {
			return fmt.Errorf("migration %s failed: %w", version, err)
		}
		if _, err := tx.Exec(ctx,
			"INSERT INTO schema_migrations (version) VALUES ($1)",
			version,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit %s: %w", version, err)
		}
		return nil
	})
}

// MigrateSQLite runs pending migrations on a SQLite database.
// Each migration is wrapped in a transaction.
func MigrateSQLite(ctx context.Context, db *sql.DB, migrations fs.FS) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	return runMigrations(migrations, func(version, content string) error {
		var count int
		if err := db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM schema_migrations WHERE version = ?",
			version,
		).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return nil
		}

		slog.Info("Applying migration", "version", version)

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", version, err)
		}
		defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

		if _, err := tx.ExecContext(ctx, content); err != nil {
			return fmt.Errorf("migration %s failed: %w", version, err)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations (version) VALUES (?)",
			version,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", version, err)
		}
		return nil
	})
}

// runMigrations reads .up.sql files from the given filesystem (sorted by name)
// and calls apply for each. Rollback files (.down.sql) are ignored here.
func runMigrations(migrations fs.FS, apply func(version, content string) error) error {
	entries, err := fs.ReadDir(migrations, ".")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	var upFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			upFiles = append(upFiles, entry.Name())
		}
	}
	sort.Strings(upFiles)

	for _, file := range upFiles {
		version := strings.TrimSuffix(file, ".up.sql")
		content, err := fs.ReadFile(migrations, file)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", file, err)
		}
		if err := apply(version, string(content)); err != nil {
			return err
		}
	}

	if len(upFiles) > 0 {
		slog.Info("Migrations complete", "checked", len(upFiles))
	}
	return nil
}
