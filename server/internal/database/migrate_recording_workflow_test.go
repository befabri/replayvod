package database_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
)

func TestRecordingWorkflowSQLUpgrade(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := t.Context()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "056")); err != nil {
				t.Fatal(err)
			}
			seedExistingInstallation(t, h.db, "056")
			execMigrationSQL(t, h.db, `
INSERT INTO videos(id,job_id,filename,display_name,broadcaster_id,status,duration_seconds,size_bytes) VALUES
(80,'historical-file','historical','Historical','channel','DONE',42,1234),
(81,'orphan','orphan','Orphan','channel','RUNNING',NULL,NULL),
(82,'seq-zero','seq-zero','Saved capture','channel','RUNNING',NULL,NULL),
(83,'corrupt','corrupt','Corrupt checkpoint','channel','RUNNING',NULL,NULL),
(84,'removed','removed','Restorable old media','channel','DONE',24,500),
(85,'purged-manual','purged-manual','Purged manual media','channel','DONE',24,500),
(86,'purged-retention','purged-retention','Purged retained media','channel','DONE',24,500),
(87,'empty-orphan','empty-orphan','Empty admission','channel','PENDING',NULL,NULL),
(88,'numeric-stage','numeric-stage','Corrupt stage','channel','RUNNING',NULL,NULL),
(89,'empty-checkpoint','empty-checkpoint','Empty checkpoint','channel','PENDING',NULL,NULL),
(90,'null-stage','null-stage','Null stage','channel','PENDING',NULL,NULL);
UPDATE videos SET deleted_at=CURRENT_TIMESTAMP, deletion_kind='missing' WHERE id=84;
UPDATE videos SET deleted_at=CURRENT_TIMESTAMP, deletion_kind='manual' WHERE id=85;
UPDATE videos SET deleted_at=CURRENT_TIMESTAMP, deletion_kind='retention' WHERE id=86;
UPDATE videos SET recording_type='audio' WHERE id=80;
INSERT INTO video_playback_assets(video_id,status,filename,mime_type,duration_seconds,size_bytes,generated_at,last_accessed_at)
VALUES(80,'ready','historical-cached.mp4','video/mp4',42,4321,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO video_parts(video_id,part_index,filename,quality,codec,segment_format,size_bytes,start_media_seq)
VALUES(81,1,'orphan-part01.mp4','HIGH','h264','ts',100,0);
INSERT INTO jobs(id,video_id,broadcaster_id,status,resume_state) VALUES
('seq-zero',82,'channel','RUNNING','{"stage":"PREPARE_INPUT","current_part_index":0,"part_start_media_sequence":0,"accounted_frontier_media_sequence":0}'),
('corrupt',83,'channel','RUNNING','{"stage":"SEGMENTS","current_part_index":"broken","part_started":{},"gaps":23}'),
('numeric-stage',88,'channel','RUNNING','{"stage":42}'),
('empty-checkpoint',89,'channel','PENDING','{}'),
('null-stage',90,'channel','PENDING','{"stage":null}');
INSERT INTO titles(id,name) VALUES(801,'Original title'),(802,'Canonical title');
INSERT INTO categories(id,name) VALUES('workflow-category','Workflow category');
INSERT INTO video_title_spans(video_id,title_id,started_at) VALUES
(80,801,'2026-01-01 00:00:00'),(82,801,'2026-01-01 00:00:00');
INSERT INTO video_category_spans(video_id,category_id,started_at) VALUES
(80,'workflow-category','2026-01-01 00:00:00'),(82,'workflow-category','2026-01-01 00:00:00');
INSERT INTO video_metadata_changes(video_id,title_id,occurred_at) VALUES(82,802,'2026-01-01 00:01:00');
`)
			if err := h.migrate(ctx, h.files); err != nil {
				t.Fatal(err)
			}
			parts, err := h.repo.ListVideoParts(ctx, 80)
			if err != nil || len(parts) != 1 || parts[0].PartIndex != 1 || parts[0].Filename != "historical.mp4" || parts[0].DurationSeconds != 42 || parts[0].SizeBytes != 1234 {
				t.Fatalf("single-file migration: %+v %v", parts, err)
			}
			orphan, err := h.repo.GetVideo(ctx, 81)
			if err != nil || orphan.Status != repository.VideoStatusFailed || orphan.CompletionKind != repository.CompletionKindPartial {
				t.Fatalf("orphan classification: %+v %v", orphan, err)
			}
			saved, err := h.repo.ListVideoParts(ctx, 81)
			if err != nil || len(saved) != 1 || saved[0].SizeBytes != 100 {
				t.Fatal("orphan media facts lost")
			}
			job, err := h.repo.GetJob(ctx, "seq-zero")
			if err != nil {
				t.Fatal(err)
			}
			checkpoint, err := downloader.UnmarshalResumeState(job.ResumeState)
			if err != nil || !checkpoint.PartStarted || checkpoint.CurrentPartIndex != 1 {
				t.Fatalf("checkpoint upgrade: %+v %v", checkpoint, err)
			}
			var fields map[string]any
			if err := json.Unmarshal(job.ResumeState, &fields); err != nil {
				t.Fatal(err)
			}
			if fields["part_start_media_sequence"] != float64(0) {
				t.Fatal("sequence zero changed")
			}
			for _, id := range []string{"empty-checkpoint", "null-stage"} {
				job, err := h.repo.GetJob(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				fields = nil
				if err := json.Unmarshal(job.ResumeState, &fields); err != nil || fields["stage"] != "AUTH" || fields["current_part_index"] != float64(1) || fields["part_started"] != false {
					t.Fatalf("unstarted checkpoint %s: %s, %v", id, job.ResumeState, err)
				}
			}
			video, _ := h.repo.GetVideo(ctx, 82)
			if video.StreamID != nil {
				t.Fatal("upgrade invented broadcast identity")
			}
			corrupt, err := h.repo.GetJob(ctx, "corrupt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := downloader.UnmarshalResumeState(corrupt.ResumeState); err == nil {
				t.Fatal("corrupt checkpoint was silently changed into valid work")
			}
			restored, err := h.repo.ListVideoParts(ctx, 84)
			if err != nil || len(restored) != 1 || restored[0].Filename != "removed.mp4" || restored[0].SizeBytes != 500 {
				t.Fatalf("tombstoned single-file media lost its restore reference: %+v %v", restored, err)
			}
			waveformKey, err := h.repo.GetVideoWaveformKey(ctx, 80)
			if err != nil || waveformKey != "thumbnails/historical-waveform.json" {
				t.Fatalf("historical waveform reference: %q %v", waveformKey, err)
			}
			publication, err := h.repo.GetMediaPublication(ctx, waveformKey)
			if err != nil || publication.VideoID != 80 {
				t.Fatalf("historical waveform cleanup was not journaled: %+v %v", publication, err)
			}
			cache, err := h.repo.GetMediaPublication(ctx, "videos/historical-cached.mp4")
			if err != nil || cache.VideoID != 80 || cache.SizeBytes != 4321 || cache.Unresolved || cache.DeleteRequested {
				t.Fatalf("historical cache cleanup reference: %+v, %v", cache, err)
			}
			for _, id := range []int64{85, 86} {
				parts, err := h.repo.ListVideoParts(ctx, id)
				if err != nil || len(parts) != 0 {
					t.Fatalf("purged recording %d acquired media references: %+v, %v", id, parts, err)
				}
			}
			empty, err := h.repo.GetVideo(ctx, 87)
			if err != nil || empty.Status != repository.VideoStatusFailed || empty.CompletionKind != repository.CompletionKindComplete || empty.Error == nil {
				t.Fatalf("empty orphan must be FAILED without partial media: %+v, %v", empty, err)
			}
			numeric, err := h.repo.GetJob(ctx, "numeric-stage")
			if err != nil {
				t.Fatal(err)
			}
			fields = nil
			if err := json.Unmarshal(numeric.ResumeState, &fields); err != nil || len(fields) != 1 || fields["stage"] != float64(42) {
				t.Fatalf("corrupt stage was normalized: %s, %v", numeric.ResumeState, err)
			}
			for _, id := range []int64{80, 82} {
				events, err := h.repo.ListVideoMetadataChanges(ctx, id)
				if err != nil || len(events) != 2 {
					t.Fatalf("timeline upgrade for %d: %+v %v", id, events, err)
				}
				for _, event := range events {
					if id == 82 && event.Title != nil && event.Title.Name != "Canonical title" {
						t.Fatal("historical spans replaced an existing canonical dimension")
					}
				}
			}
			if err := h.migrate(ctx, h.files); err != nil {
				t.Fatal(err)
			}
			parts, _ = h.repo.ListVideoParts(ctx, 80)
			if len(parts) != 1 {
				t.Fatal("restart duplicated historical part")
			}
		})
	}
}

