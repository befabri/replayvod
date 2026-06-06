package pgadapter

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func retentionHours(v int64) *int64 { return &v }

func seedRetentionSchedule(t *testing.T, ctx context.Context, a *PGAdapter, userID, broadcasterID string, deleteRediff bool, hours *int64, disabled bool) {
	t.Helper()
	seedUserChannel(t, ctx, a, userID, broadcasterID)
	if _, err := a.CreateSchedule(ctx, &repository.ScheduleInput{
		BroadcasterID:    broadcasterID,
		RequestedBy:      userID,
		Quality:          repository.QualityHigh,
		IsDeleteRediff:   deleteRediff,
		TimeBeforeDelete: hours,
		IsDisabled:       disabled,
	}); err != nil {
		t.Fatalf("seed schedule (%s/%s): %v", broadcasterID, userID, err)
	}
}

func seedRetentionDoneVideo(t *testing.T, ctx context.Context, a *PGAdapter, jobID, filename, broadcasterID string) *repository.Video {
	t.Helper()
	v, err := a.CreateVideo(ctx, &repository.VideoInput{
		JobID:                jobID,
		Filename:             filename,
		DisplayName:          broadcasterID,
		Status:               repository.VideoStatusPending,
		Quality:              repository.QualityHigh,
		BroadcasterID:        broadcasterID,
		RecordingType:        repository.RecordingTypeVideo,
		RetentionWindowHours: retentionHours(1),
	})
	if err != nil {
		t.Fatalf("create video %s: %v", filename, err)
	}
	if err := a.MarkVideoDone(ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("mark video %s done: %v", filename, err)
	}
	return v
}

func seedRetentionFailedVideo(t *testing.T, ctx context.Context, a *PGAdapter, jobID, filename, broadcasterID, completionKind string) *repository.Video {
	t.Helper()
	v, err := a.CreateVideo(ctx, &repository.VideoInput{
		JobID:                jobID,
		Filename:             filename,
		DisplayName:          broadcasterID,
		Status:               repository.VideoStatusPending,
		Quality:              repository.QualityHigh,
		BroadcasterID:        broadcasterID,
		RecordingType:        repository.RecordingTypeVideo,
		RetentionWindowHours: retentionHours(1),
	})
	if err != nil {
		t.Fatalf("create video %s: %v", filename, err)
	}
	if err := a.MarkVideoFailed(ctx, v.ID, "synthetic failure", completionKind, true); err != nil {
		t.Fatalf("mark video %s failed: %v", filename, err)
	}
	return v
}

