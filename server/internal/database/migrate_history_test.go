package database

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"
	"time"
)

// Use two real connections and pause the first runner between commits so a
// different build can advance the history without relying on goroutine timing.
func TestMigrationsRecheckHistoryBetweenCommits(t *testing.T) {
	for _, kind := range []string{"changed", "missing"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			path := filepath.Join(t.TempDir(), "history.sqlite")
			db, err := NewSQLiteDB(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			other, err := NewSQLiteDB(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = other.Close() })
			files := fstest.MapFS{
				"001_probe.up.sql":        &fstest.MapFile{Data: []byte("CREATE TABLE probe (id INTEGER PRIMARY KEY, value INTEGER); INSERT INTO probe VALUES (1, 42);")},
				"002_probe_update.up.sql": &fstest.MapFile{Data: []byte("UPDATE probe SET value = 999;")},
				"003_probe_index.up.sql":  &fstest.MapFile{Data: []byte("CREATE INDEX probe_value ON probe (value);")},
			}
			otherFiles := fstest.MapFS{"001_probe.up.sql": files["001_probe.up.sql"]}
			if kind == "changed" {
				otherFiles["003_probe_index.up.sql"] = &fstest.MapFile{Data: []byte("CREATE INDEX probe_value ON probe (value DESC);")}
			} else {
				otherFiles["004_newer_probe.up.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE newer_probe (id INTEGER PRIMARY KEY);")}
			}

			advanced := false
			err = runMigrations(ctx, files, func(ctx context.Context, fn func(ledger) error) error {
				if err := withSQLiteMigrationTx(ctx, db, func(conn *sql.Conn) error {
					return fn(sqliteLedger{conn})
				}); err != nil {
					return err
				}
				if advanced {
					return nil
				}
				var applied int
				if err := other.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = '001_probe'").Scan(&applied); err != nil {
					return err
				}
				if applied == 0 {
					return nil
				}
				advanced = true
				if err := MigrateSQLite(ctx, other, otherFiles); err != nil {
					t.Fatalf("other build advancing migration history: %v", err)
				}
				return nil
			})
			if !advanced {
				t.Fatal("other build never advanced the migration history")
			}
			if kind == "changed" {
				var changed *MigrationChangedError
				if !errors.As(err, &changed) || changed.Version != "003_probe_index" {
					t.Fatalf("history changed between commits = %v, want MigrationChangedError for 003_probe_index", err)
				}
			} else {
				var missing *MigrationMissingError
				if !errors.As(err, &missing) || !slices.Equal(missing.Versions, []string{"004_newer_probe"}) {
					t.Fatalf("history advanced between commits = %v, want MigrationMissingError for 004_newer_probe", err)
				}
			}
			var value, pending, recorded int
			if err := db.QueryRowContext(ctx, "SELECT value FROM probe WHERE id = 1").Scan(&value); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*), COUNT(CASE WHEN version = '002_probe_update' THEN 1 END) FROM schema_migrations").Scan(&recorded, &pending); err != nil {
				t.Fatal(err)
			}
			if value != 42 || pending != 0 || recorded != 2 {
				t.Fatalf("pending migration ran against incompatible history: value=%d, pending entries=%d, total entries=%d", value, pending, recorded)
			}
		})
	}
}
