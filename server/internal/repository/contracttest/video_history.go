package contracttest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testListVideosPageScope(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-scope", BroadcasterLogin: "scope", BroadcasterName: "Scope",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	base := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	mk := func(jobID string, startedAt time.Time) *repository.Video {
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: "Scope", Status: repository.VideoStatusDone,
			Quality: repository.QualityHigh, BroadcasterID: "bc-scope", Language: "en",
		})
		if err != nil {
			t.Fatalf("create %s: %v", jobID, err)
		}
		h.BackdateVideoStartDownload(t, v.ID, startedAt)
		return v
	}
	mk("job-scope-live", base)
	gone := mk("job-scope-gone", base.Add(time.Hour))
	if err := repo.SoftDeleteVideo(ctx, gone.ID, repository.DeletionKindRetention); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	baseOpts := repository.ListVideosOpts{Sort: "created_at", Order: "desc", Limit: 10}
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, repo, baseOpts), []string{"job-scope-live"})

	removed := baseOpts
	removed.Scope = "removed"
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, repo, removed), []string{"job-scope-gone"})

	all := baseOpts
	all.Scope = "all"
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, repo, all), []string{"job-scope-gone", "job-scope-live"})

	page, err := repo.ListVideosPage(ctx, removed, nil)
	if err != nil {
		t.Fatalf("ListVideosPage removed: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].DeletionKind == nil ||
		*page.Items[0].DeletionKind != repository.DeletionKindRetention {
		t.Fatalf("removed row deletion_kind: got %+v, want %q", page.Items, repository.DeletionKindRetention)
	}
	if page.Items[0].DeletedAt == nil {
		t.Fatal("removed row deleted_at must be set")
	}
}

func testListVideosSortDimensions(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-1", BroadcasterLogin: "bc", BroadcasterName: "BC",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	type seed struct {
		jobID, displayName string
		duration           float64
		size               int64
	}
	seeds := []seed{
		{"j-a", "Alpha", 100, 500},
		{"j-b", "Bravo", 500, 5000},
		{"j-c", "Charlie", 300, 1000},
	}
	ids := make(map[string]int64, len(seeds))
	for _, s := range seeds {
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID:         s.jobID,
			Filename:      s.jobID,
			DisplayName:   s.displayName,
			Status:        repository.VideoStatusDone,
			Quality:       repository.QualityHigh,
			BroadcasterID: "bc-1",
			ViewerCount:   0,
			Language:      "en",
			RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("create %s: %v", s.displayName, err)
		}
		if err := repo.MarkVideoDone(ctx, v.ID, s.duration, s.size, nil, "complete", false); err != nil {
			t.Fatalf("mark done %s: %v", s.displayName, err)
		}
		ids[s.displayName] = v.ID
	}

	// Explicit start times so created_at sort direction is tested directly,
	// not the id-DESC tiebreaker behavior.
	base := time.Now().UTC().Truncate(time.Second)
	for i, name := range []string{"Alpha", "Bravo", "Charlie"} {
		h.BackdateVideoStartDownload(t, ids[name], base.Add(time.Duration(i)*time.Minute))
	}

	cases := []struct {
		name      string
		opts      repository.ListVideosOpts
		wantOrder []string
	}{
		{"default (empty sort/order) = created desc", repository.ListVideosOpts{Limit: 10}, []string{"Charlie", "Bravo", "Alpha"}},
		{"duration desc", repository.ListVideosOpts{Sort: "duration", Order: "desc", Limit: 10}, []string{"Bravo", "Charlie", "Alpha"}},
		{"duration asc", repository.ListVideosOpts{Sort: "duration", Order: "asc", Limit: 10}, []string{"Alpha", "Charlie", "Bravo"}},
		{"size desc", repository.ListVideosOpts{Sort: "size", Order: "desc", Limit: 10}, []string{"Bravo", "Charlie", "Alpha"}},
		{"size asc", repository.ListVideosOpts{Sort: "size", Order: "asc", Limit: 10}, []string{"Alpha", "Charlie", "Bravo"}},
		{"channel asc", repository.ListVideosOpts{Sort: "channel", Order: "asc", Limit: 10}, []string{"Alpha", "Bravo", "Charlie"}},
		{"channel desc", repository.ListVideosOpts{Sort: "channel", Order: "desc", Limit: 10}, []string{"Charlie", "Bravo", "Alpha"}},
		{"created_at asc", repository.ListVideosOpts{Sort: "created_at", Order: "asc", Limit: 10}, []string{"Alpha", "Bravo", "Charlie"}},
		{"created_at desc = default", repository.ListVideosOpts{Sort: "created_at", Order: "desc", Limit: 10}, []string{"Charlie", "Bravo", "Alpha"}},
		{"status filter narrows result", repository.ListVideosOpts{Status: "DONE", Limit: 10}, []string{"Charlie", "Bravo", "Alpha"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := repo.ListVideos(ctx, tc.opts)
			if err != nil {
				t.Fatalf("ListVideos: %v", err)
			}
			if len(got) != len(tc.wantOrder) {
				t.Fatalf("row count: want %d got %d", len(tc.wantOrder), len(got))
			}
			for i, want := range tc.wantOrder {
				if got[i].DisplayName != want {
					t.Errorf("row %d: want %s got %s", i, want, got[i].DisplayName)
				}
			}
		})
	}
}

