package contracttest

import (
	"context"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testVideoStatisticsTotals(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	assertTotals := func(userID string, want repository.VideoStatsTotals) {
		t.Helper()
		got, err := repo.VideoStatsTotals(ctx, userID)
		if err != nil {
			t.Fatal(err)
		}
		if *got != want {
			t.Fatalf("totals for %q = %+v, want %+v", userID, *got, want)
		}
	}
	assertTotals("viewer", repository.VideoStatsTotals{})
	for _, id := range []string{"viewer", "other"} {
		if _, err := repo.UpsertUser(ctx, &repository.User{ID: id, Login: id, DisplayName: id, Role: "viewer"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"a", "b", "removed-only"} {
		if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: id, BroadcasterLogin: id, BroadcasterName: id}); err != nil {
			t.Fatal(err)
		}
	}
	create := func(job, channel, status string, old bool) *repository.Video {
		t.Helper()
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID: job, Filename: job, DisplayName: job, BroadcasterID: channel,
			Status: status, Quality: repository.QualityHigh, Language: "en",
		})
		if err != nil {
			t.Fatal(err)
		}
		if old {
			h.BackdateVideoStartDownload(t, v.ID, time.Now().Add(-14*24*time.Hour))
		}
		return v
	}
	done := func(v *repository.Video, duration float64, size int64, partial, truncated bool) {
		t.Helper()
		kind := repository.CompletionKindComplete
		if partial {
			kind = repository.CompletionKindPartial
		}
		if err := repo.MarkVideoDone(ctx, v.ID, duration, size, nil, kind, truncated); err != nil {
			t.Fatal(err)
		}
	}
	a := create("a", "a", repository.VideoStatusDone, false)
	b := create("b", "a", repository.VideoStatusDone, true)
	c := create("null-metrics", "b", repository.VideoStatusDone, false)
	running := create("running", "b", repository.VideoStatusRunning, false)
	failed := create("failed", "b", repository.VideoStatusFailed, true)
	removed := create("removed", "removed-only", repository.VideoStatusDone, false)
	truncated := create("truncated", "a", repository.VideoStatusDone, false)
	done(a, 100.25, 1<<40, false, false)
	done(b, 200.5, 7, true, false)
	done(removed, 9000, 1<<42, true, true)
	done(truncated, 50.125, 3, false, true)
	for _, v := range []*repository.Video{a, c, running, failed, removed} {
		if _, err := repo.SetVideoWatchLater(ctx, "viewer", v.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []*repository.Video{a, b, removed} {
		position := 60.0
		if v == b {
			position = 200.5
		}
		if _, err := repo.UpdateVideoWatchProgress(ctx, "viewer", v.ID, position, v == b, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.SetVideoWatchLater(ctx, "other", b.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpdateVideoWatchProgress(ctx, "other", c.ID, 60, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.SoftDeleteVideo(ctx, removed.ID, repository.DeletionKindManual); err != nil {
		t.Fatal(err)
	}
	base := repository.VideoStatsTotals{
		Total: 4, TotalSize: 1<<40 + 10, TotalDuration: 350.875,
		ThisWeek: 4, Incomplete: 2, Channels: 2, Removed: 1,
	}
	assertTotals("", base)
	want := base
	want.WatchLater, want.Unwatched, want.ContinueWatching = 4, 2, 1
	assertTotals("viewer", want)
	want.WatchLater, want.Unwatched, want.ContinueWatching = 1, 3, 1
	assertTotals("other", want)
	want.WatchLater, want.Unwatched, want.ContinueWatching = 0, 4, 0
	assertTotals("unknown", want)
	for _, v := range []*repository.Video{a, b, c, running, failed, truncated} {
		if err := repo.SoftDeleteVideo(ctx, v.ID, repository.DeletionKindManual); err != nil {
			t.Fatal(err)
		}
	}
	assertTotals("viewer", repository.VideoStatsTotals{Removed: 7})
}
