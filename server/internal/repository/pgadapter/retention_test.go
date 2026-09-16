package pgadapter

import (
	"context"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/contracttest"
)

func retentionHours(v int64) *int64 { return &v }

func TestFinalizeRetentionDelete_RollsBackWhenPartDeleteFails(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	contracttest.SeedUserChannel(t, ctx, a, "u-retention-tx", "b-retention-tx")

	video, err := a.CreateVideo(ctx, &repository.VideoInput{
		JobID:                "job-retention-tx",
		Filename:             "retention-tx",
		DisplayName:          "b-retention-tx",
		Status:               repository.VideoStatusDone,
		Quality:              repository.QualityHigh,
		BroadcasterID:        "b-retention-tx",
		RecordingType:        repository.RecordingTypeVideo,
		RetentionWindowHours: retentionHours(1),
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
	if _, err := a.db.Exec(ctx, `CREATE TABLE retention_part_refs (
		part_id BIGINT NOT NULL REFERENCES video_parts(id) ON DELETE RESTRICT
	)`); err != nil {
		t.Fatalf("create blocking FK table: %v", err)
	}
	if _, err := a.db.Exec(ctx, `INSERT INTO retention_part_refs (part_id) VALUES ($1)`, part.ID); err != nil {
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

// TestVideoRetentionRefs_RoundTripAndFKSetNull verifies that deleting a source
// schedule clears its references without deleting recordings or their retention
// windows.
func TestVideoRetentionRefs_RoundTripAndFKSetNull(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	contracttest.SeedUserChannel(t, ctx, a, "u-1", "b-1")

	sched, err := a.CreateSchedule(ctx, &repository.ScheduleInput{
		BroadcasterID: "b-1", RequestedBy: "u-1", Quality: repository.QualityHigh,
		IsDeleteRediff: true, TimeBeforeDelete: retentionHours(24),
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}

	v, err := a.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-1", Filename: "rec-1", DisplayName: "b-1", Status: repository.VideoStatusPending,
		Quality: repository.QualityHigh, BroadcasterID: "b-1", RecordingType: repository.RecordingTypeVideo,
		TriggerScheduleID:         &sched.ID,
		RetentionSourceScheduleID: &sched.ID,
		RetentionWindowHours:      retentionHours(24),
	})
	if err != nil {
		t.Fatalf("CreateVideo: %v", err)
	}

	got, err := a.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.TriggerScheduleID == nil || *got.TriggerScheduleID != sched.ID {
		t.Fatalf("trigger_schedule_id = %v, want %d", got.TriggerScheduleID, sched.ID)
	}
	if got.RetentionSourceScheduleID == nil || *got.RetentionSourceScheduleID != sched.ID {
		t.Fatalf("retention_source_schedule_id = %v, want %d", got.RetentionSourceScheduleID, sched.ID)
	}
	if got.RetentionWindowHours == nil || *got.RetentionWindowHours != 24 {
		t.Fatalf("retention_window_hours = %v, want 24", got.RetentionWindowHours)
	}

	if err := a.DeleteSchedule(ctx, sched.ID); err != nil {
		t.Fatalf("DeleteSchedule: %v", err)
	}
	got, err = a.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo after schedule delete: %v", err)
	}
	if got.TriggerScheduleID != nil || got.RetentionSourceScheduleID != nil {
		t.Fatalf("refs not nulled after schedule delete: trigger=%v source=%v", got.TriggerScheduleID, got.RetentionSourceScheduleID)
	}
	if got.RetentionWindowHours == nil || *got.RetentionWindowHours != 24 {
		t.Fatalf("retention_window_hours changed by schedule delete: %v, want 24 preserved", got.RetentionWindowHours)
	}

	if err := a.MarkVideoDone(ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("MarkVideoDone: %v", err)
	}
	vids, err := a.ListRetentionCandidates(ctx, time.Now().Add(48*time.Hour), 0, 100)
	if err != nil {
		t.Fatalf("ListRetentionCandidates: %v", err)
	}
	found := false
	for _, rv := range vids {
		if rv.VideoID == v.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("video %d not a retention candidate after its schedule was deleted; its captured window must still govern", v.ID)
	}
}
