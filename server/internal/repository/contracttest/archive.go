package contracttest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func seedArchive(t *testing.T, ctx context.Context, repo repository.Repository, jobID, vodID, broadcasterID string) *repository.Video {
	t.Helper()
	id := vodID
	aired := time.Date(2026, 8, 30, 20, 0, 0, 0, time.UTC)
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: jobID, Filename: jobID, DisplayName: broadcasterID, Title: "vod " + vodID,
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		BroadcasterID: broadcasterID, RecordingType: repository.RecordingTypeVideo,
		Source: repository.VideoSourceVOD, TwitchVideoID: &id, BroadcastAt: &aired,
	})
	if err != nil {
		t.Fatalf("CreateVideo %s: %v", jobID, err)
	}
	if _, err := repo.CreateJob(ctx, &repository.JobInput{ID: jobID, VideoID: v.ID, BroadcasterID: broadcasterID}); err != nil {
		t.Fatalf("CreateJob %s: %v", jobID, err)
	}
	return v
}

func seedLiveJob(t *testing.T, ctx context.Context, repo repository.Repository, jobID, broadcasterID string) *repository.Video {
	t.Helper()
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: jobID, Filename: jobID, DisplayName: broadcasterID,
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		BroadcasterID: broadcasterID, RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("CreateVideo %s: %v", jobID, err)
	}
	if _, err := repo.CreateJob(ctx, &repository.JobInput{ID: jobID, VideoID: v.ID, BroadcasterID: broadcasterID}); err != nil {
		t.Fatalf("CreateJob %s: %v", jobID, err)
	}
	return v
}

func testArchiveVideoRoundTrip(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u1", "bc-1")

	live := seedLiveJob(t, ctx, repo, "job-live", "bc-1")
	if live.Source != repository.VideoSourceLive || live.TwitchVideoID != nil || live.BroadcastAt != nil {
		t.Fatalf("live row = source %q vod %v aired %v, want live/nil/nil", live.Source, live.TwitchVideoID, live.BroadcastAt)
	}

	vod := seedArchive(t, ctx, repo, "job-vod", "123", "bc-1")
	if vod.Source != repository.VideoSourceVOD || vod.TwitchVideoID == nil || *vod.TwitchVideoID != "123" {
		t.Fatalf("archive row = source %q vod %v", vod.Source, vod.TwitchVideoID)
	}
	if vod.BroadcastAt == nil || !vod.BroadcastAt.Equal(time.Date(2026, 8, 30, 20, 0, 0, 0, time.UTC)) {
		t.Fatalf("broadcast_at = %v, want the seeded date", vod.BroadcastAt)
	}

	// The keyset page query selects columns by hand; the archive columns must
	// survive it too.
	page, err := repo.ListVideosPage(ctx, repository.ListVideosOpts{Limit: 10}, nil)
	if err != nil {
		t.Fatalf("ListVideosPage: %v", err)
	}
	var found bool
	for _, row := range page.Items {
		if row.ID == vod.ID {
			found = true
			if row.TwitchVideoID == nil || *row.TwitchVideoID != "123" || row.BroadcastAt == nil {
				t.Fatalf("page row lost archive columns: %+v", row)
			}
		}
	}
	if !found {
		t.Fatalf("archive row missing from page")
	}
}

func testArchiveOpenRowPerVOD(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u1", "bc-1")

	first := seedArchive(t, ctx, repo, "job-1", "500", "bc-1")
	got, err := repo.GetOpenVideoByTwitchVideoID(ctx, "500")
	if err != nil || got.ID != first.ID {
		t.Fatalf("GetOpenVideoByTwitchVideoID = %v, %v; want row %d", got, err, first.ID)
	}
	id := "500"
	if _, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-dup", Filename: "job-dup", DisplayName: "bc-1",
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		BroadcasterID: "bc-1", RecordingType: repository.RecordingTypeVideo,
		Source: repository.VideoSourceVOD, TwitchVideoID: &id,
	}); err == nil {
		t.Fatalf("second open row for the same VOD was accepted")
	}

	// A failed archive no longer counts, so the VOD can be queued again.
	if err := repo.MarkVideoFailed(ctx, first.ID, "boom", repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("MarkVideoFailed: %v", err)
	}
	if _, err := repo.GetOpenVideoByTwitchVideoID(ctx, "500"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("after failure err = %v, want ErrNotFound", err)
	}
	second := seedArchive(t, ctx, repo, "job-2", "500", "bc-1")

	// Nor does a removed one.
	if err := repo.SoftDeleteVideo(ctx, second.ID, repository.DeletionKindManual); err != nil {
		t.Fatalf("SoftDeleteVideo: %v", err)
	}
	if _, err := repo.GetOpenVideoByTwitchVideoID(ctx, "500"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("after removal err = %v, want ErrNotFound", err)
	}

	third := seedArchive(t, ctx, repo, "job-3", "500", "bc-1")
	seedArchive(t, ctx, repo, "job-4", "501", "bc-1")
	open, err := repo.ListOpenVideosByTwitchVideoIDs(ctx, []string{"500", "501", "502"})
	if err != nil {
		t.Fatalf("ListOpenVideosByTwitchVideoIDs: %v", err)
	}
	if len(open) != 2 {
		t.Fatalf("open rows = %d, want 2", len(open))
	}
	for _, row := range open {
		if *row.TwitchVideoID == "500" && row.ID != third.ID {
			t.Fatalf("open row for 500 = %d, want the third row %d", row.ID, third.ID)
		}
	}
	if empty, err := repo.ListOpenVideosByTwitchVideoIDs(ctx, nil); err != nil || len(empty) != 0 {
		t.Fatalf("empty input = %v, %v; want no rows", empty, err)
	}
}

