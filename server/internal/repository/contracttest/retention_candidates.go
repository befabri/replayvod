package contracttest

import (
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testListRetentionCandidates(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "bc-1")
	now := time.Now().UTC().Truncate(time.Second)
	window := int64(48)
	overdue := now.Add(-49 * time.Hour)
	names := map[int64]string{}
	mk := func(jobID string, hours *int64) *repository.Video {
		t.Helper()
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: "bc-1",
			Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
			BroadcasterID: "bc-1", RecordingType: repository.RecordingTypeVideo,
			RetentionWindowHours: hours,
		})
		if err != nil {
			t.Fatalf("CreateVideo %s: %v", jobID, err)
		}
		names[v.ID] = jobID
		return v
	}
	finish := func(v *repository.Video, status, kind string, downloadedAt time.Time) {
		t.Helper()
		var err error
		if status == repository.VideoStatusDone {
			err = repo.MarkVideoDone(ctx, v.ID, 60, 1024, nil, kind, false)
		} else {
			err = repo.MarkVideoFailed(ctx, v.ID, "boom", kind, true)
		}
		if err != nil {
			t.Fatalf("finish %s: %v", v.JobID, err)
		}
		h.BackdateVideoDownloadedAt(t, v.ID, downloadedAt)
	}
	deliver := func(v *repository.Video, test bool) *repository.RecordingWebhookDelivery {
		t.Helper()
		d, err := repo.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
			MessageID: "msg-" + v.JobID, DedupeKey: "dedupe-" + v.JobID, Event: "recording.completed",
			VideoID: v.ID, Test: test, NextAttemptAt: now.Add(-time.Minute),
		})
		if err != nil {
			t.Fatalf("delivery for %s: %v", v.JobID, err)
		}
		return d
	}
	candidates := func(at time.Time) []string {
		t.Helper()
		rows, err := repo.ListRetentionCandidates(ctx, at, batchPage(t, 0, 100))
		if err != nil {
			t.Fatal(err)
		}
		return retentionNames(names, rows)
	}

	due := mk("due", &window)
	finish(due, repository.VideoStatusDone, repository.CompletionKindComplete, overdue)
	boundary := mk("boundary", &window)
	finish(boundary, repository.VideoStatusDone, repository.CompletionKindComplete, now.Add(-48*time.Hour))
	fresh := mk("fresh", &window)
	finish(fresh, repository.VideoStatusDone, repository.CompletionKindComplete, now.Add(-47*time.Hour))
	noWindow := mk("no-window", nil)
	finish(noWindow, repository.VideoStatusDone, repository.CompletionKindComplete, now.Add(-400*time.Hour))
	partial := mk("failed-partial", &window)
	finish(partial, repository.VideoStatusFailed, repository.CompletionKindPartial, overdue)
	cancelled := mk("failed-cancelled", &window)
	finish(cancelled, repository.VideoStatusFailed, repository.CompletionKindCancelled, overdue)
	failedComplete := mk("failed-complete", &window)
	finish(failedComplete, repository.VideoStatusFailed, repository.CompletionKindComplete, overdue)
	pending := mk("pending", &window)
	h.BackdateVideoDownloadedAt(t, pending.ID, overdue)
	// A finished row that never recorded when it finished has no deadline.
	unstamped, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "done-unstamped", Filename: "done-unstamped", DisplayName: "bc-1",
		Status: repository.VideoStatusDone, Quality: repository.QualityHigh,
		BroadcasterID: "bc-1", RecordingType: repository.RecordingTypeVideo,
		RetentionWindowHours: &window,
	})
	if err != nil {
		t.Fatal(err)
	}
	names[unstamped.ID] = "done-unstamped"
	gone := mk("gone", &window)
	finish(gone, repository.VideoStatusDone, repository.CompletionKindComplete, overdue)
	if err := repo.SoftDeleteVideo(ctx, gone.ID, repository.DeletionKindRetention); err != nil {
		t.Fatal(err)
	}
	queued := mk("queued", &window)
	finish(queued, repository.VideoStatusDone, repository.CompletionKindComplete, overdue)
	if _, err := repo.RequestVideoDelete(ctx, queued.ID); err != nil {
		t.Fatal(err)
	}
	guarded := mk("guarded", &window)
	finish(guarded, repository.VideoStatusDone, repository.CompletionKindComplete, overdue)
	guard := deliver(guarded, false)
	settled := mk("settled", &window)
	finish(settled, repository.VideoStatusDone, repository.CompletionKindComplete, overdue)
	settling := deliver(settled, false)
	tested := mk("test-delivery", &window)
	finish(tested, repository.VideoStatusDone, repository.CompletionKindComplete, overdue)
	deliver(tested, true)

	assertStringSlice(t, candidates(now), []string{"due", "failed-partial", "failed-cancelled", "test-delivery"})
	assertStringSlice(t, candidates(now.Add(time.Second)), []string{"due", "boundary", "failed-partial", "failed-cancelled", "test-delivery"})
	assertStringSlice(t, candidates(now.Add(-2*time.Hour)), nil)

	rows, err := repo.ListRetentionCandidates(ctx, now, batchPage(t, 0, 1))
	if err != nil || len(rows) != 1 {
		t.Fatalf("first candidate = %+v, %v", rows, err)
	}
	if r := rows[0]; r.VideoID != due.ID || r.BroadcasterID != "bc-1" || r.DownloadedAt == nil || !r.DownloadedAt.Equal(overdue) || r.RetentionWindowHours == nil || *r.RetentionWindowHours != window {
		t.Fatalf("candidate = %+v, want %d downloaded %v with a %dh window", r, due.ID, overdue, window)
	}

	if claimed, err := repo.ClaimDueRecordingWebhookDeliveries(ctx, now, 10); err != nil || len(claimed) != 3 {
		t.Fatalf("claimed deliveries = %+v, %v", claimed, err)
	}
	assertStringSlice(t, candidates(now), []string{"due", "failed-partial", "failed-cancelled", "test-delivery"})
	if err := repo.MarkRecordingWebhookDeliveryDelivered(ctx, settling.ID, 200, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetRecordingWebhookDeliveryFrozenParts(ctx, guard.ID, "[]"); err != nil {
		t.Fatal(err)
	}
	full := candidates(now)
	assertStringSlice(t, full, []string{"due", "failed-partial", "failed-cancelled", "guarded", "settled", "test-delivery"})

	var paged []repository.RetentionVideo
	var cursor int64
	for {
		page, err := repo.ListRetentionCandidates(ctx, now, batchPage(t, cursor, 2))
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		if len(page) > 2 || page[0].VideoID <= cursor || (len(page) == 2 && page[1].VideoID <= page[0].VideoID) {
			t.Fatalf("page after %d = %+v", cursor, page)
		}
		paged = append(paged, page...)
		cursor = page[len(page)-1].VideoID
	}
	assertStringSlice(t, retentionNames(names, paged), full)
}

func retentionNames(names map[int64]string, rows []repository.RetentionVideo) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = names[r.VideoID]
	}
	return out
}
