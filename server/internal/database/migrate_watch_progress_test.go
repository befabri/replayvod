package database_test

import (
	"context"
	"testing"
	"time"
)

func TestMigrationsWatchProgressRevisionPreservesHistory(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := context.Background()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "052")); err != nil {
				t.Fatal(err)
			}
			tables := seedExistingInstallation(t, h.db, "052")
			before := snapshotMigrationTables(t, h.db, tables)
			up := migrationsThrough(t, h.files, "053")
			if err := h.migrate(ctx, up); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			state, err := h.repo.UpdateVideoWatchProgress(ctx, "viewer", 71, 45, false, time.Now())
			if err != nil || state.ProgressRevision != 1 {
				t.Fatalf("first revision: %+v, %v", state, err)
			}
			// Downgrading discards only the revision, preserving the progress
			// and watch-later state supported by the previous schema.
			progress := readMigrationTable(t, h.db, "video_user_states", "user_id,video_id,last_position_seconds,last_progress_at_ms,watch_later,watched_at,completed_at")
			rollbackMigration(t, h, "053_watch_progress_index")
			assertMigrationTablesUnchanged(t, h.db, map[string]migrationTableSnapshot{"video_user_states": progress})
			if err := h.migrate(ctx, up); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, map[string]migrationTableSnapshot{"video_user_states": progress})
		})
	}
}