func testListVideosPageTerminalOnlyHistoryWhen(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-history", BroadcasterLogin: "history", BroadcasterName: "History",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	mk := func(jobID, status string, offset time.Duration) *repository.Video {
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: "History", Status: status,
			Quality: repository.QualityHigh, BroadcasterID: "bc-history", Language: "en",
		})
		if err != nil {
			t.Fatalf("create %s: %v", jobID, err)
		}
		h.BackdateVideoStartDownload(t, v.ID, base.Add(offset))
		return v
	}
	done := mk("job-history-done", repository.VideoStatusDone, 0)
	failed := mk("job-history-failed", repository.VideoStatusFailed, time.Hour)
	removed := mk("job-history-removed", repository.VideoStatusDone, 2*time.Hour)
	removedRetention := mk("job-history-removed-retention", repository.VideoStatusDone, 150*time.Minute)
	mk("job-history-running", repository.VideoStatusRunning, 3*time.Hour)
	mk("job-history-pending", repository.VideoStatusPending, 4*time.Hour)

	h.BackdateVideoDownloadedAt(t, done.ID, base.Add(48*time.Hour))
	h.BackdateVideoDownloadedAt(t, failed.ID, base.Add(24*time.Hour))
	if err := repo.SoftDeleteVideo(ctx, removed.ID, repository.DeletionKindManual); err != nil {
		t.Fatalf("soft delete removed: %v", err)
	}
	if err := repo.SoftDeleteVideo(ctx, removedRetention.ID, repository.DeletionKindRetention); err != nil {
		t.Fatalf("soft delete retention removed: %v", err)
	}
	h.BackdateVideoDeletedAt(t, removed.ID, base.Add(72*time.Hour))
	h.BackdateVideoDeletedAt(t, removedRetention.ID, base.Add(36*time.Hour))

	opts := repository.ListVideosOpts{
		Sort:         "history_when",
		Order:        "desc",
		Scope:        "all",
		TerminalOnly: true,
		Limit:        2,
	}
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, repo, opts), []string{
		"job-history-removed",
		"job-history-done",
		"job-history-removed-retention",
		"job-history-failed",
	})

	removedOpts := opts
	removedOpts.Scope = "removed"
	removedOpts.Limit = 1
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, repo, removedOpts), []string{
		"job-history-removed",
		"job-history-removed-retention",
	})
}

