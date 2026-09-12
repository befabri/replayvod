package contracttest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testArchiveRetryLifecycle(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u1", "bc-1")

	v := seedArchive(t, ctx, repo, "job-1", "700", "bc-1")
	if job, err := repo.GetJob(ctx, "job-1"); err != nil || job.Attempt != 1 {
		t.Fatalf("first job attempt = %v, %v; want 1", job, err)
	}

	// A transient failure fails the row and schedules the retry in one step,
	// so the VOD stays held the whole time.
	retryAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	if err := repo.MarkArchiveFailedForRetry(ctx, v.ID, "edge 503", repository.CompletionKindComplete, false, retryAt); err != nil {
		t.Fatalf("MarkArchiveFailedForRetry: %v", err)
	}
	got, err := repo.GetVideo(ctx, v.ID)
	if err != nil || got.Status != repository.VideoStatusFailed || got.Error == nil || *got.Error != "edge 503" {
		t.Fatalf("failed row = %+v, %v", got, err)
	}
	if got.NextRetryAt == nil || !got.NextRetryAt.Equal(retryAt) {
		t.Fatalf("next_retry_at = %v, want %v", got.NextRetryAt, retryAt)
	}
	if open, err := repo.GetOpenVideoByTwitchVideoID(ctx, "700"); err != nil || open.ID != v.ID {
		t.Fatalf("failed archive with a retry must stay open: %v, %v", open, err)
	}
	if rows, err := repo.ListOpenVideosByTwitchVideoIDs(ctx, []string{"700"}); err != nil || len(rows) != 1 {
		t.Fatalf("open rows = %v, %v; want the retrying archive", rows, err)
	}
	id := "700"
	if _, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-dup", Filename: "job-dup", DisplayName: "bc-1",
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		BroadcasterID: "bc-1", RecordingType: repository.RecordingTypeVideo,
		Source: repository.VideoSourceVOD, TwitchVideoID: &id,
	}); !errors.Is(err, repository.ErrDuplicate) {
		t.Fatalf("second row for a VOD awaiting retry err = %v, want ErrDuplicate", err)
	}

	// A live recording is never touched by the archive failure path.
	live := seedLiveJob(t, ctx, repo, "job-live", "bc-1")
	if err := repo.MarkArchiveFailedForRetry(ctx, live.ID, "nope", repository.CompletionKindComplete, false, retryAt); err != nil {
		t.Fatalf("MarkArchiveFailedForRetry on live: %v", err)
	}
	if row, _ := repo.GetVideo(ctx, live.ID); row.Status != repository.VideoStatusPending || row.NextRetryAt != nil {
		t.Fatalf("live row changed by the archive failure path: %+v", row)
	}

	// Due and recent listings.
	due, err := repo.ListArchivesDueForRetry(ctx, time.Now().UTC(), 10)
	if err != nil || len(due) != 1 || due[0].ID != v.ID {
		t.Fatalf("due retries = %v, %v; want the failed archive", due, err)
	}
	if early, err := repo.ListArchivesDueForRetry(ctx, retryAt.Add(-time.Hour), 10); err != nil || len(early) != 0 {
		t.Fatalf("retries due before their time = %v, %v; want none", early, err)
	}
	if err := repo.MarkVideoFailed(ctx, live.ID, "live boom", repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("MarkVideoFailed live: %v", err)
	}
	recent, err := repo.ListRecentArchiveFailures(ctx, time.Now().UTC().Add(-time.Hour), 10)
	if err != nil || len(recent) != 1 || recent[0].ID != v.ID {
		t.Fatalf("recent failures = %v, %v; want only the archive", recent, err)
	}
	if stale, err := repo.ListRecentArchiveFailures(ctx, time.Now().UTC().Add(time.Hour), 10); err != nil || len(stale) != 0 {
		t.Fatalf("failures newer than the future = %v, %v; want none", stale, err)
	}

	// The scheduled retry becomes a fresh attempt under a new job.
	if err := repo.WithTx(ctx, func(tx repository.Repository) error {
		if _, err := tx.CreateJob(ctx, &repository.JobInput{ID: "job-2", VideoID: v.ID, BroadcasterID: "bc-1", Attempt: 2}); err != nil {
			return err
		}
		return tx.RequeueArchiveVideo(ctx, v.ID, "job-2", true)
	}); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	got, err = repo.GetVideo(ctx, v.ID)
	if err != nil || got.Status != repository.VideoStatusPending || got.JobID != "job-2" {
		t.Fatalf("requeued row = %+v, %v; want PENDING under job-2", got, err)
	}
	if got.Error != nil || got.NextRetryAt != nil || got.DownloadedAt != nil || got.CompletionKind != repository.CompletionKindComplete || got.Truncated {
		t.Fatalf("requeue left failure state behind: %+v", got)
	}
	if job, err := repo.GetJob(ctx, "job-2"); err != nil || job.Attempt != 2 {
		t.Fatalf("second job attempt = %v, %v; want 2", job, err)
	}
	if next, err := repo.GetNextQueuedArchiveJob(ctx); err != nil || next.ID != "job-2" {
		t.Fatalf("next queued = %v, %v; want job-2", next, err)
	}
	if err := repo.RequeueArchiveVideo(ctx, v.ID, "job-2", false); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("requeue of a pending row err = %v, want ErrNotFound", err)
	}

	// Cancelling the retry releases the VOD; the pump then refuses to revive it.
	if err := repo.MarkArchiveFailedForRetry(ctx, v.ID, "again", repository.CompletionKindComplete, false, retryAt); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClearArchiveRetry(ctx, v.ID); err != nil {
		t.Fatalf("ClearArchiveRetry: %v", err)
	}
	if err := repo.ClearArchiveRetry(ctx, v.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("second ClearArchiveRetry err = %v, want ErrNotFound", err)
	}
	if err := repo.RequeueArchiveVideo(ctx, v.ID, "job-3", true); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("scheduled-only requeue after cancel err = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetOpenVideoByTwitchVideoID(ctx, "700"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cancelled retry still holds the VOD: %v", err)
	}
	if due, _ := repo.ListArchivesDueForRetry(ctx, time.Now().UTC(), 10); len(due) != 0 {
		t.Fatalf("cancelled retry still listed as due: %v", due)
	}

	// An operator retry is refused while another open row holds the VOD, and
	// works once that row is gone.
	other := seedArchive(t, ctx, repo, "job-other", "700", "bc-1")
	if err := repo.RequeueArchiveVideo(ctx, v.ID, "job-3", false); !errors.Is(err, repository.ErrDuplicate) {
		t.Fatalf("manual requeue next to an open row err = %v, want ErrDuplicate", err)
	}
	if err := repo.DeleteQueuedArchiveVideo(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.WithTx(ctx, func(tx repository.Repository) error {
		if _, err := tx.CreateJob(ctx, &repository.JobInput{ID: "job-3", VideoID: v.ID, BroadcasterID: "bc-1", Attempt: 3}); err != nil {
			return err
		}
		return tx.RequeueArchiveVideo(ctx, v.ID, "job-3", false)
	}); err != nil {
		t.Fatalf("manual requeue: %v", err)
	}
	if got, _ := repo.GetVideo(ctx, v.ID); got.Status != repository.VideoStatusPending || got.JobID != "job-3" {
		t.Fatalf("manually requeued row = %+v", got)
	}

	// A removed row is neither due nor requeueable.
	if err := repo.MarkArchiveFailedForRetry(ctx, v.ID, "gone", repository.CompletionKindComplete, false, retryAt); err != nil {
		t.Fatal(err)
	}
	if err := repo.SoftDeleteVideo(ctx, v.ID, repository.DeletionKindManual); err != nil {
		t.Fatal(err)
	}
	if due, _ := repo.ListArchivesDueForRetry(ctx, time.Now().UTC(), 10); len(due) != 0 {
		t.Fatalf("removed archive listed as due: %v", due)
	}
	if recent, _ := repo.ListRecentArchiveFailures(ctx, time.Now().UTC().Add(-time.Hour), 10); len(recent) != 0 {
		t.Fatalf("removed archive listed as a recent failure: %v", recent)
	}
	if err := repo.RequeueArchiveVideo(ctx, v.ID, "job-4", false); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("requeue of a removed row err = %v, want ErrNotFound", err)
	}
}

