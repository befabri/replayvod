package database

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// NewSQLiteDB opens path with WAL, a busy timeout, and foreign-key enforcement
// on every connection, including replacements after errors. modernc.org/sqlite
// requires _pragma options; _journal_mode and _busy_timeout are ignored.
func NewSQLiteDB(path string) (*sql.DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sqlite directory: %w", err)
	}

	dsn := path
	if path != ":memory:" {
		absolutePath, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve sqlite path: %w", err)
		}
		dsn = (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolutePath)}).String()
	}
	pragmas := url.Values{"_pragma": {"busy_timeout(5000)", "foreign_keys(1)", "journal_mode(WAL)"}}
	db, err := sql.Open("sqlite", dsn+"?"+pragmas.Encode())
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping sqlite: %w", err)
	}

	return db, nil
}