func testVideoUserStateFiltersAndStatistics(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	userID := "user-video-state"
	if _, err := repo.UpsertUser(ctx, &repository.User{
		ID: userID, Login: "state", DisplayName: "State", Role: "viewer",
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	otherUserID := "user-video-state-other"
	if _, err := repo.UpsertUser(ctx, &repository.User{
		ID: otherUserID, Login: "state-other", DisplayName: "State Other", Role: "viewer",
	}); err != nil {
		t.Fatalf("seed other user: %v", err)
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-state", BroadcasterLogin: "state", BroadcasterName: "State",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	mk := func(jobID, status string) *repository.Video {
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: "State", Status: status,
			Quality: repository.QualityHigh, BroadcasterID: "bc-state", Language: "en",
			RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("create %s: %v", jobID, err)
		}
		return v
	}
	watched := mk("job-state-watched", repository.VideoStatusDone)
	later := mk("job-state-later", repository.VideoStatusDone)
	plain := mk("job-state-plain", repository.VideoStatusDone)
	running := mk("job-state-running", repository.VideoStatusRunning)
	failed := mk("job-state-failed", repository.VideoStatusFailed)

	if state, err := repo.SetVideoWatchLater(ctx, userID, later.ID, true); err != nil {
		t.Fatalf("set watch later: %v", err)
	} else if !state.WatchLater {
		t.Fatal("watch later state not persisted")
	}
	if _, err := repo.SetVideoWatchLater(ctx, userID, running.ID, true); err != nil {
		t.Fatalf("set running watch later: %v", err)
	}
	if _, err := repo.SetVideoWatchLater(ctx, userID, failed.ID, true); err != nil {
		t.Fatalf("set failed watch later: %v", err)
	}
	if _, err := repo.SetVideoWatchLater(ctx, otherUserID, watched.ID, true); err != nil {
		t.Fatalf("set other user watch later: %v", err)
	}
	if state, err := repo.UpdateVideoWatchProgress(ctx, userID, watched.ID, 42.5, false, 1000); err != nil {
		t.Fatalf("update progress: %v", err)
	} else if state.WatchedAt == nil || state.LastPositionSeconds != 42.5 {
		t.Fatalf("watched state = %+v, want watched_at and 42.5s", state)
	}
	if state, err := repo.UpdateVideoWatchProgress(ctx, userID, watched.ID, 60, true, 2000); err != nil {
		t.Fatalf("complete progress: %v", err)
	} else if state.CompletedAt == nil {
		t.Fatalf("completed state = %+v, want completed_at", state)
	}
	if _, err := repo.UpdateVideoWatchProgress(ctx, userID, running.ID, 12, false, 3000); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("running progress err = %v, want ErrNotFound", err)
	}

	oldWatchedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	h.BackdateVideoUserStateWatched(t, userID, watched.ID, oldWatchedAt)
	if state, err := repo.UpdateVideoWatchProgress(ctx, userID, watched.ID, 90, false, 4000); err != nil {
		t.Fatalf("second progress: %v", err)
	} else if state.WatchedAt == nil || !state.WatchedAt.Equal(oldWatchedAt) {
		t.Fatalf("watched_at = %v, want preserved %v", state.WatchedAt, oldWatchedAt)
	} else if state.LastPositionSeconds != 90 {
		t.Fatalf("last_position_seconds = %v, want 90", state.LastPositionSeconds)
	}
	if state, err := repo.UpdateVideoWatchProgress(ctx, userID, watched.ID, 1, false, 3500); err != nil {
		t.Fatalf("stale progress: %v", err)
	} else if state.LastPositionSeconds != 90 {
		t.Fatalf("stale progress rewound position to %v, want 90", state.LastPositionSeconds)
	} else if state.LastProgressAtMs == nil || *state.LastProgressAtMs != 4000 {
		t.Fatalf("stale progress watermark = %v, want 4000", state.LastProgressAtMs)
	}

	states, err := repo.ListVideoUserStatesForVideos(ctx, userID, []int64{watched.ID, later.ID, plain.ID})
	if err != nil {
		t.Fatalf("list video user states: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("state count = %d, want 2; states=%+v", len(states), states)
	}

	baseOpts := repository.ListVideosOpts{UserID: userID, Sort: "created_at", Order: "desc", Limit: 10}
	watchLaterOpts := baseOpts
	watchLaterOpts.WatchLaterOnly = true
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, repo, watchLaterOpts), []string{"job-state-failed", "job-state-running", "job-state-later"})

	unwatchedOpts := baseOpts
	unwatchedOpts.UnwatchedOnly = true
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, repo, unwatchedOpts), []string{"job-state-plain", "job-state-later"})

	emptyUserUnwatchedOpts := unwatchedOpts
	emptyUserUnwatchedOpts.UserID = ""
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, repo, emptyUserUnwatchedOpts), []string{})

	totals, err := repo.VideoStatsTotals(ctx, userID)
	if err != nil {
		t.Fatalf("video stats totals: %v", err)
	}
	if totals.WatchLater != 3 || totals.Unwatched != 2 {
		t.Fatalf("stats watch_later/unwatched = %d/%d, want 3/2", totals.WatchLater, totals.Unwatched)
	}
	emptyTotals, err := repo.VideoStatsTotals(ctx, "")
	if err != nil {
		t.Fatalf("empty-user video stats totals: %v", err)
	}
	if emptyTotals.WatchLater != 0 || emptyTotals.Unwatched != 0 {
		t.Fatalf("empty-user stats watch_later/unwatched = %d/%d, want 0/0", emptyTotals.WatchLater, emptyTotals.Unwatched)
	}
	otherTotals, err := repo.VideoStatsTotals(ctx, otherUserID)
	if err != nil {
		t.Fatalf("other video stats totals: %v", err)
	}
	if otherTotals.WatchLater != 1 {
		t.Fatalf("other stats watch_later = %d, want 1", otherTotals.WatchLater)
	}
	otherWatchLaterOpts := baseOpts
	otherWatchLaterOpts.UserID = otherUserID
	otherWatchLaterOpts.WatchLaterOnly = true
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, repo, otherWatchLaterOpts), []string{"job-state-watched"})

	if state, err := repo.SetVideoWatchLater(ctx, userID, later.ID, false); err != nil {
		t.Fatalf("unset watch later: %v", err)
	} else if state.WatchLater {
		t.Fatal("watch later state stayed true after unset")
	}
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, repo, watchLaterOpts), []string{"job-state-failed", "job-state-running"})
}

