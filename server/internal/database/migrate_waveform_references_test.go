package database_test

import "testing"

func TestWaveformReferencesUpgradePreservesLegacyKeysAndCleanup(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := t.Context()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "060")); err != nil {
				t.Fatal(err)
			}
			seedExistingInstallation(t, h.db, "060")
			execMigrationSQL(t, h.db, `
UPDATE videos SET recording_type='audio' WHERE id IN (71,72);
INSERT INTO videos(id,job_id,filename,display_name,broadcaster_id,status)
VALUES(73,'video','video','Video','channel','DONE');
INSERT INTO media_publications(key,video_id,digest,size_bytes,unresolved,delete_requested)
VALUES('thumbnails/recording-1-waveform.json',71,'existing-digest',123,TRUE,TRUE);
`)
			before := snapshotMigrationTables(t, h.db, []string{"videos", "video_parts", "jobs"})
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "061")); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			assertCount(t, h.db, "SELECT COUNT(*) FROM video_waveform_assets", 2)
			assertCount(t, h.db, "SELECT COUNT(*) FROM video_waveform_assets WHERE video_id=71 AND key='thumbnails/recording-1-waveform.json'", 1)
			assertCount(t, h.db, "SELECT COUNT(*) FROM video_waveform_assets WHERE video_id=72 AND key='thumbnails/recording-2-waveform.json'", 1)
			assertCount(t, h.db, `SELECT COUNT(*) FROM media_publications
WHERE key='thumbnails/recording-1-waveform.json' AND video_id=71 AND digest='existing-digest'
AND size_bytes=123 AND unresolved=TRUE AND delete_requested=TRUE`, 1)
			assertCount(t, h.db, `SELECT COUNT(*) FROM media_publications
WHERE key='thumbnails/recording-2-waveform.json' AND video_id=72 AND digest=''
AND size_bytes=0 AND unresolved=FALSE AND delete_requested=FALSE`, 1)
			if _, err := h.db.ExecContext(ctx, `INSERT INTO video_waveform_assets(video_id,key)
VALUES(73,'thumbnails/recording-1-waveform.json')`); err == nil {
				t.Fatal("one waveform key was attached to two recordings")
			}
			execMigrationSQL(t, h.db, "DELETE FROM videos WHERE id=72")
			assertCount(t, h.db, "SELECT COUNT(*) FROM video_waveform_assets WHERE video_id=72", 0)
			assertCount(t, h.db, "SELECT COUNT(*) FROM media_publications WHERE video_id=72 AND key='thumbnails/recording-2-waveform.json'", 1)
			remaining := snapshotMigrationTables(t, h.db, []string{"videos", "jobs", "media_publications"})
			rollbackMigration(t, h, "061_waveform_publication_references")
			assertMigrationTablesUnchanged(t, h.db, remaining)
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "061")); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, remaining)
			assertCount(t, h.db, "SELECT COUNT(*) FROM video_waveform_assets WHERE video_id=71 AND key='thumbnails/recording-1-waveform.json'", 1)
			assertCount(t, h.db, "SELECT COUNT(*) FROM video_waveform_assets", 1)
		})
	}
}