func TestRecordingWorkflowSQLRollbackAndReupgrade(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := t.Context()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "056")); err != nil {
				t.Fatal(err)
			}
			tables := seedExistingInstallation(t, h.db, "056")
			historical := snapshotMigrationTables(t, h.db, tables)
			for range 2 {
				if err := h.migrate(ctx, h.files); err != nil {
					t.Fatal(err)
				}
				// Normalization remains after rollback. Compare every historical
				// column to its upgraded value, including newly added part rows.
				expected := map[string]migrationTableSnapshot{}
				for table, snapshot := range historical {
					expected[table] = readMigrationTable(t, h.db, table, strings.Join(snapshot.columns, ","))
				}
				for _, version := range []string{
					"064_job_stop_requested",
					"063_task_availability",
					"062_canonical_recording_timeline", "061_waveform_publication_references",
					"060_recording_workflow_upgrade", "059_recording_intents",
					"058_media_publications", "057_execution_ownership",
				} {
					rollbackMigration(t, h, version)
				}
				assertMigrationTablesUnchanged(t, h.db, expected)
				assertCount(t, h.db, "SELECT count(*) FROM schema_migrations WHERE version >= '057'", 0)
				for _, table := range []string{"jobs", "tasks"} {
					for _, column := range readMigrationTable(t, h.db, table, "*").columns {
						if column == "execution_id" || column == "accepts_metadata" || column == "is_available" || column == "stop_requested" {
							t.Fatalf("rollback kept new ownership column %s.%s", table, column)
						}
					}
				}
			}
		})
	}
}
