package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MigratePostgres runs pending migrations on a PostgreSQL database.
func MigratePostgres(ctx context.Context, pool *pgxpool.Pool, migrationsDir string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	// Create migration tracking table
	_, err = conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	return runMigrations(migrationsDir, func(version, content string) error {
		var exists bool
		err := conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", version).Scan(&exists)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}

		slog.Info("Applying migration", "version", version)
		if _, err := conn.Exec(ctx, content); err != nil {
			return fmt.Errorf("migration %s failed: %w", version, err)
		}
		if _, err := conn.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
			return fmt.Errorf("failed to record migration %s: %w", version, err)
		}
		return nil
	})
}

// MigrateSQLite runs pending migrations on a SQLite database.
func MigrateSQLite(ctx context.Context, db *sql.DB, migrationsDir string) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	return runMigrations(migrationsDir, func(version, content string) error {
		var count int
		err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&count)
		if err != nil {
			return err
		}
		if count > 0 {
			return nil
		}

		slog.Info("Applying migration", "version", version)
		if _, err := db.ExecContext(ctx, content); err != nil {
			return fmt.Errorf("migration %s failed: %w", version, err)
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
			return fmt.Errorf("failed to record migration %s: %w", version, err)
		}
		return nil
	})
}

// runMigrations reads .up.sql files from a directory and runs the apply function for each.
func runMigrations(dir string, apply func(version, content string) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read migrations directory %s: %w", dir, err)
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
		content, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %w", file, err)
		}
		if err := apply(version, string(content)); err != nil {
			return err
		}
	}

	applied := len(upFiles)
	if applied > 0 {
		slog.Info("Migrations complete", "dir", dir, "checked", applied)
	}
	return nil
}