func testArchiveQueueOrderAndDequeue(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u1", "bc-1")
	SeedUserChannel(t, ctx, repo, "u2", "bc-2")

	live := seedLiveJob(t, ctx, repo, "job-live", "bc-1")
	first := seedArchive(t, ctx, repo, "job-a", "1", "bc-1")
	second := seedArchive(t, ctx, repo, "job-b", "2", "bc-2")
	third := seedArchive(t, ctx, repo, "job-c", "3", "bc-1")
	h.BackdateVideoStartDownload(t, first.ID, time.Now().Add(-3*time.Hour))
	h.BackdateVideoStartDownload(t, second.ID, time.Now().Add(-2*time.Hour))
	h.BackdateVideoStartDownload(t, third.ID, time.Now().Add(-1*time.Hour))
	if err := repo.MarkJobRunning(ctx, "job-a"); err != nil {
		t.Fatalf("MarkJobRunning: %v", err)
	}
	if err := repo.UpdateVideoStatus(ctx, first.ID, repository.VideoStatusRunning); err != nil {
		t.Fatalf("UpdateVideoStatus: %v", err)
	}

	queue, err := repo.ListArchiveQueue(ctx)
	if err != nil {
		t.Fatalf("ListArchiveQueue: %v", err)
	}
	wantIDs := []int64{first.ID, second.ID, third.ID}
	if len(queue) != len(wantIDs) {
		t.Fatalf("queue len = %d, want %d (live rows must not appear)", len(queue), len(wantIDs))
	}
	for i, id := range wantIDs {
		if queue[i].ID != id {
			t.Fatalf("queue[%d] = %d, want %d (oldest first)", i, queue[i].ID, id)
		}
	}

	// The next job to start is the oldest still-pending archive, never the
	// running one and never a live job.
	next, err := repo.GetNextQueuedArchiveJob(ctx)
	if err != nil || next.ID != "job-b" {
		t.Fatalf("GetNextQueuedArchiveJob = %v, %v; want job-b", next, err)
	}

	// Video insertion and job insertion need not have the same order. The
	// visible queue and picker must nevertheless agree on the next archive.
	h.BackdateVideoStartDownload(t, third.ID, time.Now().Add(-4*time.Hour))
	queue, err = repo.ListArchiveQueue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	next, err = repo.GetNextQueuedArchiveJob(ctx)
	if err != nil || next.VideoID != queue[0].ID || next.VideoID != third.ID {
		t.Fatalf("queue and picker disagree after reordering: next=%+v, queue=%+v, err=%v", next, queue, err)
	}

	// The live idempotency check only sees live jobs: bc-2 has a queued archive
	// and nothing live, bc-1 has both.
	if _, err := repo.GetActiveLiveJobByBroadcaster(ctx, "bc-2"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("bc-2 active live job err = %v, want ErrNotFound", err)
	}
	if job, err := repo.GetActiveLiveJobByBroadcaster(ctx, "bc-1"); err != nil || job.VideoID != live.ID {
		t.Fatalf("bc-1 active live job = %v, %v; want video %d", job, err, live.ID)
	}

	// Dequeue removes a pending archive and its job outright; a running one and
	// a live pending row are refused.
	if err := repo.DeleteQueuedArchiveVideo(ctx, second.ID); err != nil {
		t.Fatalf("DeleteQueuedArchiveVideo pending: %v", err)
	}
	if _, err := repo.GetVideo(ctx, second.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("dequeued video still readable: %v", err)
	}
	if _, err := repo.GetJob(ctx, "job-b"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("dequeued job still readable: %v", err)
	}
	if err := repo.DeleteQueuedArchiveVideo(ctx, first.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("dequeue running archive err = %v, want ErrNotFound", err)
	}
	if err := repo.DeleteQueuedArchiveVideo(ctx, live.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("dequeue live row err = %v, want ErrNotFound", err)
	}

	next, err = repo.GetNextQueuedArchiveJob(ctx)
	if err != nil || next.ID != "job-c" {
		t.Fatalf("after dequeue next = %v, %v; want job-c", next, err)
	}
	if err := repo.DeleteQueuedArchiveVideo(ctx, third.ID); err != nil {
		t.Fatalf("DeleteQueuedArchiveVideo third: %v", err)
	}
	if _, err := repo.GetNextQueuedArchiveJob(ctx); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("empty queue err = %v, want ErrNotFound", err)
	}
}

