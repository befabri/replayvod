package sqliteadapter

import (
	"context"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

func sqliteRetentionHours(v int64) *int64 { return &v }

func TestFinalizeRetentionDelete_RollsBackWhenPartDeleteFails(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	seedUserChannel(t, ctx, a, "u-retention-tx", "b-retention-tx")

	video, err := a.CreateVideo(ctx, &repository.VideoInput{
		JobID:                "job-retention-tx",
		Filename:             "retention-tx",
		DisplayName:          "b-retention-tx",
		Status:               repository.VideoStatusDone,
		Quality:              repository.QualityHigh,
		BroadcasterID:        "b-retention-tx",
		RecordingType:        repository.RecordingTypeVideo,
		RetentionWindowHours: sqliteRetentionHours(1),
	})
	if err != nil {
		t.Fatalf("CreateVideo: %v", err)
	}
	part, err := a.CreateVideoPart(ctx, &repository.VideoPartInput{
		VideoID:       video.ID,
		PartIndex:     1,
		Filename:      "retention-tx-part01.mp4",
		Quality:       "1080",
		Codec:         repository.CodecH264,
		SegmentFormat: repository.SegmentFormatFMP4,
	})
	if err != nil {
		t.Fatalf("CreateVideoPart: %v", err)
	}
	if _, err := a.db.ExecContext(ctx, `CREATE TABLE retention_part_refs (
		part_id INTEGER NOT NULL REFERENCES video_parts(id) ON DELETE RESTRICT
	)`); err != nil {
		t.Fatalf("create blocking FK table: %v", err)
	}
	if _, err := a.db.ExecContext(ctx, `INSERT INTO retention_part_refs (part_id) VALUES (?)`, part.ID); err != nil {
		t.Fatalf("insert blocking FK: %v", err)
	}

	if err := a.FinalizeDelete(ctx, video.ID, repository.DeletionKindRetention); err == nil {
		t.Fatal("FinalizeRetentionDelete returned nil; want FK failure")
	}

	got, err := a.GetVideo(ctx, video.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.DeletedAt != nil {
		t.Fatalf("video deleted_at = %v, want nil after rollback", got.DeletedAt)
	}
	parts, err := a.ListVideoParts(ctx, video.ID)
	if err != nil {
		t.Fatalf("ListVideoParts: %v", err)
	}
	if len(parts) != 1 || parts[0].ID != part.ID {
		t.Fatalf("parts after rollback = %+v, want original part %d", parts, part.ID)
	}
}