func testArchiveStreamMatchAndMissingPoster(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u1", "bc-1")
	for _, id := range []string{"s-done", "s-failed", "s-removed"} {
		if _, err := repo.UpsertStream(ctx, &repository.StreamInput{ID: id, BroadcasterID: "bc-1", Type: "live", StartedAt: time.Now().UTC()}); err != nil {
			t.Fatalf("seed stream %s: %v", id, err)
		}
	}
	seedLive := func(jobID, streamID string) *repository.Video {
		sid := streamID
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: "bc-1", StreamID: &sid,
			Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
			BroadcasterID: "bc-1", RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("seed live %s: %v", jobID, err)
		}
		return v
	}
	done := seedLive("job-done", "s-done")
	if err := repo.MarkVideoDone(ctx, done.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatal(err)
	}
	failed := seedLive("job-failed", "s-failed")
	if err := repo.MarkVideoFailed(ctx, failed.ID, "boom", repository.CompletionKindComplete, true); err != nil {
		t.Fatal(err)
	}
	removed := seedLive("job-removed", "s-removed")
	if err := repo.MarkVideoDone(ctx, removed.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatal(err)
	}
	if err := repo.SoftDeleteVideo(ctx, removed.ID, repository.DeletionKindManual); err != nil {
		t.Fatal(err)
	}
	// An archive of the same stream is not a live recording.
	archive := seedArchive(t, ctx, repo, "job-arch", "900", "bc-1")
	_ = archive

	rows, err := repo.ListOpenVideosByStreamIDs(ctx, []string{"s-done", "s-failed", "s-removed", "s-unknown"})
	if err != nil || len(rows) != 1 || rows[0].ID != done.ID {
		t.Fatalf("open live recordings by stream = %v, %v; want only the finished one", rows, err)
	}
	if empty, err := repo.ListOpenVideosByStreamIDs(ctx, nil); err != nil || len(empty) != 0 {
		t.Fatalf("empty input = %v, %v", empty, err)
	}

	// Posters: only fresh, live-status archives without a thumbnail qualify.
	withPoster := seedArchive(t, ctx, repo, "job-poster", "901", "bc-1")
	if err := repo.SetVideoThumbnail(ctx, withPoster.ID, "thumbnails/x-snap00.jpg"); err != nil {
		t.Fatal(err)
	}
	failedArchive := seedArchive(t, ctx, repo, "job-failed-arch", "902", "bc-1")
	if err := repo.MarkVideoFailed(ctx, failedArchive.ID, "boom", repository.CompletionKindComplete, false); err != nil {
		t.Fatal(err)
	}
	old := seedArchive(t, ctx, repo, "job-old", "903", "bc-1")
	h.BackdateVideoStartDownload(t, old.ID, time.Now().UTC().Add(-48*time.Hour))
	removedArchive := seedArchive(t, ctx, repo, "job-removed-arch", "904", "bc-1")
	if err := repo.SoftDeleteVideo(ctx, removedArchive.ID, repository.DeletionKindManual); err != nil {
		t.Fatal(err)
	}
	fresh := seedArchive(t, ctx, repo, "job-fresh", "905", "bc-1")

	missing, err := repo.ListArchivesMissingPoster(ctx, time.Now().UTC().Add(-24*time.Hour), 0, 10)
	if err != nil {
		t.Fatalf("ListArchivesMissingPoster: %v", err)
	}
	wantIDs := map[int64]bool{archive.ID: true, fresh.ID: true}
	if len(missing) != len(wantIDs) {
		t.Fatalf("missing posters = %d rows, want %d: %+v", len(missing), len(wantIDs), missing)
	}
	for _, row := range missing {
		if !wantIDs[row.ID] {
			t.Fatalf("unexpected row %d in missing posters", row.ID)
		}
	}
	if limited, err := repo.ListArchivesMissingPoster(ctx, time.Now().UTC().Add(-24*time.Hour), 0, 1); err != nil || len(limited) != 1 {
		t.Fatalf("limit ignored: %v, %v", limited, err)
	}

	// Keyset paging visits every eligible row, so a placeholder that persists
	// on an early archive never shadows the ones queued after it.
	var paged []int64
	var after int64
	for {
		page, err := repo.ListArchivesMissingPoster(ctx, time.Now().UTC().Add(-24*time.Hour), after, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		if page[0].ID <= after {
			t.Fatalf("page did not advance: %d after %d", page[0].ID, after)
		}
		paged = append(paged, page[0].ID)
		after = page[0].ID
	}
	if len(paged) != 2 || paged[0] != archive.ID || paged[1] != fresh.ID {
		t.Fatalf("paged ids = %v, want [%d %d]", paged, archive.ID, fresh.ID)
	}
	for _, limit := range []int{0, -1, 1001} {
		if _, err := repo.ListArchivesMissingPoster(ctx, time.Now().UTC(), 0, limit); err == nil {
			t.Fatalf("accepted limit %d", limit)
		}
	}

	// A failed archive that salvaged a part still shows in the library and
	// qualifies; the tombstone above never does, and neither does a removed row
	// on the final write.
	salvaged := seedArchive(t, ctx, repo, "job-salvaged", "906", "bc-1")
	if _, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{
		VideoID: salvaged.ID, PartIndex: 1, Filename: "job-salvaged-part01.mp4",
		Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkVideoFailed(ctx, salvaged.ID, "boom", repository.CompletionKindPartial, false); err != nil {
		t.Fatal(err)
	}
	missing, err = repo.ListArchivesMissingPoster(ctx, time.Now().UTC().Add(-24*time.Hour), 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 3 || missing[2].ID != salvaged.ID {
		t.Fatalf("missing posters with a salvaged failure = %+v, want the salvaged archive listed last", missing)
	}
	if set, err := repo.SetVideoThumbnailIfMissing(ctx, removedArchive.ID, "thumbnails/late.jpg"); err != nil || set {
		t.Fatalf("poster write on a removed archive = %v, %v; want refused", set, err)
	}
	if got, err := repo.GetVideo(ctx, removedArchive.ID); err != nil || got.Thumbnail != nil {
		t.Fatalf("removed archive after refused write = %+v, %v", got, err)
	}
}

func testArchiveSourceFilterAndBroadcastSort(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u1", "bc-1")

	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	live := seedLiveJob(t, ctx, repo, "job-live", "bc-1")
	h.BackdateVideoStartDownload(t, live.ID, base.Add(3*time.Minute))
	seedAired := func(jobID, vodID string, started, aired time.Time) {
		id := vodID
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: "bc-1", Title: jobID,
			Status: repository.VideoStatusDone, Quality: repository.QualityHigh,
			BroadcasterID: "bc-1", RecordingType: repository.RecordingTypeVideo,
			Source: repository.VideoSourceVOD, TwitchVideoID: &id, BroadcastAt: &aired,
		})
		if err != nil {
			t.Fatalf("seed %s: %v", jobID, err)
		}
		h.BackdateVideoStartDownload(t, v.ID, started)
	}
	// Archived today, aired ten days ago; archived earlier, aired five days out.
	seedAired("job-old-vod", "1", base.Add(2*time.Minute), base.AddDate(0, 0, -10))
	seedAired("job-new-vod", "2", base.Add(1*time.Minute), base.AddDate(0, 0, 5))

	cases := []struct {
		name string
		opts repository.ListVideosOpts
		want []string
	}{
		{"download date ignores air date", repository.ListVideosOpts{Sort: "created_at", Order: "desc", Limit: 1}, []string{"job-live", "job-old-vod", "job-new-vod"}},
		{"aired desc", repository.ListVideosOpts{Sort: "broadcast_at", Order: "desc", Limit: 1}, []string{"job-new-vod", "job-live", "job-old-vod"}},
		{"aired asc", repository.ListVideosOpts{Sort: "broadcast_at", Order: "asc", Limit: 1}, []string{"job-old-vod", "job-live", "job-new-vod"}},
		{"archives only", repository.ListVideosOpts{Sort: "created_at", Order: "desc", Limit: 1, Source: repository.VideoSourceVOD}, []string{"job-old-vod", "job-new-vod"}},
		{"live only", repository.ListVideosOpts{Sort: "created_at", Order: "desc", Limit: 5, Source: repository.VideoSourceLive}, []string{"job-live"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, repo, tc.opts), tc.want)
		})
	}

	// The retry column rides along in the hand-written page projection.
	retryAt := base.Add(time.Hour)
	if err := repo.MarkArchiveFailedForRetry(ctx, live.ID, "x", repository.CompletionKindComplete, false, retryAt); err != nil {
		t.Fatal(err)
	}
	vod, err := repo.GetVideoByJobID(ctx, "job-old-vod")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkArchiveFailedForRetry(ctx, vod.ID, "x", repository.CompletionKindComplete, false, retryAt); err != nil {
		t.Fatal(err)
	}
	page, err := repo.ListVideosPage(ctx, repository.ListVideosOpts{Limit: 10, Source: repository.VideoSourceVOD}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range page.Items {
		if row.ID == vod.ID {
			found = true
			if row.NextRetryAt == nil || !row.NextRetryAt.Equal(retryAt) {
				t.Fatalf("page row lost next_retry_at: %+v", row)
			}
		}
	}
	if !found {
		t.Fatal("retrying archive missing from page")
	}
}

