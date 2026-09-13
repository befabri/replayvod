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
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "056")); err != nil {
				t.Fatal(err)
			}
			seedExistingInstallation(t, h.db, "056")
			execMigrationSQL(t, h.db, `
INSERT INTO videos(id,job_id,filename,display_name,broadcaster_id,status) VALUES
(80,'orphan-media','orphan-media','Saved capture','channel','RUNNING'),
(81,'orphan-empty','orphan-empty','Empty admission','channel','PENDING');
INSERT INTO video_parts(video_id,part_index,filename,quality,codec,segment_format,size_bytes,start_media_seq)
VALUES(80,1,'orphan-media-part01.mp4','HIGH','h264','ts',100,0);
`)
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "057")); err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				id         int64
				completion string
			}{{80, repository.CompletionKindPartial}, {81, repository.CompletionKindComplete}} {
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
			assertCount(t, h.db, "SELECT COUNT(*) FROM jobs WHERE execution_id = '' AND accepts_metadata = FALSE", 1)
		})
	}
}
