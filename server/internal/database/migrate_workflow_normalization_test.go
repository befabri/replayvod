package database_test

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRecordingWorkflowUpgradeNormalizesRecoverableCheckpoints(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := t.Context()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "059")); err != nil {
				t.Fatal(err)
			}
			execMigrationSQL(t, h.db, `
INSERT INTO channels(broadcaster_id,broadcaster_login,broadcaster_name) VALUES('channel','channel','Channel');
INSERT INTO videos(id,job_id,filename,display_name,broadcaster_id,status)
VALUES(71,'recording','recording','Recording','channel','RUNNING');
`)
			cases := []struct {
				name, before, want string
			}{
				{"empty", `{}`, `{"stage":"AUTH","current_part_index":1,"part_started":false}`},
				{"nulls", `{"stage":null,"current_part_index":null,"part_started":null}`, `{"stage":"AUTH","current_part_index":1,"part_started":false}`},
				{"unstarted", `{"stage":"AUTH","current_part_index":0,"custom":"preserved"}`, `{"stage":"AUTH","current_part_index":1,"part_started":false,"custom":"preserved"}`},
				{"zero sequence already stored", `{"stage":"STORE","current_part_index":2,"part_start_media_sequence":0}`, `{"stage":"STORE","current_part_index":2,"part_start_media_sequence":0,"part_started":true}`},
				{"downloaded bytes", `{"stage":"DOWNLOAD","part_bytes":123}`, `{"stage":"DOWNLOAD","current_part_index":1,"part_bytes":123,"part_started":true}`},
				{"accounted gap", `{"gaps":[{"start":0,"end":1}]}`, `{"stage":"AUTH","current_part_index":1,"gaps":[{"start":0,"end":1}],"part_started":true}`},
				{"invalid stage", `{"stage":7,"current_part_index":0}`, `{"stage":7,"current_part_index":0}`},
				{"invalid index", `{"current_part_index":"corrupt"}`, `{"current_part_index":"corrupt"}`},
				{"invalid flag", `{"part_started":"true"}`, `{"part_started":"true"}`},
				{"array", `[]`, `[]`},
			}
			for _, tc := range cases {
				if _, err := h.db.ExecContext(ctx, `INSERT INTO jobs
(id,video_id,broadcaster_id,status,resume_state,execution_id,accepts_metadata)
VALUES($1,71,'channel','RUNNING',$2,'owner',TRUE)`, tc.name, tc.before); err != nil {
					t.Fatal(err)
				}
			}
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "060")); err != nil {
				t.Fatal(err)
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					var data string
					if err := h.db.QueryRowContext(ctx, "SELECT resume_state FROM jobs WHERE id=$1", tc.name).Scan(&data); err != nil {
						t.Fatal(err)
					}
					var got, want any
					if err := json.Unmarshal([]byte(data), &got); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("checkpoint = %s, want %s", data, tc.want)
					}
				})
			}
			assertCount(t, h.db, "SELECT COUNT(*) FROM jobs WHERE execution_id='owner' AND accepts_metadata=TRUE AND status='RUNNING'", len(cases))
			assertCount(t, h.db, "SELECT COUNT(*) FROM video_parts", 0)
			beforeRollback := snapshotMigrationTables(t, h.db, []string{"jobs", "videos"})
			rollbackMigration(t, h, "060_recording_workflow_upgrade")
			assertMigrationTablesUnchanged(t, h.db, beforeRollback)
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "060")); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, beforeRollback)
		})
	}
}