// testArchiveRetryYieldsToQueuedDelete pins that an operator's delete wins
// over a scheduled retry: the request clears the retry, the pump neither
// lists nor requeues the row, the delete worker sees it, and the VOD is free
// to be archived anew.
func testArchiveRetryYieldsToQueuedDelete(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u1", "bc-1")
	v := seedArchive(t, ctx, repo, "job-retry-del", "930", "bc-1")
	if err := repo.MarkArchiveFailedForRetry(ctx, v.ID, "boom", repository.CompletionKindComplete, false, time.Now().UTC().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RequestVideoDelete(ctx, v.ID); err != nil {
		t.Fatalf("RequestVideoDelete on a retrying archive: %v", err)
	}
	got, err := repo.GetVideo(ctx, v.ID)
	if err != nil || got.NextRetryAt != nil || got.DeleteRequestedAt == nil {
		t.Fatalf("after the delete request = retry %v, requested %v, %v; want the retry cleared", got.NextRetryAt, got.DeleteRequestedAt, err)
	}
	if due, err := repo.ListArchivesDueForRetry(ctx, time.Now().UTC(), 10); err != nil || len(due) != 0 {
		t.Fatalf("due retries with a delete queued = %v, %v; want none", due, err)
	}
	err = repo.WithTx(ctx, func(tx repository.Repository) error {
		if _, err := tx.CreateJob(ctx, &repository.JobInput{ID: "job-retry-del-2", VideoID: v.ID, BroadcasterID: "bc-1", Attempt: 2}); err != nil {
			return err
		}
		return tx.RequeueArchiveVideo(ctx, v.ID, "job-retry-del-2", false)
	})
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("requeue with a delete queued err = %v, want ErrNotFound", err)
	}
	pending, err := repo.ListVideosPendingManualDelete(ctx, 10)
	if err != nil || len(pending) != 1 || pending[0].ID != v.ID {
		t.Fatalf("pending manual deletes = %+v, %v; want the archive", pending, err)
	}
	// The failed row no longer holds the VOD open, so it can be archived anew.
	if again := seedArchive(t, ctx, repo, "job-retry-del-again", "930", "bc-1"); again.ID == v.ID {
		t.Fatal("re-archive reused the row")
	}
}

