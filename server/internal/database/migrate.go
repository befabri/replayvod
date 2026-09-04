package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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

	if err := withPostgresMigrationTx(ctx, conn, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
		`)
		return err
	}); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	return runMigrations(migrations, func(version, content string) error {
		if err := withPostgresMigrationTx(ctx, conn, func(tx pgx.Tx) error {
			var exists bool
			if err := tx.QueryRow(ctx,
				"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)",
				version,
			).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return nil
			}

			slog.Info("Applying migration", "version", version)
			if _, err := tx.Exec(ctx, content); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version)
			return err
		}); err != nil {
			return fmt.Errorf("migration %s failed: %w", version, err)
		}
		return nil
	})
}

// withPostgresMigrationTx locks before checking or creating the migration
// ledger. The database-scoped transaction lock cannot leak into the pool after
// rollback or cancellation.
func withPostgresMigrationTx(ctx context.Context, conn *pgxpool.Conn, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		const migrationLockKey int64 = 0x7265706c6179766f // "replayvo"
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrationLockKey); err != nil {
			return fmt.Errorf("lock migrations: %w", err)
		}
		return fn(tx)
	})
}

// MigrateSQLite runs pending migrations on a SQLite database.
// Each migration is wrapped in a transaction.
func MigrateSQLite(ctx context.Context, db *sql.DB, migrations fs.FS) error {
	if err := withSQLiteMigrationTx(ctx, db, func(conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
		`)
		return err
	}); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	return runMigrations(migrations, func(version, content string) error {
		if err := withSQLiteMigrationTx(ctx, db, func(conn *sql.Conn) error {
			var count int
			if err := conn.QueryRowContext(ctx,
				"SELECT COUNT(*) FROM schema_migrations WHERE version = ?",
				version,
			).Scan(&count); err != nil {
				return err
			}
			if count > 0 {
				return nil
			}

			slog.Info("Applying migration", "version", version)
			if _, err := conn.ExecContext(ctx, content); err != nil {
				return err
			}
			_, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", version)
			return err
		}); err != nil {
			return fmt.Errorf("migration %s failed: %w", version, err)
		}
		return nil
	})
}

// withSQLiteMigrationTx takes the write lock before reading the ledger on one
// connection. A deferred BeginTx can read stale versions and then fail to
// acquire the write lock.
func withSQLiteMigrationTx(ctx context.Context, db *sql.DB, fn func(*sql.Conn) error) (err error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		return fmt.Errorf("lock migrations: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		// Roll back even after cancellation so the pool never receives
		// an open transaction.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, rollbackErr := conn.ExecContext(cleanupCtx, "ROLLBACK"); rollbackErr != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			err = errors.Join(err, fmt.Errorf("rollback migration: %w", rollbackErr))
		}
	}()
	if err := fn(conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	committed = true
	return nil
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
