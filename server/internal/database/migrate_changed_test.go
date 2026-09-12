package database_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/befabri/replayvod/server/internal/database"
	"github.com/befabri/replayvod/server/migrations"
)

func TestMigrationsChangedAfterApply(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		for _, installed := range []string{"044", "999"} {
			t.Run(backend+"/from_"+installed, func(t *testing.T) {
				h := newMigrationDB(t, backend)
				ctx := context.Background()
				if err := h.migrate(ctx, migrationsThrough(t, h.files, installed)); err != nil {
					t.Fatal(err)
				}
				tables := append(seedExistingInstallation(t, h.db, installed), "schema_migrations")
				if installed == "999" {
					execMigrationSQL(t, h.db, `UPDATE video_user_states SET progress_revision = 23`)
				}
				before := snapshotMigrationTables(t, h.db, tables)

				edited := migrationsThrough(t, h.files, "999")
				edited["044_user_states.up.sql"].Data = append(edited["044_user_states.up.sql"].Data,
					[]byte("\nCREATE TABLE drift_probe (id INTEGER PRIMARY KEY);")...)
				err := h.migrate(ctx, edited)
				var changed *database.MigrationChangedError
				if !errors.As(err, &changed) || changed.Version != "044_user_states" || changed.AppliedAt == "" {
					t.Fatalf("upgrade with an edited applied migration = %v, want MigrationChangedError for 044_user_states", err)
				}
				for _, want := range []string{"restore the original migration file", changed.AppliedAt} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not mention %q", err, want)
					}
				}
				for _, unsafe := range []string{".down.sql", "DELETE", "UPDATE schema_migrations"} {
					if strings.Contains(err.Error(), unsafe) {
						t.Errorf("error suggests bypassing migration history: %s", err)
					}
				}
				// Later migrations must not build on a schema the build
				// does not know.
				assertMigrationTablesUnchanged(t, h.db, before)
				if installed == "044" {
					assertTableMissing(t, h.db, "invites")
				}
				assertTableMissing(t, h.db, "drift_probe")

				// Restore the original file. A later migration's schema and saved
				// progress must survive recovery without rolling back user-state tables.
				if err := h.migrate(ctx, h.files); err != nil {
					t.Fatalf("upgrade after restoring the original migration: %v", err)
				}
				assertMigrationLedgerPreserved(t, h.db, before["schema_migrations"])
				delete(before, "schema_migrations")
				assertMigrationTablesUnchanged(t, h.db, before)
				assertTableMissing(t, h.db, "drift_probe")
				assertCount(t, h.db, "SELECT COUNT(*) FROM schema_migrations WHERE checksum IS NULL", 0)
			})
		}
	}
}

func TestMigrationsFileEditsAreDetected(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		cases := []struct{ name, original, edited string }{
			{"leading_comment", "CREATE TABLE probe(id INTEGER);", "-- comment\nCREATE TABLE probe(id INTEGER);"},
			{"inline_comment", "CREATE TABLE probe(id INTEGER);", "CREATE TABLE probe(/* identifier */id INTEGER);"},
			{"formatting", "CREATE TABLE probe(id INTEGER);", "CREATE TABLE probe(\n id INTEGER\n);\n"},
			{"identifier_spaces", `CREATE TABLE "probe  one" (id INTEGER);`, `CREATE TABLE "probe one" (id INTEGER);`},
			{"identifier_comments", `CREATE TABLE "probe -- before" (id INTEGER);`, `CREATE TABLE "probe -- after" (id INTEGER);`},
			{"string_literal", `CREATE TABLE probe(v TEXT DEFAULT 'it''s -- before');`, `CREATE TABLE probe(v TEXT DEFAULT 'it''s -- after');`},
		}
		if backend == "postgres" {
			cases = append(cases, struct{ name, original, edited string }{
				"dollar_quoted_literal", `CREATE TABLE probe(v TEXT DEFAULT $$left -- before$$);`, `CREATE TABLE probe(v TEXT DEFAULT $$left -- after$$);`,
			})
		}
		for _, tc := range cases {
			t.Run(backend+"/"+tc.name, func(t *testing.T) {
				h := newMigrationDB(t, backend)
				ctx := context.Background()
				original := fstest.MapFS{"001_probe.up.sql": &fstest.MapFile{Data: []byte(tc.original)}}
				if err := h.migrate(ctx, original); err != nil {
					t.Fatal(err)
				}
				before := snapshotMigrationTables(t, h.db, []string{"schema_migrations"})
				edited := fstest.MapFS{"001_probe.up.sql": &fstest.MapFile{Data: []byte(tc.edited)}}
				// Both files must be valid SQL; drift is not a SQL execution error.
				fresh := newMigrationDB(t, backend)
				if err := fresh.migrate(ctx, edited); err != nil {
					t.Fatal(err)
				}
				var changed *database.MigrationChangedError
				if err := h.migrate(ctx, edited); !errors.As(err, &changed) || changed.Version != "001_probe" {
					t.Fatalf("file edit = %v, want MigrationChangedError for 001_probe", err)
				}
				assertMigrationTablesUnchanged(t, h.db, before)
				if err := h.migrate(ctx, original); err != nil {
					t.Fatalf("restart with original file: %v", err)
				}
			})
		}
	}
}

