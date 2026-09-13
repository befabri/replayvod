package testdb

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/befabri/replayvod/server/internal/database"
	"github.com/befabri/replayvod/server/migrations"
)

// migratedSQLite caches a closed database image so fixtures avoid repeating historical table rebuilds.
var migratedSQLite = sync.OnceValues(func() ([]byte, error) {
	dir, err := os.MkdirTemp("", "replayvod-test-schema-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "schema.db")
	db, err := database.NewSQLiteDB(path)
	if err != nil {
		return nil, err
	}
	if err := database.MigrateSQLite(context.Background(), db, migrations.SQLite()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := db.Close(); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
})

// NewSQLiteDB returns an isolated migrated database and closes it during test cleanup.
func NewSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()
	image, err := migratedSQLite()
	if err != nil {
		t.Fatalf("testdb: migrate sqlite template: %v", err)
	}
	path := filepath.Join(t.TempDir(), "test.db")
	if err := os.WriteFile(path, image, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := database.NewSQLiteDB(path)
	if err != nil {
		t.Fatalf("testdb: open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
