package testdb

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/befabri/replayvod/server/internal/database"
	"github.com/befabri/replayvod/server/migrations"
)

// NewSQLiteDB returns a fresh migrated SQLite database backed by a tempfile
// under t.TempDir(). The DB closes on t.Cleanup; the tempfile is cleaned
// up by t.TempDir itself.
//
// Tempfile rather than :memory: because modernc.org/sqlite gives each
// connection its own private DB under :memory:, which silently breaks
// isolation whenever the pool opens more than one connection.
func NewSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.NewSQLiteDB(dbPath)
	if err != nil {
		t.Fatalf("testdb: open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := database.MigrateSQLite(context.Background(), db, migrations.SQLite()); err != nil {
		t.Fatalf("testdb: migrate sqlite: %v", err)
	}
	return db
}
