package database_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"modernc.org/sqlite"

	"github.com/befabri/replayvod/server/internal/database"
)

func TestSQLiteMigrationLockDiscardsUncertainConnections(t *testing.T) {
	restoreFailure := errors.New("restore timeout failed")
	beginFailure := errors.New("non-busy BEGIN failure")
	for _, tc := range []struct {
		name, query  string
		after        bool
		failure      error
		wantRestored int64
	}{
		{"interrupted timeout change", "PRAGMA busy_timeout = 0", true, context.Canceled, 1},
		{"interrupted acquired transaction", "BEGIN IMMEDIATE", true, context.Canceled, 1},
		{"non-busy BEGIN failure", "BEGIN IMMEDIATE", false, beginFailure, 1},
		{"failed timeout restoration", "PRAGMA busy_timeout = 137", false, restoreFailure, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			path := (&url.URL{Scheme: "file", Path: filepath.ToSlash(filepath.Join(t.TempDir(), "fault.db"))}).String()
			connector := &sqliteMigrationFaultConnector{
				dsn:   path + "?_pragma=busy_timeout(137)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)",
				query: tc.query, after: tc.after, failure: tc.failure,
			}
			if errors.Is(tc.failure, context.Canceled) {
				connector.cancel = cancel
			}
			db := sql.OpenDB(connector)
			db.SetMaxOpenConns(1)
			db.SetMaxIdleConns(1)
			t.Cleanup(func() { _ = db.Close() })
			files := fstest.MapFS{
				"001_lock_probe.up.sql": {Data: []byte("CREATE TABLE lock_probe (id INTEGER PRIMARY KEY);")},
			}
			if err := database.MigrateSQLite(ctx, db, files); !errors.Is(err, tc.failure) {
				t.Fatalf("failed migration lock: %v, want %v", err, tc.failure)
			}
			if !connector.injected.Load() || connector.closed.Load() != 1 {
				t.Fatalf("uncertain connection retained: injected=%v closed=%d", connector.injected.Load(), connector.closed.Load())
			}
			if got := connector.restored.Load(); got != tc.wantRestored {
				t.Fatalf("successful timeout restorations = %d, want %d", got, tc.wantRestored)
			}
			assertCount(t, db, "PRAGMA busy_timeout", 137)
			assertCount(t, db, "PRAGMA foreign_keys", 1)
			assertCount(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE name = 'schema_migrations'", 0)
			retryCtx, retryCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer retryCancel()
			if err := database.MigrateSQLite(retryCtx, db, files); err != nil {
				t.Fatalf("reuse after discarded connection: %v", err)
			}
			assertCount(t, db, "SELECT COUNT(*) FROM schema_migrations", 1)
			assertCount(t, db, "SELECT COUNT(*) FROM lock_probe", 0)
		})
	}
}

type sqliteMigrationFaultConnector struct {
	dsn, query string
	after      bool
	failure    error
	cancel     context.CancelFunc
	injected   atomic.Bool
	closed     atomic.Int64
	restored   atomic.Int64
}

func (c *sqliteMigrationFaultConnector) Driver() driver.Driver { return &sqlite.Driver{} }

func (c *sqliteMigrationFaultConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.Driver().Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &sqliteMigrationFaultConn{Conn: conn, owner: c}, nil
}

type sqliteMigrationFaultConn struct {
	driver.Conn
	owner *sqliteMigrationFaultConnector
}

func (c *sqliteMigrationFaultConn) Close() error {
	c.owner.closed.Add(1)
	return c.Conn.Close()
}

func (c *sqliteMigrationFaultConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	exec := c.Conn.(driver.ExecerContext)
	if query == c.owner.query && c.owner.injected.CompareAndSwap(false, true) {
		if c.owner.after {
			if _, err := exec.ExecContext(ctx, query, args); err != nil {
				return nil, err
			}
		}
		if c.owner.cancel != nil {
			c.owner.cancel()
		}
		return nil, c.owner.failure
	}
	result, err := exec.ExecContext(ctx, query, args)
	if query == "PRAGMA busy_timeout = 137" && err == nil {
		c.owner.restored.Add(1)
	}
	return result, err
}
