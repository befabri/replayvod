package database_test

import (
	"context"
	"testing"
)

// TestMigrationsMissingKindAndTombstonePosters covers 047 in both directions:
// the missing kind becomes valid, poster paths that point at purged objects
// are cleared on manual and retention tombstones while live rows and missing
// tombstones keep theirs, and a downgrade maps missing to manual. The cleared
// paths are intentionally irreversible.
func TestMigrationsMissingKindAndTombstonePosters(t *testing.T) {
	const version = "047_videos_deletion_kind_missing"
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := context.Background()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "046")); err != nil {
				t.Fatal(err)
			}
			tables := seedExistingInstallation(t, h.db, "046")
			execMigrationSQL(t, h.db, `UPDATE videos SET deleted_at = '2025-03-01 00:00:00', deletion_kind = 'retention', thumbnail = 'purged.jpg' WHERE id = 71;
				UPDATE videos SET thumbnail = 'live.jpg' WHERE id = 72;
				INSERT INTO videos (id, job_id, filename, display_name, broadcaster_id, deleted_at, deletion_kind, thumbnail) VALUES
				(73, 'manual', 'manual', 'Manual tombstone', 'channel', '2025-03-01 00:00:00', 'manual', 'manual.jpg'),
				(74, 'null-poster', 'null-poster', 'Already cleared', 'channel', '2025-03-01 00:00:00', 'retention', NULL),
				(75, 'live-null', 'live-null', 'No poster', 'channel', NULL, NULL, NULL)`)
			want := snapshotMigrationTables(t, h.db, tables)
			expectMigrationValue(t, want, "videos", 71, "thumbnail", nil)
			expectMigrationValue(t, want, "videos", 73, "thumbnail", nil)
			up := migrationsThrough(t, h.files, "047")
			if err := h.migrate(ctx, up); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, want)
			assertRejected(t, h, `UPDATE videos SET deletion_kind = 'bogus' WHERE id = 71`)

			// A missing-media tombstone keeps its poster, and a restart never
			// re-runs the cleanup on it.
			execMigrationSQL(t, h.db, `UPDATE videos SET deletion_kind = 'missing', thumbnail = 'kept.jpg' WHERE id = 71`)
			written := snapshotMigrationTables(t, h.db, tables)
			if err := h.migrate(ctx, up); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, written)

			// Downgrade: older releases only know retention and manual.
			expectMigrationValue(t, written, "videos", 71, "deletion_kind", "manual")
			rollbackMigration(t, h, version)
			assertMigrationTablesUnchanged(t, h.db, written)
			assertRejected(t, h, `UPDATE videos SET deletion_kind = 'missing' WHERE id = 71`)
			// Re-applying clears the poster of what is now a manual tombstone.
			expectMigrationValue(t, written, "videos", 71, "thumbnail", nil)
			if err := h.migrate(ctx, up); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, written)
			execMigrationSQL(t, h.db, `UPDATE videos SET deletion_kind = 'missing' WHERE id = 71`)
			assertCount(t, h.db, `SELECT COUNT(*) FROM videos WHERE deletion_kind = 'missing'`, 1)
		})
	}
}

// assertRejected runs statement expecting the schema to refuse it. It runs in
// its own transaction so a Postgres session is not left aborted.
func assertRejected(t *testing.T, h migrationDB, statement string) {
	t.Helper()
	tx, err := h.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback is the point
	if _, err := tx.ExecContext(context.Background(), statement); err == nil {
		t.Fatalf("statement was accepted, want a check violation: %s", statement)
	}
}