func testMarkVideoDoneKeepsPosterWithoutFrame(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u1", "bc-1")

	// An audio recording gets its poster from the snapshotter (live) or the
	// archive fetch (VOD) before the pipeline finishes; finishing without a
	// frame must not erase it.
	audio := seedArchive(t, ctx, repo, "job-audio", "9001", "bc-1")
	if err := repo.SetVideoThumbnail(ctx, audio.ID, "thumbnails/job-audio-snap00.jpg"); err != nil {
		t.Fatalf("SetVideoThumbnail: %v", err)
	}
	if err := repo.MarkVideoDone(ctx, audio.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("MarkVideoDone: %v", err)
	}
	got, err := repo.GetVideo(ctx, audio.ID)
	if err != nil || got.Thumbnail == nil || *got.Thumbnail != "thumbnails/job-audio-snap00.jpg" {
		t.Fatalf("thumbnail after frameless done = %v, %v; want the stored poster", got.Thumbnail, err)
	}

	// A frame from the pipeline still replaces the early poster.
	video := seedArchive(t, ctx, repo, "job-video", "9002", "bc-1")
	if err := repo.SetVideoThumbnail(ctx, video.ID, "thumbnails/job-video-snap00.jpg"); err != nil {
		t.Fatalf("SetVideoThumbnail: %v", err)
	}
	frame := "thumbnails/job-video-part01.jpg"
	if err := repo.MarkVideoDone(ctx, video.ID, 60, 1024, &frame, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("MarkVideoDone with frame: %v", err)
	}
	got, err = repo.GetVideo(ctx, video.ID)
	if err != nil || got.Thumbnail == nil || *got.Thumbnail != frame {
		t.Fatalf("thumbnail after done with frame = %v, %v; want %q", got.Thumbnail, err, frame)
	}

	// The webhook-enqueueing variant shares the rule.
	both := seedArchive(t, ctx, repo, "job-both", "9003", "bc-1")
	if err := repo.SetVideoThumbnail(ctx, both.ID, "thumbnails/job-both-snap00.jpg"); err != nil {
		t.Fatalf("SetVideoThumbnail: %v", err)
	}
	if err := repo.MarkVideoDoneAndEnqueueRecordingWebhook(ctx, both.ID, 60, 1024, nil, repository.CompletionKindComplete, false, nil); err != nil {
		t.Fatalf("MarkVideoDoneAndEnqueueRecordingWebhook: %v", err)
	}
	got, err = repo.GetVideo(ctx, both.ID)
	if err != nil || got.Thumbnail == nil || *got.Thumbnail != "thumbnails/job-both-snap00.jpg" {
		t.Fatalf("thumbnail after webhook done = %v, %v; want the stored poster", got.Thumbnail, err)
	}
}

// testVideoStreamLinkRequiresKnownStream pins the constraint the archive
// enqueue path has to respect: videos.stream_id references streams, so a
// row may only be linked to a broadcast the library already holds.
func testVideoStreamLinkRequiresKnownStream(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u1", "bc-1")
	unknown, vodID := "s-never-seen", "600"
	if _, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-unknown", Filename: "job-unknown", DisplayName: "bc-1",
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		BroadcasterID: "bc-1", RecordingType: repository.RecordingTypeVideo,
		Source: repository.VideoSourceVOD, TwitchVideoID: &vodID, StreamID: &unknown,
	}); err == nil {
		t.Fatal("a video linked to an unknown stream was accepted")
	}
	if _, err := repo.UpsertStream(ctx, &repository.StreamInput{ID: "s-known", BroadcasterID: "bc-1", Type: "live", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	known := "s-known"
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-known", Filename: "job-known", DisplayName: "bc-1",
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		BroadcasterID: "bc-1", RecordingType: repository.RecordingTypeVideo,
		Source: repository.VideoSourceVOD, TwitchVideoID: &vodID, StreamID: &known,
	})
	if err != nil {
		t.Fatalf("CreateVideo with a known stream: %v", err)
	}
	got, err := repo.GetVideo(ctx, v.ID)
	if err != nil || got.StreamID == nil || *got.StreamID != known {
		t.Fatalf("linked archive = %+v, %v; want stream %q", got, err, known)
	}
}
