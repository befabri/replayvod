//go:build ffmpeg

package downloader

import (
	"context"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

func TestRealArchive_CompletionFailurePreservesRecovery(t *testing.T) {
	requireFFmpegHarness(t)
	edge := newTwitchEdge(t, twitchEdgeOpts{tsCount: 1, fmp4Count: 2, windowA: 1, baseSeqA: 100, baseSeqB: 50})
	h := newHarnessService(t, edge.URL())
	t.Cleanup(h.svc.Shutdown)
	h.svc.repo = &terminalFaultRepo{Repository: h.repo, fail: "job"}
	seedArchiveChannel(t, h.repo, "archivist")
	ctx := t.Context()
	if _, err := h.repo.UpsertRecordingWebhookConfig(ctx, true, "https://hooks.example/test", "recording.completed"); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.EnsureRecordingWebhookSecret(ctx, "secret"); err != nil {
		t.Fatal(err)
	}
	bus := eventbus.New()
	h.svc.SetEventBus(bus)
	terminals := bus.RecordingTerminal.Subscribe(ctx)
	release := edge.BlockGQL()
	defer release()
	jobID, err := enqueueAndPump(h.svc, ctx, Params{
		BroadcasterID: "archivist", BroadcasterLogin: "archivist", DisplayName: "ARCHIVIST",
		Title: "completion persistence failure", Quality: repository.QualityHigh, RecordingType: repository.RecordingTypeVideo, VODID: "7171",
	})
	if err != nil {
		t.Fatal(err)
	}
	progress := h.svc.Subscribe(jobID)
	if progress == nil {
		t.Fatal("recording was not active behind the GQL barrier")
	}
	release()
	timeout := time.NewTimer(time.Minute)
	defer timeout.Stop()
waitForExit:
	for {
		select {
		case _, ok := <-progress:
			if !ok {
				break waitForExit
			}
		case <-timeout.C:
			t.Fatal("recording did not finish its attempt")
		}
	}
	h.svc.Shutdown()
	v, err := h.repo.GetVideoByJobID(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	job, err := h.repo.GetJob(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != repository.VideoStatusRunning || job.Status != repository.JobStatusRunning {
		t.Fatalf("partial completion escaped transaction: video=%s job=%s", v.Status, job.Status)
	}
	parts, err := h.repo.ListVideoParts(ctx, v.ID)
	if err != nil || len(parts) != 1 || parts[0].SizeBytes <= 0 {
		t.Fatalf("media did not finalize before the injected DB error: %+v, %v", parts, err)
	}
	if rows, err := h.repo.ListRecordingWebhookDeliveries(ctx, 10); err != nil || len(rows) != 0 {
		t.Fatalf("uncommitted completion webhook: %+v, %v", rows, err)
	}
	select {
	case ev := <-terminals:
		t.Fatalf("uncommitted terminal event: %+v", ev)
	default:
	}
	scratch := filepath.Join(h.scratchDir, jobID)
	if files, err := os.ReadDir(scratch); err != nil || len(files) == 0 {
		t.Fatalf("recovery scratch lost: %v, %v", files, err)
	}
	resumed := resumeOver(t, h, edge.URL())
	t.Cleanup(resumed.svc.Shutdown)
	resumed.svc.SetEventBus(bus)
	if err := resumed.svc.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	waitForVideoStatus(t, h.repo, v.ID, repository.VideoStatusDone, time.Minute)
	resumed.svc.Shutdown()
	job, err = h.repo.GetJob(ctx, jobID)
	if err != nil || job.Status != repository.JobStatusDone {
		t.Fatalf("completion recovery: job=%+v, %v", job, err)
	}
	if got, err := h.repo.ListVideoParts(ctx, v.ID); err != nil || len(got) != 1 || got[0].ID != parts[0].ID {
		t.Fatalf("completion recovery duplicated parts: %+v, %v", got, err)
	}
	if rows, err := h.repo.ListRecordingWebhookDeliveries(ctx, 10); err != nil || len(rows) != 1 {
		t.Fatalf("recovered completion webhook: %+v, %v", rows, err)
	}
	select {
	case ev := <-terminals:
		if ev.Kind != eventbus.RecordingCompleted {
			t.Errorf("unexpected terminal: %+v", ev)
		}
	default:
		t.Error("completion recovery did not notify")
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Errorf("committed completion did not clear scratch: %v", err)
	}
}

// A retry starts from persisted part boundaries after the old attempt's scratch
// has been discarded. Exercise that handoff across a fresh Service, with real
// fragments and ffmpeg, and verify both media coverage and preserved bytes.
func TestRealArchive_RetryPreservesFinalizedParts(t *testing.T) {
	requireFFmpegHarness(t)
	opts := twitchEdgeOpts{tsCount: 1, fmp4Count: 6, windowA: 1, baseSeqA: 100, baseSeqB: 50}
	edge := newTwitchEdge(t, opts)
	edge.segBFailureFrom.Store(3) // first two segments work; the remaining tail fails
	h := newHarnessService(t, edge.URL())
	t.Cleanup(h.svc.Shutdown)
	h.svc.cfg.App.Download.MaxPartSeconds = 2
	h.svc.cfg.App.Download.SegmentConcurrency = 1
	h.svc.cfg.App.Download.MaxGapRatio = 0.01
	seedArchiveChannel(t, h.repo, "archivist")
	ctx := t.Context()
	firstJob, err := enqueueAndPump(h.svc, ctx, Params{
		BroadcasterID: "archivist", BroadcasterLogin: "archivist", DisplayName: "ARCHIVIST",
		Title: "retry a partially saved vod", Quality: repository.QualityHigh, RecordingType: repository.RecordingTypeVideo, VODID: "6161",
	})
	if err != nil {
		t.Fatal(err)
	}
	v, err := h.repo.GetVideoByJobID(ctx, firstJob)
	if err != nil {
		t.Fatal(err)
	}
	failed := waitForVideoStatus(t, h.repo, v.ID, repository.VideoStatusFailed, 60*time.Second)
	h.svc.Shutdown()
	if failed.NextRetryAt == nil {
		t.Fatal("tail failure did not schedule a retry")
	}
	parts, err := h.repo.ListVideoParts(ctx, v.ID)
	if err != nil || len(parts) == 0 || parts[0].SizeBytes <= 0 {
		t.Fatalf("first attempt never finalized a part: %+v, %v", parts, err)
	}
	preserved := parts[0]
	assertPartRange(t, preserved, 50, 51)
	readPart := func(name string) []byte {
		t.Helper()
		f, err := h.storage.Open(ctx, storagekeys.Video(name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		b, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	wantHash := sha256.Sum256(readPart(preserved.Filename))
	edge.segBFailureFrom.Store(0)
	edge.mu.Lock()
	edge.segBRequests = nil
	edge.mu.Unlock()
	resumed := resumeOver(t, h, edge.URL())
	t.Cleanup(resumed.svc.Shutdown)
	resumed.svc.cfg.App.Download.MaxPartSeconds = 2
	resumed.svc.cfg.App.Download.SegmentConcurrency = 1
	resumed.svc.cfg.App.Download.MaxGapRatio = 0.01
	if err := h.repo.MarkArchiveFailedForRetry(ctx, v.ID, *failed.Error, failed.CompletionKind, failed.Truncated, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	resumed.svc.PumpArchiveQueue(ctx)
	done := waitForVideoStatus(t, h.repo, v.ID, repository.VideoStatusDone, 60*time.Second)
	resumed.svc.Shutdown()
	parts, err = h.repo.ListVideoParts(ctx, v.ID)
	if err != nil || len(parts) != 3 {
		t.Fatalf("completed parts=%+v, %v; want three 2-second parts", parts, err)
	}
	assertContiguousCoverage(t, parts, 50, 55)
	if parts[0].ID != preserved.ID || parts[0].Filename != preserved.Filename || sha256.Sum256(readPart(parts[0].Filename)) != wantHash {
		t.Error("retry replaced an already finalized part")
	}
	var duration float64
	var size int64
	for _, part := range parts {
		if part.Quality != preserved.Quality || part.Codec != preserved.Codec || part.SegmentFormat != preserved.SegmentFormat {
			t.Errorf("retry changed rendition: %+v", part)
		}
		if part.SizeBytes <= 0 {
			t.Errorf("unfinished part survived completion: %+v", part)
		}
		duration += part.DurationSeconds
		size += part.SizeBytes
	}
	if done.DurationSeconds == nil || abs(*done.DurationSeconds-duration) > 0.001 || abs(duration-6) > 1 || done.SizeBytes == nil || *done.SizeBytes != size {
		t.Errorf("incorrect aggregate: duration=%v size=%v; parts sum=%f/%d", done.DurationSeconds, done.SizeBytes, duration, size)
	}
	if done.Truncated || done.CompletionKind != repository.CompletionKindComplete || done.NextRetryAt != nil || done.Error != nil {
		t.Errorf("retry did not complete cleanly: %+v", done)
	}
	old, err := h.repo.GetJob(ctx, firstJob)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := h.repo.GetJob(ctx, done.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if old.Status != repository.JobStatusFailed || latest.ID == old.ID || latest.Status != repository.JobStatusDone || latest.Attempt != 2 {
		t.Errorf("incorrect attempt history: old=%+v latest=%+v", old, latest)
	}
	edge.mu.Lock()
	requests := append([]int(nil), edge.segBRequests...)
	edge.mu.Unlock()
	if len(requests) == 0 {
		t.Fatal("retry fetched no remaining media")
	}
	for _, index := range requests {
		if index < 2 {
			t.Errorf("retry re-fetched finalized segment %d: %v", index, requests)
		}
	}
}

func seedArchiveChannel(t *testing.T, repo repository.Repository, id string) {
	t.Helper()
	if _, err := repo.UpsertChannel(context.Background(), &repository.Channel{
		BroadcasterID: id, BroadcasterLogin: id, BroadcasterName: strings.ToUpper(id),
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
}

// TestRealArchive_VODEndToEnd runs a queued VOD through the whole native
// pipeline against the fake edge (VOD token, /vod/ usher, finite fMP4
// playlist, remux, probe, thumbnail, storage) and expects a complete,
// untruncated archive row.
func TestRealArchive_VODEndToEnd(t *testing.T) {
	requireFFmpegHarness(t)
	edge := newTwitchEdge(t, twitchEdgeOpts{tsCount: 1, fmp4Count: 6, windowA: 1, baseSeqA: 100, baseSeqB: 50})
	h := newHarnessService(t, edge.URL())
	bus := eventbus.New()
	h.svc.SetEventBus(bus)
	events := bus.ArchiveQueue.Subscribe(t.Context())
	t.Cleanup(h.svc.Shutdown)
	seedArchiveChannel(t, h.repo, "archivist")
	ctx := context.Background()
	aired := time.Date(2026, 7, 4, 21, 30, 0, 0, time.UTC)

	jobID, err := enqueueAndPump(h.svc, ctx, Params{
		BroadcasterID:    "archivist",
		BroadcasterLogin: "archivist",
		DisplayName:      "ARCHIVIST",
		Title:            "the vod title",
		Quality:          repository.QualityHigh,
		RecordingType:    repository.RecordingTypeVideo,
		VODID:            "424242",
		BroadcastAt:      &aired,
	})
	if err != nil {
		t.Fatalf("EnqueueVOD: %v", err)
	}
	pending, err := h.repo.GetVideoByJobID(ctx, jobID)
	if err != nil {
		t.Fatalf("video by job: %v", err)
	}
	v := waitForVideoStatus(t, h.repo, pending.ID, repository.VideoStatusDone, 60*time.Second)
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
waitForCompletion:
	for {
		select {
		case ev := <-events:
			if ev.Kind == eventbus.ArchiveCompleted && ev.VideoID == v.ID {
				break waitForCompletion
			}
		case <-deadline.C:
			t.Fatal("completed archive did not publish the queue update")
		}
	}

	if v.Source != repository.VideoSourceVOD || v.TwitchVideoID == nil || *v.TwitchVideoID != "424242" {
		t.Errorf("row source=%q vod=%v", v.Source, v.TwitchVideoID)
	}
	if v.BroadcastAt == nil || !v.BroadcastAt.Equal(aired) {
		t.Errorf("broadcast_at = %v, want %v", v.BroadcastAt, aired)
	}
	if v.Title != "the vod title" {
		t.Errorf("title = %q, want the queued VOD title (no live tracking ran)", v.Title)
	}
	if v.Truncated || v.CompletionKind != repository.CompletionKindComplete {
		t.Errorf("truncated=%v kind=%q, want false/complete for a playlist that closed", v.Truncated, v.CompletionKind)
	}
	if v.DurationSeconds == nil || abs(*v.DurationSeconds-6) > 1 {
		t.Errorf("duration = %v, want about 6s", v.DurationSeconds)
	}
	if v.SizeBytes == nil || *v.SizeBytes <= 0 {
		t.Errorf("size = %v, want > 0", v.SizeBytes)
	}
	if v.Thumbnail == nil || *v.Thumbnail == "" {
		t.Errorf("thumbnail unset: the stage 8 part frame must become the row thumbnail")
	} else if ok, err := h.storage.Exists(ctx, *v.Thumbnail); err != nil || !ok {
		t.Errorf("thumbnail %q not in storage (exists=%v err=%v)", *v.Thumbnail, ok, err)
	}
	parts, err := h.repo.ListVideoParts(ctx, v.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("parts = %d, %v; want 1", len(parts), err)
	}
	if parts[0].SegmentFormat != repository.SegmentFormatFMP4 || parts[0].Quality != "360" {
		t.Errorf("part = %+v, want fmp4/360 from the VOD master", parts[0])
	}
	if ok, err := h.storage.Exists(ctx, storagekeys.Video(parts[0].Filename)); err != nil || !ok {
		t.Errorf("part file %q missing from storage (exists=%v err=%v)", parts[0].Filename, ok, err)
	}
	job, err := h.repo.GetJob(ctx, jobID)
	if err != nil || job.Status != repository.JobStatusDone {
		t.Errorf("job = %v, %v; want DONE", job, err)
	}

	// Stage 1 to 3 went through the VOD endpoints with the VOD token.
	if vars := edge.LastGQLVars(); vars["isVod"] != true || vars["vodID"] != "424242" || vars["isLive"] != false {
		t.Errorf("gql variables = %v, want isVod/vodID/isLive=false", vars)
	}
	paths, query := edge.VODUsherRequests()
	if len(paths) == 0 || paths[0] != "/vod/424242.m3u8" {
		t.Errorf("usher paths = %v, want /vod/424242.m3u8", paths)
	}
	if got := query["nauth"]; len(got) != 1 || got[0] != "fake-vod-token-value" {
		t.Errorf("nauth = %v, want the VOD token", got)
	}
	if got := query["nauthsig"]; len(got) != 1 || got[0] != "fake-vod-signature" {
		t.Errorf("nauthsig = %v, want the VOD signature", got)
	}
	if _, live := query["token"]; live {
		t.Errorf("live token parameter sent to the VOD endpoint")
	}

	// No live preview snapshots were captured for an archive.
	if ok, _ := h.storage.Exists(ctx, storagekeys.Snapshot(v.Filename, 0)); ok {
		t.Errorf("a live preview snapshot was stored for an archive")
	}
	if queue, err := h.repo.ListArchiveQueue(ctx); err != nil || len(queue) != 0 {
		t.Errorf("queue after completion = %d rows, %v; want empty", len(queue), err)
	}
	// No title span was opened for the archive: its history is empty and the
	// watch page falls back to the row title.
	if changes, err := h.repo.ListVideoMetadataChanges(ctx, v.ID); err != nil || len(changes) != 0 {
		t.Errorf("archive timeline = %v, %v; want empty", changes, err)
	}
	if titles, err := h.repo.ListTitlesForVideo(ctx, v.ID); err != nil || len(titles) != 0 {
		t.Errorf("archive title spans = %v, %v; want none", titles, err)
	}
}

// TestRealArchive_QueueRunsSequentially pins that with one archive slot the
// second queued VOD only starts once the first has finished.
func TestRealArchive_QueueRunsSequentially(t *testing.T) {
	requireFFmpegHarness(t)
	edge := newTwitchEdge(t, twitchEdgeOpts{tsCount: 1, fmp4Count: 4, windowA: 1, baseSeqA: 100, baseSeqB: 50})
	h := newHarnessService(t, edge.URL())
	t.Cleanup(h.svc.Shutdown)
	seedArchiveChannel(t, h.repo, "archivist")
	ctx := context.Background()
	release := edge.BlockGQL()
	defer release()

	params := func(vodID string) Params {
		return Params{
			BroadcasterID: "archivist", BroadcasterLogin: "archivist", DisplayName: "ARCHIVIST",
			Title: "vod " + vodID, Quality: repository.QualityHigh, RecordingType: repository.RecordingTypeVideo,
			VODID: vodID,
		}
	}
	firstJob, err := enqueueAndPump(h.svc, ctx, params("1"))
	if err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	secondJob, err := enqueueAndPump(h.svc, ctx, params("2"))
	if err != nil {
		t.Fatalf("enqueue second: %v", err)
	}
	first, _ := h.repo.GetVideoByJobID(ctx, firstJob)
	second, _ := h.repo.GetVideoByJobID(ctx, secondJob)
	if first.Status != repository.VideoStatusRunning || second.Status != repository.VideoStatusPending {
		t.Fatalf("statuses = %s/%s, want RUNNING/PENDING with one archive slot", first.Status, second.Status)
	}
	if active := h.svc.ListActiveProgress(); len(active) != 1 || active[0].JobID != firstJob {
		t.Fatalf("active = %+v, want only the first archive", active)
	}

	release()
	firstDone := waitForVideoStatus(t, h.repo, first.ID, repository.VideoStatusDone, 60*time.Second)
	secondDone := waitForVideoStatus(t, h.repo, second.ID, repository.VideoStatusDone, 60*time.Second)
	firstFinished, secondStarted := jobTimes(t, h.repo, firstJob, secondJob)
	if secondStarted.Before(firstFinished) {
		t.Errorf("second archive started at %v before the first finished at %v", secondStarted, firstFinished)
	}
	for _, v := range []*repository.Video{firstDone, secondDone} {
		if v.Truncated || v.CompletionKind != repository.CompletionKindComplete {
			t.Errorf("archive %d truncated=%v kind=%q", v.ID, v.Truncated, v.CompletionKind)
		}
	}
}

func jobTimes(t *testing.T, repo repository.Repository, firstJob, secondJob string) (firstFinished, secondStarted time.Time) {
	t.Helper()
	first, err := repo.GetJob(context.Background(), firstJob)
	if err != nil || first.FinishedAt == nil {
		t.Fatalf("first job = %v, %v; want finished_at", first, err)
	}
	second, err := repo.GetJob(context.Background(), secondJob)
	if err != nil || second.StartedAt == nil {
		t.Fatalf("second job = %v, %v; want started_at", second, err)
	}
	return *first.FinishedAt, *second.StartedAt
}

// TestRealArchive_PosterSurvivesAudioOnly pins the poster of an audio archive:
// it has no frame for the stage 8 thumbnail and no live preview to snapshot,
// so the Twitch VOD thumbnail fetched at enqueue must survive finishing. A
// video archive gets the same early poster and then the real frame replaces it.
func TestRealArchive_PosterSurvivesAudioOnly(t *testing.T) {
	requireFFmpegHarness(t)
	// Six seconds of test pattern: shorter synthetic clips can read as
	// monochrome to the thumbnailer, which would leave the video archive on
	// its early poster and blur what this test checks.
	edge := newTwitchEdge(t, twitchEdgeOpts{tsCount: 1, fmp4Count: 6, windowA: 1, baseSeqA: 100, baseSeqB: 50})
	h := newHarnessService(t, edge.URL())
	t.Cleanup(h.svc.Shutdown)
	seedArchiveChannel(t, h.repo, "archivist")
	ctx := context.Background()

	params := func(vodID, recordingType string) Params {
		return Params{
			BroadcasterID: "archivist", BroadcasterLogin: "archivist", DisplayName: "ARCHIVIST",
			Title: "vod " + vodID, Quality: repository.QualityHigh, RecordingType: recordingType,
			VODID: vodID, PosterURL: edge.URL() + "/poster.jpg",
		}
	}
	audioJob, err := enqueueAndPump(h.svc, ctx, params("audio-1", repository.RecordingTypeAudio))
	if err != nil {
		t.Fatalf("enqueue audio: %v", err)
	}
	audio, _ := h.repo.GetVideoByJobID(ctx, audioJob)
	done := waitForVideoStatus(t, h.repo, audio.ID, repository.VideoStatusDone, 60*time.Second)
	poster := storagekeys.Snapshot(done.Filename, 0)
	if done.Thumbnail == nil || *done.Thumbnail != poster {
		t.Fatalf("audio archive thumbnail = %v, want the Twitch poster %q", done.Thumbnail, poster)
	}
	if ok, err := h.storage.Exists(ctx, poster); err != nil || !ok {
		t.Fatalf("poster object missing (exists=%v err=%v)", ok, err)
	}

	videoJob, err := enqueueAndPump(h.svc, ctx, params("video-1", repository.RecordingTypeVideo))
	if err != nil {
		t.Fatalf("enqueue video: %v", err)
	}
	video, _ := h.repo.GetVideoByJobID(ctx, videoJob)
	done = waitForVideoStatus(t, h.repo, video.ID, repository.VideoStatusDone, 60*time.Second)
	if done.Thumbnail == nil || !strings.HasSuffix(*done.Thumbnail, "-part01.jpg") {
		t.Fatalf("video archive thumbnail = %v, want the stage 8 frame", done.Thumbnail)
	}
	if ok, _ := h.storage.Exists(ctx, storagekeys.Snapshot(done.Filename, 0)); !ok {
		t.Fatalf("video archive lost its early poster object")
	}
}

// TestRealArchive_TransientFailureRetriesAndCompletes runs a first attempt
// against an edge whose segments answer 503. The attempt fails for a passing
// reason and is scheduled for retry; the requeued attempt completes the
// archive: one video row, two job rows, and no live metadata history.
func TestRealArchive_TransientFailureRetriesAndCompletes(t *testing.T) {
	requireFFmpegHarness(t)
	edge := newTwitchEdge(t, twitchEdgeOpts{tsCount: 1, fmp4Count: 4, windowA: 1, baseSeqA: 100, baseSeqB: 50})
	edge.FailFMP4Segments(1 << 20)
	h := newHarnessService(t, edge.URL())
	t.Cleanup(h.svc.Shutdown)
	seedArchiveChannel(t, h.repo, "archivist")
	ctx := context.Background()

	firstJob, err := enqueueAndPump(h.svc, ctx, Params{
		BroadcasterID: "archivist", BroadcasterLogin: "archivist", DisplayName: "ARCHIVIST",
		Title: "flaky vod", Quality: repository.QualityHigh, RecordingType: repository.RecordingTypeVideo, VODID: "5150",
	})
	if err != nil {
		t.Fatalf("EnqueueVOD: %v", err)
	}
	queued, err := h.repo.GetVideoByJobID(ctx, firstJob)
	if err != nil {
		t.Fatal(err)
	}
	failed := waitForVideoStatus(t, h.repo, queued.ID, repository.VideoStatusFailed, 60*time.Second)
	if failed.NextRetryAt == nil || failed.Error == nil {
		t.Fatalf("first attempt = %+v, want FAILED with a retry scheduled and an error", failed)
	}
	if job, err := h.repo.GetJob(ctx, firstJob); err != nil || job.Status != repository.JobStatusFailed || job.Attempt != 1 {
		t.Fatalf("first job = %+v, %v; want FAILED attempt 1", job, err)
	}

	// The edge recovers; bring the retry forward and let the pump requeue it.
	edge.FailFMP4Segments(0)
	if err := h.repo.MarkArchiveFailedForRetry(ctx, queued.ID, *failed.Error, failed.CompletionKind, failed.Truncated, time.Now().UTC().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	h.svc.PumpArchiveQueue(ctx)
	done := waitForVideoStatus(t, h.repo, queued.ID, repository.VideoStatusDone, 60*time.Second)
	if done.JobID == firstJob || done.NextRetryAt != nil || done.Error != nil {
		t.Fatalf("completed row = %+v, want a new job and no failure left", done)
	}
	if done.Truncated || done.CompletionKind != repository.CompletionKindComplete {
		t.Errorf("truncated=%v kind=%q, want a complete archive", done.Truncated, done.CompletionKind)
	}
	if done.DurationSeconds == nil || abs(*done.DurationSeconds-4) > 1 {
		t.Errorf("duration = %v, want about 4s", done.DurationSeconds)
	}
	second, err := h.repo.GetJob(ctx, done.JobID)
	if err != nil || second.Status != repository.JobStatusDone || second.Attempt != 2 {
		t.Fatalf("second job = %+v, %v; want DONE attempt 2", second, err)
	}
	parts, err := h.repo.ListVideoParts(ctx, done.ID)
	if err != nil || len(parts) != 1 || parts[0].SizeBytes <= 0 {
		t.Fatalf("parts = %+v, %v; want one finalized part", parts, err)
	}
	if changes, err := h.repo.ListVideoMetadataChanges(ctx, done.ID); err != nil || len(changes) != 0 {
		t.Errorf("archive timeline = %v, %v; want empty", changes, err)
	}
	if titles, err := h.repo.ListTitlesForVideo(ctx, done.ID); err != nil || len(titles) != 0 {
		t.Errorf("archive title spans = %v, %v; want none", titles, err)
	}
}
