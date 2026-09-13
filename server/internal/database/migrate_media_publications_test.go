package database_test

import "testing"

func TestMediaPublicationsUpgradeRetainsCleanupAfterVideoDeletion(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := t.Context()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "057")); err != nil {
				t.Fatal(err)
			}
			before := snapshotMigrationTables(t, h.db, seedExistingInstallation(t, h.db, "057"))
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "058")); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			execMigrationSQL(t, h.db, `
INSERT INTO media_publications(key,video_id,digest,size_bytes,unresolved,delete_requested)
VALUES('videos/recording-1-part0',71,'original-digest',123,TRUE,TRUE);
DELETE FROM videos WHERE id=71;
`)
			assertCount(t, h.db, "SELECT COUNT(*) FROM video_parts WHERE video_id=71", 0)
			assertCount(t, h.db, `SELECT COUNT(*) FROM media_publications
WHERE key='videos/recording-1-part0' AND video_id=71 AND digest='original-digest'
AND size_bytes=123 AND unresolved=TRUE AND delete_requested=TRUE`, 1)

			// A remote upload may finish after deletion removed its recording.
			execMigrationSQL(t, h.db, `INSERT INTO media_publications(key,video_id,digest,size_bytes)
VALUES('videos/late-upload',71,'late-digest',456)`)
			assertCount(t, h.db, `SELECT COUNT(*) FROM media_publications
WHERE key='videos/late-upload' AND unresolved=FALSE AND delete_requested=FALSE`, 1)
			if _, err := h.db.ExecContext(ctx, `INSERT INTO media_publications(key,video_id,digest,size_bytes)
VALUES('videos/recording-1-part0',72,'replacement-digest',789)`); err == nil {
				t.Fatal("duplicate publication key replaced an outstanding cleanup obligation")
			}
			assertCount(t, h.db, `SELECT COUNT(*) FROM media_publications
WHERE key='videos/recording-1-part0' AND video_id=71 AND digest='original-digest'`, 1)

			remaining := snapshotMigrationTables(t, h.db, []string{"videos", "jobs", "channels"})
			rollbackMigration(t, h, "058_media_publications")
			assertMigrationTablesUnchanged(t, h.db, remaining)
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "058")); err != nil {
				t.Fatalf("reapply media publication schema: %v", err)
			}
			assertCount(t, h.db, "SELECT COUNT(*) FROM media_publications", 0)
			assertMigrationTablesUnchanged(t, h.db, remaining)
		})
	}
}