func seedRetentionDoneVideoNoPolicy(t *testing.T, ctx context.Context, a *PGAdapter, jobID, filename, broadcasterID string) *repository.Video {
	t.Helper()
	v, err := a.CreateVideo(ctx, &repository.VideoInput{
		JobID:         jobID,
		Filename:      filename,
		DisplayName:   broadcasterID,
		Status:        repository.VideoStatusPending,
		Quality:       repository.QualityHigh,
		BroadcasterID: broadcasterID,
		RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("create video %s: %v", filename, err)
	}
	if err := a.MarkVideoDone(ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("mark video %s done: %v", filename, err)
	}
	return v
}

// TestRetentionQueries_FilterContracts is the Postgres mirror for the
// retention query contract. It exercises the generated PG SQL and adapter
// conversions directly so SQLite-only coverage cannot hide dialect drift.
//
// ListFinishedVideosForRetention must return due visible terminal rows that can
// own reclaimable artifacts only when the recording has a snapshotted retention
// policy: DONE plus FAILED partial/cancelled, never active, soft-deleted,
// no-salvage FAILED, no-policy recordings, terminal rows missing downloaded_at,
// or rows still inside their retention window.
func TestRetentionQueries_FilterContracts(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	seedRetentionSchedule(t, ctx, a, "u-1", "b-del", true, retentionHours(24), false)
	vDel := seedRetentionDoneVideo(t, ctx, a, "job-del", "rec-del", "b-del")
	vPartial := seedRetentionFailedVideo(t, ctx, a, "job-partial", "rec-partial", "b-del", repository.CompletionKindPartial)
	vCancelled := seedRetentionFailedVideo(t, ctx, a, "job-cancelled", "rec-cancelled", "b-del", repository.CompletionKindCancelled)
	vFailedNoSalvage := seedRetentionFailedVideo(t, ctx, a, "job-failed-nosalvage", "rec-failed-nosalvage", "b-del", repository.CompletionKindComplete)

	seedRetentionSchedule(t, ctx, a, "u-1", "b-min", true, retentionHours(48), false)
	seedRetentionSchedule(t, ctx, a, "u-2", "b-min", true, retentionHours(12), false)
	vMin := seedRetentionDoneVideo(t, ctx, a, "job-min", "rec-min", "b-min")

	seedRetentionSchedule(t, ctx, a, "u-1", "b-keep", true, retentionHours(1), false)
	vKeep := seedRetentionDoneVideoNoPolicy(t, ctx, a, "job-keep", "rec-keep", "b-keep")

	seedRetentionSchedule(t, ctx, a, "u-1", "b-disabled", true, retentionHours(24), true)
	vDisabled := seedRetentionDoneVideoNoPolicy(t, ctx, a, "job-disabled", "rec-disabled", "b-disabled")

	if _, err := a.CreateVideo(ctx, &repository.VideoInput{
		JobID:                "job-pending",
		Filename:             "rec-pending",
		DisplayName:          "b-del",
		Status:               repository.VideoStatusPending,
		Quality:              repository.QualityHigh,
		BroadcasterID:        "b-del",
		RecordingType:        repository.RecordingTypeVideo,
		RetentionWindowHours: retentionHours(1),
	}); err != nil {
		t.Fatalf("create pending video: %v", err)
	}
	vDoneNoDownloadedAt, err := a.CreateVideo(ctx, &repository.VideoInput{
		JobID:                "job-done-no-downloaded-at",
		Filename:             "rec-done-no-downloaded-at",
		DisplayName:          "b-del",
		Status:               repository.VideoStatusDone,
		Quality:              repository.QualityHigh,
		BroadcasterID:        "b-del",
		RecordingType:        repository.RecordingTypeVideo,
		RetentionWindowHours: retentionHours(1),
	})
	if err != nil {
		t.Fatalf("create done video with null downloaded_at: %v", err)
	}
	vGone := seedRetentionDoneVideo(t, ctx, a, "job-gone", "rec-gone", "b-del")
	if err := a.SoftDeleteVideo(ctx, vGone.ID, repository.DeletionKindManual); err != nil {
		t.Fatalf("soft delete video: %v", err)
	}
	vNotDue, err := a.CreateVideo(ctx, &repository.VideoInput{
		JobID:                "job-not-due",
		Filename:             "rec-not-due",
		DisplayName:          "b-del",
		Status:               repository.VideoStatusPending,
		Quality:              repository.QualityHigh,
		BroadcasterID:        "b-del",
		RecordingType:        repository.RecordingTypeVideo,
		RetentionWindowHours: retentionHours(24),
	})
	if err != nil {
		t.Fatalf("create not-due video: %v", err)
	}
	if err := a.MarkVideoDone(ctx, vNotDue.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("mark not-due video done: %v", err)
	}
	vUnfrozenWebhook := seedRetentionDoneVideo(t, ctx, a, "job-unfrozen-webhook", "rec-unfrozen-webhook", "b-del")
	unfrozenDelivery, err := a.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
		MessageID:     "msg-unfrozen-webhook",
		DedupeKey:     "dedupe-unfrozen-webhook",
		Event:         "recording.completed",
		VideoID:       vUnfrozenWebhook.ID,
		NextAttemptAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateRecordingWebhookDelivery: %v", err)
	}

	videos, err := a.ListFinishedVideosForRetention(ctx, time.Now().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("ListFinishedVideosForRetention: %v", err)
	}
	gotVids := make(map[int64]bool, len(videos))
	for _, v := range videos {
		if v.DownloadedAt == nil {
			t.Fatalf("video %d has nil downloaded_at; query filters it NOT NULL", v.VideoID)
		}
		if v.RetentionWindowHours == nil {
			t.Fatalf("video %d has nil retention_window_hours; query filters it NOT NULL", v.VideoID)
		}
		gotVids[v.VideoID] = true
	}
	wantVids := map[int64]bool{
		vDel.ID:       true,
		vPartial.ID:   true,
		vCancelled.ID: true,
		vMin.ID:       true,
	}
	if !reflect.DeepEqual(gotVids, wantVids) {
		t.Fatalf("retention videos = %v, want %v (no-policy ids %d/%d, failed-no-salvage id %d, done-null-downloaded-at id %d, not-due id %d, and unfrozen-webhook id %d excluded)",
			gotVids, wantVids, vKeep.ID, vDisabled.ID, vFailedNoSalvage.ID, vDoneNoDownloadedAt.ID, vNotDue.ID, vUnfrozenWebhook.ID)
	}

	if err := a.SetRecordingWebhookDeliveryFrozenParts(ctx, unfrozenDelivery.ID, "[]"); err != nil {
		t.Fatalf("SetRecordingWebhookDeliveryFrozenParts: %v", err)
	}
	videos, err = a.ListFinishedVideosForRetention(ctx, time.Now().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("ListFinishedVideosForRetention after freeze: %v", err)
	}
	gotVids = make(map[int64]bool, len(videos))
	for _, v := range videos {
		gotVids[v.VideoID] = true
	}
	wantVids[vUnfrozenWebhook.ID] = true
	if !reflect.DeepEqual(gotVids, wantVids) {
		t.Fatalf("retention videos after frozen webhook = %v, want %v", gotVids, wantVids)
	}
}

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

// TestVideoRetentionRefs_RoundTripAndFKSetNull pins two things the per-video
// retention model depends on. (1) The provenance columns round-trip through
// CreateVideo -> GetVideo (a column-order typo in the INSERT/scan would surface
// here). (2) The FK is ON DELETE SET NULL, not CASCADE/RESTRICT: deleting the
// source schedule nulls the refs but must neither delete the recording nor
// change its snapshotted window, so the recording stays a retention candidate
// governed by its own captured policy rather than the live schedule.
func TestVideoRetentionRefs_RoundTripAndFKSetNull(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	seedUserChannel(t, ctx, a, "u-1", "b-1")

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
	vids, err := a.ListFinishedVideosForRetention(ctx, time.Now().Add(48*time.Hour))
	if err != nil {
		t.Fatalf("ListFinishedVideosForRetention: %v", err)
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
