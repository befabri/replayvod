package database_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/befabri/replayvod/server/internal/database"
)

func holdSQLiteWriter(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "contended.db")
	open := func() *sql.DB {
		db, err := database.NewSQLiteDB(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		return db
	}
	holder, waiter := open(), open()
	conn, err := holder.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	release := sync.OnceFunc(func() {
		if _, err := conn.ExecContext(context.Background(), "ROLLBACK"); err != nil {
			t.Errorf("release competing writer: %v", err)
		}
		conn.Close()
	})
	t.Cleanup(release)
	return waiter, release
}

func TestSQLiteMigrationLockWaitsBeyondBusyTimeout(t *testing.T) {
	db, release := holdSQLiteWriter(t)
	const busyTimeout = 20
	execMigrationSQL(t, db, "PRAGMA busy_timeout = 20")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	files := fstest.MapFS{
		"001_lock_probe.up.sql": {Data: []byte(`CREATE TABLE lock_probe AS SELECT timeout FROM pragma_busy_timeout;`)},
	}
	result := make(chan error, 1)
	go func() { result <- database.MigrateSQLite(ctx, db, files) }()
	select {
	case err := <-result:
		t.Fatalf("migration stopped while the competing writer was still active: %v", err)
	case <-time.After(10 * busyTimeout * time.Millisecond):
	}
	release()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("migration after competing writer released: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("migration did not acquire the released lock")
	}
	assertCount(t, db, "SELECT timeout FROM lock_probe", busyTimeout)
	assertCount(t, db, "PRAGMA busy_timeout", busyTimeout)
	assertCount(t, db, "PRAGMA foreign_keys", 1)
	assertCount(t, db, "SELECT COUNT(*) FROM schema_migrations", 1)
	if err := database.MigrateSQLite(ctx, db, files); err != nil {
		t.Fatalf("reuse migrated connection: %v", err)
	}
	assertCount(t, db, "SELECT COUNT(*) FROM lock_probe", 1)
}

func TestSQLiteMigrationLockCancellationIsPromptAndReusable(t *testing.T) {
	db, release := holdSQLiteWriter(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- database.MigrateSQLite(ctx, db, fstest.MapFS{}) }()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for db.Stats().InUse == 0 {
		select {
		case err := <-result:
			t.Fatalf("migration stopped before cancellation: %v", err)
		case <-deadline.C:
			t.Fatal("migration did not reserve its connection")
		case <-time.After(time.Millisecond):
		}
	}
	select {
	case err := <-result:
		t.Fatalf("migration stopped before cancellation: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled lock acquisition: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation waited for SQLite's busy timeout")
	}
	release()
	assertCount(t, db, "PRAGMA busy_timeout", 5000)
	assertCount(t, db, "PRAGMA foreign_keys", 1)
	assertCount(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE name = 'schema_migrations'", 0)
	if err := database.MigrateSQLite(context.Background(), db, fstest.MapFS{}); err != nil {
		t.Fatalf("reuse after canceled lock acquisition: %v", err)
	}
	assertCount(t, db, "SELECT COUNT(*) FROM schema_migrations", 0)
}

func TestSQLiteMigrationLockRestoresSettingsAfterMigrationFailure(t *testing.T) {
	db := newUnmigratedSQLiteDB(t)
	execMigrationSQL(t, db, "PRAGMA busy_timeout = 137")
	files := fstest.MapFS{
		"001_lock_probe.up.sql": {Data: []byte(`
			CREATE TABLE lock_probe AS SELECT timeout FROM pragma_busy_timeout;
			INSERT INTO missing_target VALUES (1);
		`)},
	}
	if err := database.MigrateSQLite(context.Background(), db, files); err == nil || !strings.Contains(err.Error(), "001_lock_probe") {
		t.Fatalf("invalid migration: %v", err)
	}
	assertCount(t, db, "PRAGMA busy_timeout", 137)
	assertCount(t, db, "PRAGMA foreign_keys", 1)
	assertCount(t, db, "SELECT COUNT(*) FROM schema_migrations", 0)
	assertCount(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE name = 'lock_probe'", 0)
	files["001_lock_probe.up.sql"].Data = []byte(`CREATE TABLE lock_probe AS SELECT timeout FROM pragma_busy_timeout;`)
	if err := database.MigrateSQLite(context.Background(), db, files); err != nil {
		t.Fatalf("reuse after migration rollback: %v", err)
	}
	assertCount(t, db, "SELECT timeout FROM lock_probe", 137)
}
