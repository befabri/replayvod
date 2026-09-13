package database_test

import "testing"

func TestCanonicalTimelineUpgradeBackfillsOnlyMissingDimensions(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := t.Context()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "061")); err != nil {
				t.Fatal(err)
			}
			seedExistingInstallation(t, h.db, "061")
			execMigrationSQL(t, h.db, `
INSERT INTO titles(id,name) VALUES(52,'Changed title');
INSERT INTO categories(id,name) VALUES('game-2','Changed game');
INSERT INTO videos(id,job_id,filename,display_name,broadcaster_id,status) VALUES
(73,'spans-only','spans-only','Spans only','channel','DONE'),
(74,'observed-both','observed-both','Observed both','channel','DONE');
INSERT INTO video_metadata_changes(id,video_id,occurred_at,title_id,category_id,media_offset_seconds) VALUES
(500,71,'2025-01-03 00:00:00',51,NULL,42.5),
(501,72,'2025-01-03 00:00:00',NULL,'game',9.5),
(502,74,'2025-01-03 00:00:00',52,'game-2',99);
INSERT INTO video_title_spans(video_id,title_id,started_at,ended_at,duration_seconds) VALUES
(71,51,'2025-01-01 00:00:00','2025-01-02 00:00:00',86400),
(71,52,'2025-01-02 00:00:00',NULL,0),
(72,51,'2025-01-01 00:00:00','2025-01-02 00:00:00',86400),
(72,52,'2025-01-02 00:00:00',NULL,0),
(73,51,'2025-01-01 00:00:00','2025-01-02 00:00:00',86400),
(73,52,'2025-01-02 00:00:00','2025-01-03 00:00:00',86400),
(73,51,'2025-01-03 00:00:00',NULL,0),
(74,51,'2025-01-01 00:00:00',NULL,0);
INSERT INTO video_category_spans(video_id,category_id,started_at,ended_at,duration_seconds) VALUES
(71,'game','2025-01-01 00:00:00','2025-01-02 00:00:00',86400),
(71,'game-2','2025-01-02 00:00:00',NULL,0),
(72,'game','2025-01-01 00:00:00',NULL,0),
(73,'game','2025-01-01 00:00:00','2025-01-02 00:00:00',86400),
(73,'game-2','2025-01-02 00:00:00',NULL,0),
(74,'game','2025-01-01 00:00:00',NULL,0);
`)
			before := snapshotMigrationTables(t, h.db, []string{"videos", "video_title_spans", "video_category_spans", "titles", "categories"})
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "062")); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			assertCount(t, h.db, "SELECT COUNT(*) FROM video_metadata_changes", 12)
			assertCount(t, h.db, `SELECT COUNT(*) FROM video_metadata_changes WHERE id=500
AND video_id=71 AND occurred_at='2025-01-03 00:00:00' AND title_id=51 AND category_id IS NULL AND media_offset_seconds=42.5`, 1)
			assertCount(t, h.db, `SELECT COUNT(*) FROM video_metadata_changes WHERE id=501
AND video_id=72 AND occurred_at='2025-01-03 00:00:00' AND title_id IS NULL AND category_id='game' AND media_offset_seconds=9.5`, 1)
			assertCount(t, h.db, `SELECT COUNT(*) FROM video_metadata_changes WHERE id=502
AND video_id=74 AND occurred_at='2025-01-03 00:00:00' AND title_id=52 AND category_id='game-2' AND media_offset_seconds=99`, 1)
			assertCount(t, h.db, "SELECT COUNT(*) FROM video_metadata_changes WHERE video_id=71 AND title_id IS NOT NULL", 1)
			assertCount(t, h.db, "SELECT COUNT(*) FROM video_metadata_changes WHERE video_id=72 AND category_id IS NOT NULL", 1)
			assertCount(t, h.db, "SELECT COUNT(*) FROM video_metadata_changes WHERE video_id=74", 1)
			assertCount(t, h.db, `SELECT COUNT(*) FROM video_metadata_changes c
JOIN video_title_spans s ON s.video_id=c.video_id AND s.title_id=c.title_id AND s.started_at=c.occurred_at
WHERE c.video_id IN (72,73) AND c.category_id IS NULL AND c.media_offset_seconds IS NULL`, 5)
			assertCount(t, h.db, `SELECT COUNT(*) FROM video_metadata_changes c
JOIN video_category_spans s ON s.video_id=c.video_id AND s.category_id=c.category_id AND s.started_at=c.occurred_at
WHERE c.video_id IN (71,73) AND c.title_id IS NULL AND c.media_offset_seconds IS NULL`, 4)
			beforeRollback := snapshotMigrationTables(t, h.db, []string{"video_metadata_changes", "video_title_spans", "video_category_spans"})
			rollbackMigration(t, h, "062_canonical_recording_timeline")
			assertMigrationTablesUnchanged(t, h.db, beforeRollback)
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "062")); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, beforeRollback)
		})
	}
}
