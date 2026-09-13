package contracttest

import (
	"context"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/resumepolicy"
)

func testUserPlaybackSettings(t *testing.T, h Harness) {
	ctx := context.Background()
	r := h.Repo()
	SeedUserChannel(t, ctx, r, "playback-viewer", "playback-channel")
	if _, err := r.UpsertUser(ctx, &repository.User{ID: "other", Login: "other", DisplayName: "Other", Role: "viewer"}); err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct {
		job                string
		duration, position float64
	}{
		{"middle", 100, 40}, {"long-end", 1000, 980}, {"short-end", 100, 94},
	} {
		v, err := r.CreateVideo(ctx, &repository.VideoInput{JobID: tc.job, Filename: tc.job, DisplayName: tc.job, BroadcasterID: "playback-channel", Status: repository.VideoStatusDone, Quality: repository.QualityHigh})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.MarkVideoDone(ctx, v.ID, tc.duration, 100, nil, repository.CompletionKindComplete, false); err != nil {
			t.Fatal(err)
		}
		for _, user := range []string{"playback-viewer", "other"} {
			if _, err := r.UpdateVideoWatchProgress(ctx, user, v.ID, tc.position, false, time.Unix(int64(i+1), 0)); err != nil {
				t.Fatal(err)
			}
		}
	}
	assertList := func(user string, want []string) {
		t.Helper()
		assertStringSlice(t, continueWatchingJobIDs(t, ctx, r, user, 10), want)
		for _, sort := range []string{"last_watched", "created_at"} {
			assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, r, repository.ListVideosOpts{UserID: user, ContinueWatchingOnly: true, Sort: sort, Order: "desc", Limit: 1}), want)
		}
		totals, err := r.VideoStatsTotals(ctx, user)
		if err != nil || totals.ContinueWatching != int64(len(want)) {
			t.Fatalf("count: %+v, %v; want %d", totals, err, len(want))
		}
	}
	assertList("playback-viewer", []string{"short-end", "middle"})
	saved, err := r.UpdatePlaybackSettings(ctx, &repository.Settings{UserID: "playback-viewer", ResumeMinSeconds: 50, ResumeEndMarginSeconds: 10, ResumeEndMarginPercent: 5})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Timezone != "UTC" || saved.Language != "en" {
		t.Fatalf("new preferences lost locale defaults: %+v", saved)
	}
	assertList("playback-viewer", []string{"short-end", "long-end"})
	assertList("other", []string{"short-end", "middle"})
	// An older client can still save locale without resetting playback preferences.
	saved, err = r.UpsertSettings(ctx, &repository.Settings{UserID: "playback-viewer", Timezone: "Europe/Paris", DatetimeFormat: "EU", Language: "fr"})
	if err != nil || saved.ResumeMinSeconds != 50 || saved.ResumeEndMarginSeconds != 10 {
		t.Fatalf("locale overwrote playback: %+v, %v", saved, err)
	}
	saved, err = r.UpdatePlaybackSettings(ctx, &repository.Settings{UserID: "playback-viewer", ResumeMinSeconds: 5, ResumeEndMarginSeconds: 600, ResumeEndMarginPercent: 20})
	if err != nil || saved.Language != "fr" || saved.Timezone != "Europe/Paris" {
		t.Fatalf("playback overwrote locale: %+v, %v", saved, err)
	}
	assertList("playback-viewer", []string{"middle"})
	// Database defaults and the no-settings-row SQL fallback must stay equivalent.
	defaults, err := r.UpsertSettings(ctx, &repository.Settings{UserID: "other", Timezone: "UTC", DatetimeFormat: "ISO", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if defaults.ResumeMinSeconds != resumepolicy.MinSeconds || defaults.ResumeEndMarginSeconds != resumepolicy.EndMarginSeconds || defaults.ResumeEndMarginPercent != resumepolicy.EndMarginPercent {
		t.Fatalf("database defaults diverged: %+v", defaults)
	}
	assertList("other", []string{"short-end", "middle"})
	ensured, err := r.EnsureSettings(ctx, "playback-viewer")
	if err != nil || *ensured != *saved {
		t.Fatalf("default initialization overwrote saved settings: %+v, %v; want %+v", ensured, err, saved)
	}
	if _, err := r.UpsertUser(ctx, &repository.User{ID: "new-viewer", Login: "new-viewer", DisplayName: "New", Role: "viewer"}); err != nil {
		t.Fatal(err)
	}
	created, err := r.EnsureSettings(ctx, "new-viewer")
	if err != nil {
		t.Fatal(err)
	}
	if created.Timezone != "UTC" || created.DatetimeFormat != "ISO" || created.Language != "en" || created.ResumeMinSeconds != resumepolicy.MinSeconds || created.ResumeEndMarginSeconds != resumepolicy.EndMarginSeconds || created.ResumeEndMarginPercent != resumepolicy.EndMarginPercent {
		t.Fatalf("default initialization = %+v", created)
	}
	ensured, err = r.EnsureSettings(ctx, "new-viewer")
	if err != nil || *ensured != *created {
		t.Fatalf("repeated initialization changed defaults or timestamps: %+v, %v; want %+v", ensured, err, created)
	}
}
