package database_test

import (
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

func TestExecutionOwnershipUpgradePreservesOrphanMedia(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := t.Context()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "046")); err != nil {
				t.Fatal(err)
			}
			seedExistingInstallation(t, h.db, "046")
			execMigrationSQL(t, h.db, `
INSERT INTO videos(id,job_id,filename,display_name,broadcaster_id,status) VALUES
(80,'orphan-media','orphan-media','Saved capture','channel','RUNNING'),
(81,'orphan-empty','orphan-empty','Empty admission','channel','PENDING'),
(82,'orphan-single-pending','orphan-single-pending','Historical pending media','channel','PENDING'),
(83,'orphan-single-running','orphan-single-running','Historical running media','channel','RUNNING');
UPDATE videos SET size_bytes=2048,duration_seconds=12.5 WHERE id IN (82,83);
INSERT INTO video_parts(video_id,part_index,filename,quality,codec,segment_format,size_bytes,start_media_seq)
VALUES(80,1,'orphan-media-part01.mp4','HIGH','h264','ts',100,0);
`)
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "057")); err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				id         int64
				completion string
			}{{80, repository.CompletionKindPartial}, {81, repository.CompletionKindComplete}, {82, repository.CompletionKindPartial}, {83, repository.CompletionKindPartial}} {
				v, err := h.repo.GetVideo(ctx, tc.id)
				if err != nil || v.Status != repository.VideoStatusFailed || v.CompletionKind != tc.completion || v.Error == nil {
					t.Fatalf("orphan %d: %+v, %v", tc.id, v, err)
				}
			}
			parts, err := h.repo.ListVideoParts(ctx, 80)
			if err != nil || len(parts) != 1 || parts[0].SizeBytes != 100 {
				t.Fatalf("saved parts: %+v, %v", parts, err)
			}
			owned, err := h.repo.GetVideo(ctx, 72)
			if err != nil || owned.Status != repository.VideoStatusRunning {
				t.Fatalf("owned recording changed: %+v, %v", owned, err)
			}
			assertCount(t, h.db, "SELECT COUNT(*) FROM videos WHERE id IN (82,83) AND size_bytes=2048 AND duration_seconds=12.5", 2)
			assertCount(t, h.db, "SELECT COUNT(*) FROM video_parts WHERE video_id IN (82,83)", 0)
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "060")); err != nil {
				t.Fatal(err)
			}
			assertCount(t, h.db, "SELECT COUNT(*) FROM videos WHERE id IN (82,83) AND status='FAILED' AND completion_kind='partial'", 2)
			assertCount(t, h.db, "SELECT COUNT(*) FROM video_parts WHERE video_id IN (82,83) AND size_bytes=2048 AND duration_seconds=12.5", 2)
			assertCount(t, h.db, "SELECT COUNT(*) FROM jobs WHERE id IN ('orphan-single-pending','orphan-single-running')", 0)
			assertCount(t, h.db, "SELECT COUNT(*) FROM jobs WHERE execution_id = '' AND accepts_metadata = FALSE", 1)
		})
	}
}
