package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// NewSQLiteDB opens a SQLite database with WAL mode + foreign-key
// enforcement enabled.
//
// Why set foreign_keys via Exec rather than the DSN: modernc.org/sqlite
// silently drops the `_foreign_keys` DSN parameter, so relying on it
// leaves cascade deletes as no-ops (caught by
// TestSettings_UserCascadeDelete). MaxOpenConns=1 keeps the pragma
// sticky — without it, a fresh connection from the pool could reset
// the setting mid-session.
func NewSQLiteDB(path string) (*sql.DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sqlite directory: %w", err)
	}

	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping sqlite: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if _, err := db.ExecContext(context.Background(), "PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	return db, nil
}
