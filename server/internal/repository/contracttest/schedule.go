package contracttest

import (
	"context"
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

// testScheduleUpsertPreservesTriggerCount guards operational history.
// UpdateSchedule deliberately omits trigger_count and last_triggered_at from
// the SET clause: if someone adds those fields later ("let me also update this
// while I'm here"), operators lose the fire-history the dashboard uses to
// answer "is this schedule actually working?" and the retention task uses to
// pick what to prune.
func testScheduleUpsertPreservesTriggerCount(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u-1", "b-1")

	created, err := repo.CreateSchedule(ctx, &repository.ScheduleInput{
		BroadcasterID: "b-1", RequestedBy: "u-1", Quality: "HIGH",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.RecordingType != repository.RecordingTypeVideo {
		t.Fatalf("created recording_type = %q, want video default", created.RecordingType)
	}
	if created.ForceH264 {
		t.Fatalf("created force_h264 = true, want false default")
	}

	// Fire the schedule twice so trigger_count is non-zero and
	// last_triggered_at is set, the very state the test needs to defend.
	for range 2 {
		if err := repo.RecordScheduleTrigger(ctx, created.ID); err != nil {
			t.Fatalf("record trigger: %v", err)
		}
	}
	before, err := repo.GetSchedule(ctx, created.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if before.TriggerCount != 2 || before.LastTriggeredAt == nil {
		t.Fatalf("setup precondition failed: count=%d triggered=%v", before.TriggerCount, before.LastTriggeredAt)
	}

	updated, err := repo.UpdateSchedule(ctx, created.ID, &repository.ScheduleInput{
		BroadcasterID: "b-1", RequestedBy: "u-1", RecordingType: repository.RecordingTypeAudio,
		Quality: "MEDIUM", ForceH264: true, IsDisabled: true,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Quality != "MEDIUM" || !updated.IsDisabled {
		t.Errorf("update didn't apply: quality=%s disabled=%v", updated.Quality, updated.IsDisabled)
	}
	if updated.RecordingType != repository.RecordingTypeAudio {
		t.Errorf("update didn't apply recording_type: got %q, want audio", updated.RecordingType)
	}
	if updated.ForceH264 {
		t.Errorf("audio update stored force_h264=true, want false")
	}
	if updated.TriggerCount != 2 {
		t.Errorf("UpdateSchedule clobbered trigger_count: was 2, now %d", updated.TriggerCount)
	}
	if updated.LastTriggeredAt == nil || !updated.LastTriggeredAt.Equal(*before.LastTriggeredAt) {
		t.Errorf("UpdateSchedule clobbered last_triggered_at: was %v, now %v", before.LastTriggeredAt, updated.LastTriggeredAt)
	}
}

// testScheduleFilterLinkFailureRollsBack pins that creating/updating a schedule
// with filters is atomic: a bad category reference must roll back the whole
// operation, leaving no orphaned schedule and not mutating an existing one.
func testScheduleFilterLinkFailureRollsBack(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u-tx", "b-tx")
	if _, err := repo.UpsertCategory(ctx, &repository.Category{ID: "game-1", Name: "Game 1"}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	tag, err := repo.UpsertTag(ctx, "tag-one")
	if err != nil {
		t.Fatalf("seed tag: %v", err)
	}

	_, err = repo.CreateScheduleWithFilters(ctx, &repository.ScheduleInput{
		BroadcasterID: "b-tx",
		RequestedBy:   "u-tx",
		Quality:       repository.QualityHigh,
		HasCategories: true,
	}, repository.ScheduleFilterInput{
		CategoryIDs: []string{"missing-game"},
	})
	if err == nil {
		t.Fatal("CreateScheduleWithFilters with missing category succeeded, want FK error")
	}
	if _, err := repo.GetScheduleForUserChannel(ctx, "b-tx", "u-tx"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("GetScheduleForUserChannel after failed create err = %v, want ErrNotFound", err)
	}

	created, err := repo.CreateScheduleWithFilters(ctx, &repository.ScheduleInput{
		BroadcasterID: "b-tx",
		RequestedBy:   "u-tx",
		Quality:       repository.QualityLow,
		HasCategories: true,
		HasTags:       true,
	}, repository.ScheduleFilterInput{
		CategoryIDs: []string{"game-1"},
		TagIDs:      []int64{tag.ID},
	})
	if err != nil {
		t.Fatalf("CreateScheduleWithFilters valid: %v", err)
	}

	_, err = repo.UpdateScheduleWithFilters(ctx, created.ID, &repository.ScheduleInput{
		BroadcasterID: "b-tx",
		RequestedBy:   "u-tx",
		Quality:       repository.QualityHigh,
		HasCategories: true,
		HasTags:       true,
	}, repository.ScheduleFilterInput{
		CategoryIDs: []string{"missing-game"},
		TagIDs:      []int64{tag.ID},
	})
	if err == nil {
		t.Fatal("UpdateScheduleWithFilters with missing category succeeded, want FK error")
	}
	got, err := repo.GetSchedule(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetSchedule after failed update: %v", err)
	}
	if got.Quality != repository.QualityLow {
		t.Fatalf("quality after failed update = %q, want original %q", got.Quality, repository.QualityLow)
	}
	cats, err := repo.ListScheduleCategories(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListScheduleCategories after failed update: %v", err)
	}
	if len(cats) != 1 || cats[0].ID != "game-1" {
		t.Fatalf("categories after failed update = %+v, want only game-1", cats)
	}
	tags, err := repo.ListScheduleTags(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListScheduleTags after failed update: %v", err)
	}
	if len(tags) != 1 || tags[0].ID != tag.ID {
		t.Fatalf("tags after failed update = %+v, want only tag %d", tags, tag.ID)
	}
}
