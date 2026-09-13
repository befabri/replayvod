package database_test

import "testing"

func TestMigrationsFailedTruncatedPreservesCapturedAndDeletedRecordings(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := t.Context()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "046")); err != nil {
				t.Fatal(err)
			}
			tables := seedExistingInstallation(t, h.db, "046")
			execMigrationSQL(t, h.db, `
    UPDATE videos SET status = 'FAILED' WHERE id = 71;
    INSERT INTO videos (id, job_id, filename, display_name, broadcaster_id, status, deleted_at, deletion_kind) VALUES
     (73, 'saved', 'saved', 'Saved media', 'channel', 'FAILED', NULL, NULL),
     (74, 'done', 'done', 'Done', 'channel', 'DONE', NULL, NULL),
     (75, 'deleted', 'deleted', 'Deleted', 'channel', 'FAILED', '2025-03-01 00:00:00', 'manual'),
     (76, 'empty', 'empty', 'Empty', 'channel', 'FAILED', NULL, NULL),
     (77, 'single-file', 'single-file', 'Historical capture', 'channel', 'FAILED', NULL, NULL);
    UPDATE videos SET size_bytes=2048,duration_seconds=12.5 WHERE id=77;
    INSERT INTO video_parts (video_id, part_index, filename, quality, codec, segment_format, size_bytes, start_media_seq)
     VALUES (73, 1, 'saved.mp4', 'HIGH', 'h264', 'fmp4', 100, 0);
    UPDATE videos SET truncated = TRUE;
   `)
			want := snapshotMigrationTables(t, h.db, tables)
			var cleared any = false
			if backend == "sqlite" {
				cleared = int64(0)
			}
			expectMigrationValue(t, want, "videos", 71, "truncated", cleared)
			expectMigrationValue(t, want, "videos", 76, "truncated", cleared)
			files := migrationsThrough(t, h.files, "049")
			if err := h.migrate(ctx, files); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, want)
			rollbackMigration(t, h, "049_videos_failed_truncated")
			assertMigrationTablesUnchanged(t, h.db, want)
			if err := h.migrate(ctx, files); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, want)
		})
	}
}
