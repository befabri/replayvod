package database

import (
	"context"
	"crypto/sha256"
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

// MigrationChangedError reports an applied migration whose file differs from
// its recorded checksum. Startup stops until the original file is restored.
type MigrationChangedError struct {
	Version   string
	AppliedAt string
}

func (e *MigrationChangedError) Error() string {
	return fmt.Sprintf("migration %s changed after it was applied on %s: "+
		"restore the original migration file and rebuild; "+
		"to roll back the database, restore a backup with its matching server version",
		e.Version, e.AppliedAt)
}

// MigrationMissingError reports applied migrations absent from this build.
// Startup stops until a build with the complete original history is used.
type MigrationMissingError struct {
	Versions []string
}

func (e *MigrationMissingError) Error() string {
	return fmt.Sprintf("database has applied migrations this build does not include: %s; "+
		"use a server build with the complete original migration history; "+
		"to roll back the database, restore a backup with its matching server version",
		strings.Join(e.Versions, ", "))
}

// MigratePostgres validates the applied migration history and applies pending
// migrations on a PostgreSQL database.
// Each migration is wrapped in a transaction so a partial apply cannot leave
// the DB in an intermediate state.
func MigratePostgres(ctx context.Context, pool *pgxpool.Pool, migrations fs.FS) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	return runMigrations(ctx, migrations, func(ctx context.Context, fn func(ledger) error) error {
		return withPostgresMigrationTx(ctx, conn, func(tx pgx.Tx) error {
			return fn(postgresLedger{tx})
		})
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

// MigrateSQLite validates the applied migration history and applies pending
// migrations on a SQLite database.
// Each migration is wrapped in a transaction.
func MigrateSQLite(ctx context.Context, db *sql.DB, migrations fs.FS) error {
	return runMigrations(ctx, migrations, func(ctx context.Context, fn func(ledger) error) error {
		return withSQLiteMigrationTx(ctx, db, func(conn *sql.Conn) error {
			return fn(sqliteLedger{conn})
		})
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

// ledger is the schema_migrations table seen through one locked transaction.
type ledger interface {
	bootstrap(ctx context.Context) error
	entries(ctx context.Context) ([]ledgerEntry, error)
	apply(ctx context.Context, statements string) error
	record(ctx context.Context, version, checksum string) error
	stamp(ctx context.Context, version, checksum string) error
}

type ledgerEntry struct {
	version   string
	appliedAt string
	// checksum is nil for rows written before checksums were recorded.
	checksum *string
}

type postgresLedger struct{ tx pgx.Tx }

func (l postgresLedger) bootstrap(ctx context.Context) error {
	if _, err := l.tx.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			checksum TEXT
		)
	`); err != nil {
		return err
	}
	_, err := l.tx.Exec(ctx, "ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS checksum TEXT")
	return err
}

func (l postgresLedger) entries(ctx context.Context) ([]ledgerEntry, error) {
	rows, err := l.tx.Query(ctx, "SELECT version, CAST(applied_at AS TEXT), checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ledgerEntry, error) {
		var entry ledgerEntry
		err := row.Scan(&entry.version, &entry.appliedAt, &entry.checksum)
		return entry, err
	})
}

func (l postgresLedger) apply(ctx context.Context, statements string) error {
	_, err := l.tx.Exec(ctx, statements)
	return err
}

func (l postgresLedger) record(ctx context.Context, version, checksum string) error {
	_, err := l.tx.Exec(ctx, "INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)", version, checksum)
	return err
}

func (l postgresLedger) stamp(ctx context.Context, version, checksum string) error {
	_, err := l.tx.Exec(ctx, "UPDATE schema_migrations SET checksum = $1 WHERE version = $2", checksum, version)
	return err
}

type sqliteLedger struct{ conn *sql.Conn }

func (l sqliteLedger) bootstrap(ctx context.Context) error {
	if _, err := l.conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now')),
			checksum TEXT
		)
	`); err != nil {
		return err
	}
	var hasChecksum int
	if err := l.conn.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM pragma_table_info('schema_migrations') WHERE name = 'checksum'",
	).Scan(&hasChecksum); err != nil {
		return err
	}
	if hasChecksum > 0 {
		return nil
	}
	_, err := l.conn.ExecContext(ctx, "ALTER TABLE schema_migrations ADD COLUMN checksum TEXT")
	return err
}

func (l sqliteLedger) entries(ctx context.Context) ([]ledgerEntry, error) {
	rows, err := l.conn.QueryContext(ctx, "SELECT version, applied_at, checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []ledgerEntry
	for rows.Next() {
		var entry ledgerEntry
		if err := rows.Scan(&entry.version, &entry.appliedAt, &entry.checksum); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (l sqliteLedger) apply(ctx context.Context, statements string) error {
	_, err := l.conn.ExecContext(ctx, statements)
	return err
}

func (l sqliteLedger) record(ctx context.Context, version, checksum string) error {
	_, err := l.conn.ExecContext(ctx, "INSERT INTO schema_migrations (version, checksum) VALUES (?, ?)", version, checksum)
	return err
}

func (l sqliteLedger) stamp(ctx context.Context, version, checksum string) error {
	_, err := l.conn.ExecContext(ctx, "UPDATE schema_migrations SET checksum = ? WHERE version = ?", checksum, version)
	return err
}

type migration struct {
	version  string
	content  string
	checksum string
}

// readMigrations returns the .up.sql files of the filesystem sorted by name.
// Rollback files (.down.sql) are ignored.
func readMigrations(files fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var migrations []migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		content, err := fs.ReadFile(files, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		migrations = append(migrations, migration{
			version:  strings.TrimSuffix(entry.Name(), ".up.sql"),
			content:  string(content),
			checksum: fmt.Sprintf("%x", sha256.Sum256(content)),
		})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	return migrations, nil
}

// runMigrations validates the complete applied history before changing it.
// Each pending migration rechecks that history under its transaction lock,
// since another process may have migrated between commits.
func runMigrations(ctx context.Context, files fs.FS, inTx func(context.Context, func(ledger) error) error) error {
	migrations, err := readMigrations(files)
	if err != nil {
		return err
	}
	known := make(map[string]migration, len(migrations))
	for _, m := range migrations {
		known[m.version] = m
	}
	if err := inTx(ctx, func(l ledger) error {
		if err := l.bootstrap(ctx); err != nil {
			return fmt.Errorf("create schema_migrations: %w", err)
		}
		_, err := validateMigrationHistory(ctx, l, known)
		return err
	}); err != nil {
		return err
	}

	stamped := 0
	for {
		var applied bool
		var stampedInTx int
		err := inTx(ctx, func(l ledger) error {
			var err error
			applied, stampedInTx, err = applyNextMigration(ctx, l, migrations, known)
			return err
		})
		if err != nil {
			return err
		}
		stamped += stampedInTx
		if !applied {
			break
		}
	}
	if stamped > 0 {
		slog.Info("Recorded checksums for previously applied migrations", "count", stamped)
	}

	if len(migrations) > 0 {
		slog.Info("Migrations complete", "checked", len(migrations))
	}
	return nil
}

// applyNextMigration stamps checksumless rows and applies at most one pending
// migration, after validating every recorded version in the same transaction.
func applyNextMigration(ctx context.Context, l ledger, migrations []migration, known map[string]migration) (applied bool, stamped int, err error) {
	history, err := validateMigrationHistory(ctx, l, known)
	if err != nil {
		return false, 0, err
	}
	for _, m := range migrations {
		if entry, exists := history[m.version]; exists {
			if entry.checksum == nil {
				if err := l.stamp(ctx, m.version, m.checksum); err != nil {
					return false, 0, fmt.Errorf("record checksum for migration %s: %w", m.version, err)
				}
				stamped++
			}
			continue
		}

		slog.Info("Applying migration", "version", m.version)
		if err := l.apply(ctx, m.content); err != nil {
			return false, 0, fmt.Errorf("migration %s failed: %w", m.version, err)
		}
		if err := l.record(ctx, m.version, m.checksum); err != nil {
			return false, 0, fmt.Errorf("record migration %s: %w", m.version, err)
		}
		return true, stamped, nil
	}
	return false, stamped, nil
}

// validateMigrationHistory checks every ledger row before any migration SQL or
// checksum writes. Rows from published builds without checksums can only be
// checked for a matching filename until their first checksum is recorded.
func validateMigrationHistory(ctx context.Context, l ledger, known map[string]migration) (map[string]ledgerEntry, error) {
	entries, err := l.entries(ctx)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	history := make(map[string]ledgerEntry, len(entries))
	var missing []string
	for _, entry := range entries {
		m, exists := known[entry.version]
		if !exists {
			missing = append(missing, entry.version)
			continue
		}
		if entry.checksum != nil && *entry.checksum != m.checksum {
			return nil, &MigrationChangedError{Version: entry.version, AppliedAt: entry.appliedAt}
		}
		history[entry.version] = entry
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, &MigrationMissingError{Versions: missing}
	}
	return history, nil
}