func testDeleteOldRecordingWebhookDeliveriesPrunesTerminalKeepsActive(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-48 * time.Hour)
	cutoff := now.Add(-24 * time.Hour)

	mkPending := func(dk string, vid int64) *repository.RecordingWebhookDelivery {
		row, err := repo.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
			MessageID: dk, DedupeKey: dk, Event: "recording.completed", VideoID: vid, NextAttemptAt: now,
		})
		if err != nil {
			t.Fatalf("create %s: %v", dk, err)
		}
		return row
	}

	d1 := mkPending("recording.completed:1", 1)
	if _, err := repo.ClaimDueRecordingWebhookDeliveries(ctx, now, 1); err != nil {
		t.Fatalf("claim d1: %v", err)
	}
	if err := repo.MarkRecordingWebhookDeliveryDelivered(ctx, d1.ID, 200, now); err != nil {
		t.Fatalf("mark delivered: %v", err)
	}
	h.BackdateRecordingWebhookDelivery(t, d1.ID, &old, &old, &old)

	d2 := mkPending("recording.completed:2", 2)
	if err := repo.MarkRecordingWebhookDeliveryFinal(ctx, d2.ID, repository.RecordingWebhookDeliveryFailed, 500, "boom", now, now); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	// Only created_at is aged; updated_at stays recent so retention keeps it.
	h.BackdateRecordingWebhookDelivery(t, d2.ID, &old, nil, nil)

	pending := mkPending("recording.completed:3", 3)
	h.BackdateRecordingWebhookDelivery(t, pending.ID, &old, &old, nil)

	delivering, err := repo.CreateClaimedRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
		MessageID: "test:x", DedupeKey: "test:x", Event: "recording.test", Test: true, NextAttemptAt: now,
	})
	if err != nil {
		t.Fatalf("create claimed: %v", err)
	}
	h.BackdateRecordingWebhookDelivery(t, delivering.ID, &old, &old, nil)

	if err := repo.DeleteOldRecordingWebhookDeliveries(ctx, cutoff); err != nil {
		t.Fatalf("DeleteOldRecordingWebhookDeliveries: %v", err)
	}
	rows, err := repo.ListRecordingWebhookDeliveries(ctx, 50)
	if err != nil {
		t.Fatalf("ListRecordingWebhookDeliveries: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 surviving (recent terminal + pending + delivering), got %d: %+v", len(rows), rows)
	}
	seenRecentTerminal := false
	for _, r := range rows {
		if r.ID == d1.ID {
			t.Fatalf("old delivered row survived retention: %+v", r)
		}
		if r.ID == d2.ID {
			seenRecentTerminal = true
		}
		if r.Status != repository.RecordingWebhookDeliveryPending &&
			r.Status != repository.RecordingWebhookDeliveryDelivering &&
			r.ID != d2.ID {
			t.Fatalf("retention kept an unexpected terminal row or deleted an active one: %+v", r)
		}
	}
	if !seenRecentTerminal {
		t.Fatalf("recent terminal row was pruned even though updated_at is after cutoff")
	}
}