func TestMigrationsLedgerMatchesManifest(t *testing.T) {
	manifest, err := migrations.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := context.Background()
			if err := h.migrate(ctx, h.files); err != nil {
				t.Fatal(err)
			}
			rows, err := h.db.QueryContext(ctx, "SELECT version, checksum FROM schema_migrations")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			for rows.Next() {
				var version, checksum string
				if err := rows.Scan(&version, &checksum); err != nil {
					t.Fatal(err)
				}
				if want := manifest[backend+"/"+version+".up.sql"]; checksum != want {
					t.Errorf("%s ledger checksum = %s, manifest = %s", version, checksum, want)
				}
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMigrationsLedgerWithoutChecksums(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := context.Background()
			appliedAt := "TIMESTAMPTZ NOT NULL DEFAULT NOW()"
			if backend == "sqlite" {
				appliedAt = "TEXT NOT NULL DEFAULT (datetime('now'))"
			}
			execMigrationSQL(t, h.db, "CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at "+appliedAt+")")
			execMigrationSQL(t, h.db, "INSERT INTO schema_migrations (version) VALUES ('001_probe')")
			const content = "CREATE TABLE probe (id INTEGER PRIMARY KEY, name TEXT);"
			execMigrationSQL(t, h.db, content)
			execMigrationSQL(t, h.db, "INSERT INTO probe (id, name) VALUES (42, 'preserved')")
			before := snapshotMigrationTables(t, h.db, []string{"schema_migrations", "probe"})

			files := fstest.MapFS{"001_probe.up.sql": &fstest.MapFile{Data: []byte(content)}}
			if err := h.migrate(ctx, files); err != nil {
				t.Fatalf("upgrade a ledger without checksums: %v", err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			assertCount(t, h.db, "SELECT COUNT(*) FROM schema_migrations WHERE checksum IS NOT NULL", 1)
			if err := h.migrate(ctx, files); err != nil {
				t.Fatalf("restart after recording checksums: %v", err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)

			files["001_probe.up.sql"].Data = []byte(content + "\nALTER TABLE probe ADD COLUMN changed INTEGER;")
			var changed *database.MigrationChangedError
			if err := h.migrate(ctx, files); !errors.As(err, &changed) || changed.Version != "001_probe" || changed.AppliedAt == "" {
				t.Fatalf("edit after the checksum was recorded = %v, want MigrationChangedError for 001_probe", err)
			}
		})
	}
}

func TestMigrationsChangedBeforePendingApply(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := context.Background()
			const originalIndex = "CREATE INDEX probe_value ON probe (value);"
			files := fstest.MapFS{
				"001_probe.up.sql":       &fstest.MapFile{Data: []byte("CREATE TABLE probe (id INTEGER PRIMARY KEY, value INTEGER); INSERT INTO probe VALUES (1, 42);")},
				"003_probe_index.up.sql": &fstest.MapFile{Data: []byte(originalIndex)},
			}
			if err := h.migrate(ctx, files); err != nil {
				t.Fatal(err)
			}
			// A rejected history must not stamp earlier rows either.
			execMigrationSQL(t, h.db, "UPDATE schema_migrations SET checksum = NULL WHERE version = '001_probe'")
			before := snapshotMigrationTables(t, h.db, []string{"probe", "schema_migrations"})
			files["002_probe_update.up.sql"] = &fstest.MapFile{Data: []byte("UPDATE probe SET value = 999; CREATE TABLE pending_probe (id INTEGER PRIMARY KEY);")}
			files["003_probe_index.up.sql"].Data = []byte("CREATE INDEX probe_value ON probe (value DESC);")

			var changed *database.MigrationChangedError
			if err := h.migrate(ctx, files); !errors.As(err, &changed) || changed.Version != "003_probe_index" {
				t.Fatalf("upgrade with pending SQL before an edited applied file = %v, want MigrationChangedError for 003_probe_index", err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			assertTableMissing(t, h.db, "pending_probe")

			files["003_probe_index.up.sql"].Data = []byte(originalIndex)
			if err := h.migrate(ctx, files); err != nil {
				t.Fatalf("retry after restoring the original file: %v", err)
			}
			assertCount(t, h.db, "SELECT COUNT(*) FROM probe WHERE value = 999", 1)
			assertCount(t, h.db, "SELECT COUNT(*) FROM schema_migrations", 3)
			assertCount(t, h.db, "SELECT COUNT(*) FROM schema_migrations WHERE checksum IS NULL", 0)
		})
	}
}

func TestMigrationsRejectMissingAppliedFiles(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		for _, kind := range []string{"removed", "renamed", "empty_files", "without_checksums"} {
			t.Run(backend+"/"+kind, func(t *testing.T) {
				h := newMigrationDB(t, backend)
				ctx := context.Background()
				files := fstest.MapFS{
					"001_probe.up.sql":       &fstest.MapFile{Data: []byte("CREATE TABLE probe (id INTEGER PRIMARY KEY, value INTEGER); INSERT INTO probe VALUES (1, 42);")},
					"003_probe_index.up.sql": &fstest.MapFile{Data: []byte("CREATE INDEX probe_value ON probe (value);")},
				}
				if err := h.migrate(ctx, files); err != nil {
					t.Fatal(err)
				}
				if kind == "without_checksums" {
					execMigrationSQL(t, h.db, "ALTER TABLE schema_migrations DROP COLUMN checksum")
				} else {
					execMigrationSQL(t, h.db, "UPDATE schema_migrations SET checksum = NULL WHERE version = '001_probe'")
				}
				before := snapshotMigrationTables(t, h.db, []string{"probe", "schema_migrations"})
				files["002_probe_update.up.sql"] = &fstest.MapFile{Data: []byte("UPDATE probe SET value = 999; CREATE TABLE pending_probe (id INTEGER PRIMARY KEY);")}
				incomplete := fstest.MapFS{}
				for name, file := range files {
					if name != "003_probe_index.up.sql" {
						incomplete[name] = file
					}
				}
				if kind == "renamed" {
					incomplete["003_renamed_index.up.sql"] = files["003_probe_index.up.sql"]
				} else if kind == "empty_files" {
					incomplete = fstest.MapFS{}
				}

				err := h.migrate(ctx, incomplete)
				wantMissing := []string{"003_probe_index"}
				if kind == "empty_files" {
					wantMissing = []string{"001_probe", "003_probe_index"}
				}
				var missing *database.MigrationMissingError
				if !errors.As(err, &missing) || !slices.Equal(missing.Versions, wantMissing) {
					t.Fatalf("startup with missing applied files = %v, want MigrationMissingError for %v", err, wantMissing)
				}
				for _, want := range append(wantMissing, "complete original migration history", "restore a backup with its matching server version") {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not mention %q", err, want)
					}
				}
				assertMigrationTablesUnchanged(t, h.db, before)
				if after := readMigrationTable(t, h.db, "schema_migrations", "*"); !slices.Equal(after.columns, before["schema_migrations"].columns) {
					t.Errorf("rejected history changed ledger columns: before %v, after %v", before["schema_migrations"].columns, after.columns)
				}
				assertTableMissing(t, h.db, "pending_probe")

				if err := h.migrate(ctx, files); err != nil {
					t.Fatalf("retry with the complete original history: %v", err)
				}
				assertCount(t, h.db, "SELECT COUNT(*) FROM probe WHERE value = 999", 1)
				assertCount(t, h.db, "SELECT COUNT(*) FROM schema_migrations", 3)
				assertCount(t, h.db, "SELECT COUNT(*) FROM schema_migrations WHERE checksum IS NULL", 0)
			})
		}
	}
}

func assertTableMissing(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), "SELECT * FROM "+table)
	if err == nil {
		rows.Close()
		t.Fatalf("table %s exists", table)
	}
}
