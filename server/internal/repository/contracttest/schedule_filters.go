package contracttest

import (
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

func scheduleIDs(schedules []repository.DownloadSchedule) []int64 {
	out := make([]int64, len(schedules))
	for i, s := range schedules {
		out[i] = s.ID
	}
	return out
}

func testScheduleFiltersAndToggle(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "bc-1")
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "bc-2", BroadcasterLogin: "bc-2", BroadcasterName: "bc-2"}); err != nil {
		t.Fatal(err)
	}
	first, err := repo.CreateSchedule(ctx, &repository.ScheduleInput{BroadcasterID: "bc-1", RequestedBy: "owner", Quality: repository.QualityHigh})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.CreateSchedule(ctx, &repository.ScheduleInput{BroadcasterID: "bc-2", RequestedBy: "owner", Quality: repository.QualityHigh})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []repository.Category{{ID: "c1", Name: "Chess"}, {ID: "c2", Name: "Art"}} {
		if _, err := repo.UpsertCategory(ctx, &c); err != nil {
			t.Fatal(err)
		}
	}
	speedrun, err := repo.UpsertTag(ctx, "speedrun")
	if err != nil {
		t.Fatal(err)
	}
	chill, err := repo.UpsertTag(ctx, "chill")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"c1", "c2"} {
		if err := repo.LinkScheduleCategory(ctx, first.ID, id); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []int64{speedrun.ID, chill.ID} {
		if err := repo.LinkScheduleTag(ctx, first.ID, id); err != nil {
			t.Fatal(err)
		}
	}
	categories, err := repo.ListScheduleCategories(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, categoryNamesOf(categories), []string{"Art", "Chess"})
	if err := repo.UnlinkScheduleCategory(ctx, first.ID, "c2"); err != nil {
		t.Fatal(err)
	}
	categories, err = repo.ListScheduleCategories(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, categoryNamesOf(categories), []string{"Chess"})
	if err := repo.ClearScheduleCategories(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if categories, err := repo.ListScheduleCategories(ctx, first.ID); err != nil || len(categories) != 0 {
		t.Fatalf("categories after clear = %+v, %v", categories, err)
	}
	tags, err := repo.ListScheduleTags(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, tagNames(tags), []string{"chill", "speedrun"})
	if err := repo.UnlinkScheduleTag(ctx, first.ID, chill.ID); err != nil {
		t.Fatal(err)
	}
	tags, err = repo.ListScheduleTags(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, tagNames(tags), []string{"speedrun"})
	if err := repo.ClearScheduleTags(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if tags, err := repo.ListScheduleTags(ctx, first.ID); err != nil || len(tags) != 0 {
		t.Fatalf("tags after clear = %+v, %v", tags, err)
	}
	if active, err := repo.ListActiveSchedulesForBroadcaster(ctx, "bc-1"); err != nil || len(active) != 1 || active[0].ID != first.ID {
		t.Fatalf("active schedules = %+v, %v", active, err)
	}
	if toggled, err := repo.ToggleSchedule(ctx, first.ID); err != nil || !toggled.IsDisabled {
		t.Fatalf("toggled off = %+v, %v", toggled, err)
	}
	if active, err := repo.ListActiveSchedulesForBroadcaster(ctx, "bc-1"); err != nil || len(active) != 0 {
		t.Fatalf("disabled schedule still active = %+v, %v", active, err)
	}
	if toggled, err := repo.ToggleSchedule(ctx, first.ID); err != nil || toggled.IsDisabled {
		t.Fatalf("toggled back on = %+v, %v", toggled, err)
	}
	if _, err := repo.ToggleSchedule(ctx, first.ID+second.ID+1); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("toggling a missing schedule: %v", err)
	}
	mine, err := repo.ListSchedulesForUser(ctx, "owner", 10, 0)
	if err != nil || len(mine) != 2 {
		t.Fatalf("owner schedules = %v, %v", scheduleIDs(mine), err)
	}
	if page, err := repo.ListSchedulesForUser(ctx, "owner", 1, 1); err != nil || len(page) != 1 {
		t.Fatalf("second page = %v, %v", scheduleIDs(page), err)
	}
	if none, err := repo.ListSchedulesForUser(ctx, "nobody", 10, 0); err != nil || len(none) != 0 {
		t.Fatalf("schedules of an unknown user = %v, %v", scheduleIDs(none), err)
	}
	if err := repo.DeleteSchedule(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetSchedule(ctx, first.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("deleted schedule survived: %v", err)
	}
	if mine, err := repo.ListSchedulesForUser(ctx, "owner", 10, 0); err != nil || len(mine) != 1 || mine[0].ID != second.ID {
		t.Fatalf("owner schedules after delete = %v, %v", scheduleIDs(mine), err)
	}
	if err := repo.DeleteSchedule(ctx, first.ID); err != nil {
		t.Fatalf("deleting a missing schedule: %v", err)
	}
}
