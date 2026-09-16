package contracttest

import (
	"context"
	"maps"
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

func testVideoStatsByStatus(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	byStatus := func() map[string]int64 {
		t.Helper()
		rows, err := repo.VideoStatsByStatus(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out := make(map[string]int64, len(rows))
		for _, r := range rows {
			if _, dup := out[r.Status]; dup {
				t.Fatalf("status %s repeats in %+v", r.Status, rows)
			}
			out[r.Status] = r.Count
		}
		return out
	}
	if got := byStatus(); len(got) != 0 {
		t.Fatalf("stats of an empty library = %v", got)
	}
	SeedUserChannel(t, ctx, repo, "owner", "execution-channel")
	create := func(job string) *repository.Video {
		t.Helper()
		v, err := repo.CreateVideo(ctx, executionInput(job))
		if err != nil {
			t.Fatalf("create %s: %v", job, err)
		}
		return v
	}
	create("pending")
	pendingGone := create("pending-gone")
	running := create("running")
	if err := repo.UpdateVideoStatus(ctx, running.ID, repository.VideoStatusRunning); err != nil {
		t.Fatal(err)
	}
	for _, v := range []*repository.Video{create("done"), create("done-gone")} {
		if err := repo.MarkVideoDone(ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
			t.Fatal(err)
		}
	}
	failed := create("failed")
	if err := repo.MarkVideoFailed(ctx, failed.ID, "boom", repository.CompletionKindComplete, true); err != nil {
		t.Fatal(err)
	}
	if err := repo.SoftDeleteVideo(ctx, pendingGone.ID, repository.DeletionKindManual); err != nil {
		t.Fatal(err)
	}
	doneGone, err := repo.GetVideoByJobID(ctx, "done-gone")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SoftDeleteVideo(ctx, doneGone.ID, repository.DeletionKindRetention); err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{
		repository.VideoStatusPending: 1, repository.VideoStatusRunning: 1,
		repository.VideoStatusDone: 1, repository.VideoStatusFailed: 1,
	}
	if got := byStatus(); !maps.Equal(got, want) {
		t.Fatalf("stats by status = %v, want %v", got, want)
	}
	if err := repo.SoftDeleteVideo(ctx, failed.ID, repository.DeletionKindManual); err != nil {
		t.Fatal(err)
	}
	delete(want, repository.VideoStatusFailed)
	if got := byStatus(); !maps.Equal(got, want) {
		t.Fatalf("stats after removing every failed row = %v, want %v", got, want)
	}
}

func testVideoStatsTotalsByBroadcaster(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	assertTotals := func(broadcasterID string, want repository.VideoStatsTotals) {
		t.Helper()
		got, err := repo.VideoStatsTotalsByBroadcaster(ctx, broadcasterID)
		if err != nil {
			t.Fatal(err)
		}
		if *got != want {
			t.Fatalf("totals for %q = %+v, want %+v", broadcasterID, *got, want)
		}
	}
	assertTotals("a", repository.VideoStatsTotals{})
	for _, id := range []string{"a", "b"} {
		if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: id, BroadcasterLogin: id, BroadcasterName: id}); err != nil {
			t.Fatal(err)
		}
	}
	create := func(job, channel, status string) *repository.Video {
		t.Helper()
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID: job, Filename: job, DisplayName: job, BroadcasterID: channel,
			Status: status, Quality: repository.QualityHigh, Language: "en",
		})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	done := func(job, channel string, duration float64, size int64, kind string) *repository.Video {
		t.Helper()
		v := create(job, channel, repository.VideoStatusPending)
		if err := repo.MarkVideoDone(ctx, v.ID, duration, size, nil, kind, false); err != nil {
			t.Fatal(err)
		}
		return v
	}
	done("big", "a", 100.25, 1<<40, repository.CompletionKindComplete)
	done("small", "a", 200.5, 7, repository.CompletionKindPartial)
	create("bare", "a", repository.VideoStatusDone)
	create("running", "a", repository.VideoStatusRunning)
	failed := create("failed", "a", repository.VideoStatusPending)
	if err := repo.MarkVideoFailed(ctx, failed.ID, "boom", repository.CompletionKindPartial, true); err != nil {
		t.Fatal(err)
	}
	gone := done("gone", "a", 9000, 1<<42, repository.CompletionKindComplete)
	if err := repo.SoftDeleteVideo(ctx, gone.ID, repository.DeletionKindRetention); err != nil {
		t.Fatal(err)
	}
	done("other", "b", 50.125, 3, repository.CompletionKindComplete)
	assertTotals("a", repository.VideoStatsTotals{Total: 3, TotalSize: 1<<40 + 7, TotalDuration: 300.75})
	assertTotals("b", repository.VideoStatsTotals{Total: 1, TotalSize: 3, TotalDuration: 50.125})
	assertTotals("missing", repository.VideoStatsTotals{})
	if library, err := repo.VideoStatsTotals(ctx, ""); err != nil || library.Removed != 1 {
		t.Fatalf("library totals = %+v, %v; the tombstone counts there and nowhere per broadcaster", library, err)
	}
}