func testRunningJobsOnlyResumeCurrentActiveAttempt(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u1", "bc-1")
	archive := seedArchive(t, ctx, repo, "old", "resume", "bc-1")
	live := seedLiveJob(t, ctx, repo, "live", "bc-1")
	for _, id := range []string{"old", "live"} {
		if err := repo.MarkJobRunning(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.MarkArchiveFailedForRetry(ctx, archive.ID, "retry", repository.CompletionKindComplete, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateJob(ctx, &repository.JobInput{ID: "current", VideoID: archive.ID, BroadcasterID: "bc-1", Attempt: 2}); err != nil {
		t.Fatal(err)
	}
	if err := repo.RequeueArchiveVideo(ctx, archive.ID, "current", true); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkJobRunning(ctx, "current"); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.ListRunningJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, j := range rows {
		ids[j.ID] = true
	}
	if len(rows) != 2 || !ids["current"] || !ids["live"] || ids["old"] {
		t.Fatalf("obsolete attempt resumed: %+v", rows)
	}
	broadcasters, err := repo.ListRunningLiveBroadcasters(ctx)
	if err != nil || len(broadcasters) != 1 || broadcasters[0] != "bc-1" {
		t.Fatalf("live set: %+v %v", broadcasters, err)
	}
	if err := repo.UpdateVideoStatus(ctx, live.ID, repository.VideoStatusDone); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateVideoStatus(ctx, archive.ID, repository.VideoStatusDone); err != nil {
		t.Fatal(err)
	}
	if rows, err = repo.ListRunningJobs(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("terminal recording resumed: %+v %v", rows, err)
	}
	if broadcasters, err = repo.ListRunningLiveBroadcasters(ctx); err != nil || len(broadcasters) != 0 {
		t.Fatalf("terminal live recording subscribed: %+v %v", broadcasters, err)
	}
	// Start persists a pending live attempt before launching its worker. A
	// crash in that gap still needs recovery; pending archives belong to the
	// archive queue and must not bypass its admission controls.
	pending := seedLiveJob(t, ctx, repo, "pending-live", "bc-1")
	seedArchive(t, ctx, repo, "pending-archive", "not-started", "bc-1")
	rows, err = repo.ListRunningJobs(ctx)
	if err != nil || len(rows) != 1 || rows[0].VideoID != pending.ID {
		t.Fatalf("pending live recovery = %+v, %v", rows, err)
	}
}