func TestRecordingWorkflowUpgradeUsesStoredMediaReferences(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := t.Context()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "059")); err != nil {
				t.Fatal(err)
			}
			execMigrationSQL(t, h.db, `
INSERT INTO channels(broadcaster_id,broadcaster_login,broadcaster_name) VALUES('channel','channel','Channel');
INSERT INTO videos(id,job_id,filename,display_name,broadcaster_id,status,recording_type,quality,selected_quality,duration_seconds,size_bytes,thumbnail) VALUES
(71,'audio','audio','Audio','channel','DONE','audio','HIGH','MEDIUM',12.5,123,'audio-poster.jpg'),
(72,'empty-fields','empty-fields','Unknown metadata','channel','DONE','video','LOW',NULL,NULL,NULL,NULL),
(73,'failed-media','failed-media','Partial capture','channel','FAILED','video','HIGH',NULL,4.5,99,NULL),
(74,'failed-empty','failed-empty','Empty capture','channel','FAILED','video','HIGH',NULL,NULL,0,NULL),
(75,'missing','missing','Missing media','channel','DONE','video','HIGH',NULL,10,100,NULL),
(76,'retention','retention','Pruned media','channel','DONE','video','HIGH',NULL,10,100,NULL),
(77,'manual','manual','Removed media','channel','DONE','video','HIGH',NULL,10,100,NULL),
(78,'parts','parts','Existing parts','channel','DONE','video','HIGH',NULL,10,100,NULL);
UPDATE videos SET deleted_at='2025-01-01 00:00:00',deletion_kind='missing' WHERE id=75;
UPDATE videos SET deleted_at='2025-01-01 00:00:00',deletion_kind='retention' WHERE id=76;
UPDATE videos SET deleted_at='2025-01-01 00:00:00',deletion_kind='manual' WHERE id=77;
INSERT INTO video_parts(video_id,part_index,filename,quality,codec,segment_format,duration_seconds,size_bytes,start_media_seq,end_media_seq)
VALUES(78,0,'original-part.ts','LOW','h264','ts',9,88,100,109);
INSERT INTO video_playback_assets(video_id,status,filename,mime_type,duration_seconds,size_bytes,generated_at,last_accessed_at) VALUES
(71,'ready','cache-custom.mp4','video/mp4',12.5,123,'2025-01-01 00:00:00','2025-01-01 00:00:00'),
(72,'ready','cache-existing.mp4','video/mp4',9,90,'2025-01-01 00:00:00','2025-01-01 00:00:00');
INSERT INTO media_publications(key,video_id,digest,size_bytes,unresolved,delete_requested)
VALUES('videos/cache-existing.mp4',72,'known-digest',90,TRUE,TRUE);
`)
			before := snapshotMigrationTables(t, h.db, []string{"videos", "video_playback_assets"})
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "060")); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			assertCount(t, h.db, "SELECT COUNT(*) FROM video_parts", 5)
			assertCount(t, h.db, `SELECT COUNT(*) FROM video_parts WHERE video_id=71
AND part_index=1 AND filename='audio.mp4' AND quality='MEDIUM' AND codec='aac' AND segment_format='fmp4'
AND duration_seconds=12.5 AND size_bytes=123 AND thumbnail='audio-poster.jpg' AND start_media_seq=0 AND end_media_seq IS NULL`, 1)
			assertCount(t, h.db, `SELECT COUNT(*) FROM video_parts WHERE video_id=72
AND filename='empty-fields.mp4' AND quality='LOW' AND codec='h264' AND duration_seconds=0 AND size_bytes=0`, 1)
			assertCount(t, h.db, "SELECT COUNT(*) FROM video_parts WHERE video_id=73 AND filename='failed-media.mp4' AND size_bytes=99", 1)
			assertCount(t, h.db, "SELECT COUNT(*) FROM video_parts WHERE video_id=75 AND filename='missing.mp4'", 1)
			assertCount(t, h.db, "SELECT COUNT(*) FROM video_parts WHERE video_id IN (74,76,77)", 0)
			assertCount(t, h.db, `SELECT COUNT(*) FROM video_parts WHERE video_id=78
AND part_index=0 AND filename='original-part.ts' AND quality='LOW' AND segment_format='ts'
AND duration_seconds=9 AND size_bytes=88 AND start_media_seq=100 AND end_media_seq=109`, 1)
			assertCount(t, h.db, `SELECT COUNT(*) FROM media_publications
WHERE key='videos/cache-custom.mp4' AND video_id=71 AND digest='' AND size_bytes=123`, 1)
			assertCount(t, h.db, `SELECT COUNT(*) FROM media_publications
WHERE key='videos/cache-existing.mp4' AND digest='known-digest' AND size_bytes=90 AND unresolved=TRUE AND delete_requested=TRUE`, 1)
			beforeRollback := snapshotMigrationTables(t, h.db, []string{"videos", "video_parts", "video_playback_assets", "media_publications"})
			rollbackMigration(t, h, "060_recording_workflow_upgrade")
			assertMigrationTablesUnchanged(t, h.db, beforeRollback)
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "060")); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, beforeRollback)
		})
	}
}
