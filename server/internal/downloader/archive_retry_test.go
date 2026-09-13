package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/testutil/mediatest"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/downloader/hls"
	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
)

func TestSegmentHostConnectionCap(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.DownloadConfig
		want int
	}{
		{"live and archive slots both count", config.DownloadConfig{MaxConcurrent: 2, ArchiveMaxConcurrent: 1, SegmentConcurrency: 4}, 12},
		{"two archive slots", config.DownloadConfig{MaxConcurrent: 5, ArchiveMaxConcurrent: 2, SegmentConcurrency: 4}, 28},
		{"zero config still leaves one connection per slot", config.DownloadConfig{}, 2},
	}
	for _, tc := range cases {
		if got := segmentHostConnectionCap(tc.cfg); got != tc.want {
			t.Errorf("%s: cap = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestArchiveRateLimiter(t *testing.T) {
	if l := archiveRateLimiter(config.DownloadConfig{}); l != nil {
		t.Fatalf("no cap must mean no limiter, got %v", l)
	}
	l := archiveRateLimiter(config.DownloadConfig{ArchiveMaxBytesPerSecond: 1 << 20})
	if l == nil || l.Burst() != 1<<20 {
		t.Fatalf("limiter = %v, want a one-second bucket of 1 MiB", l)
	}
}

func TestArchiveRetryDelay(t *testing.T) {
	want := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour}
	for i, d := range want {
		got, ok := archiveRetryDelay(int32(i + 1))
		if !ok || got != d {
			t.Errorf("attempt %d: delay = %v, %v; want %v", i+1, got, ok, d)
		}
	}
	for _, attempt := range []int32{0, 5, 9} {
		if _, ok := archiveRetryDelay(attempt); ok {
			t.Errorf("attempt %d: still retried, want the budget exhausted", attempt)
		}
	}
}

type fakeNetError struct{}

func (fakeNetError) Error() string   { return "dial tcp: i/o timeout" }
func (fakeNetError) Timeout() bool   { return true }
func (fakeNetError) Temporary() bool { return true }

func TestArchiveRetryable(t *testing.T) {
	var _ net.Error = fakeNetError{}
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"cancelled by operator", ErrCancelled, false},
		{"context cancelled", context.Canceled, false},
		{"empty playback token", &playbackResolutionError{cause: errors.Join(twitch.ErrPlaybackTokenEmpty)}, false},
		{"vod gone at usher", &playbackResolutionError{cause: twitch.NewAuthError(http.StatusNotFound, []byte(`[{"error_code":"vod_not_found"}]`))}, false},
		{"usher overloaded", &playbackResolutionError{cause: twitch.NewAuthError(http.StatusServiceUnavailable, nil)}, true},
		{"usher rate limited", twitch.NewAuthError(http.StatusTooManyRequests, nil), true},
		{"usher forbidden", twitch.NewAuthError(http.StatusForbidden, nil), false},
		{"network during resolution", &playbackResolutionError{cause: fakeNetError{}}, true},
		{"transport exhausted on a segment", &hls.FetchError{Kind: hls.FetchKindTransport, Cause: io.ErrUnexpectedEOF, Permanent: true}, true},
		{"edge 5xx on a segment", &hls.FetchError{Kind: hls.FetchKindServer, Status: 503, Permanent: true}, true},
		{"cdn lag on a segment", &hls.FetchError{Kind: hls.FetchKindCDNLag, Status: 404, Permanent: true}, true},
		{"permanent auth on a segment", &hls.FetchError{Kind: hls.FetchKindAuth, Status: 403, Permanent: true}, false},
		{"malformed segment", &hls.FetchError{Kind: hls.FetchKindMalformed, Status: 418, Permanent: true}, false},
		{"gap policy abort", &hls.GapAbortError{}, true},
		{"playlist gone", hls.ErrPlaylistGone, false},
		{"playlist auth renewable", hls.ErrPlaylistAuth, true},
		{"bare network error", fakeNetError{}, true},
		{"remux failure", errors.New("remux: exit status 1"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		if got := archiveRetryable(tc.err); got != tc.want {
			t.Errorf("%s: retryable = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestArchiveFailureMessage(t *testing.T) {
	if got := archiveFailureMessage(&playbackResolutionError{cause: twitch.ErrPlaybackTokenEmpty}); !strings.Contains(got, "subscriber session") {
		t.Errorf("empty token message = %q", got)
	}
	if got := archiveFailureMessage(fmt.Errorf("fetch playlist: %w: status 403", hls.ErrPlaylistAuthPermanent)); !strings.Contains(got, "subscriber session") || strings.Contains(got, "403") {
		t.Errorf("permanent playlist refusal message = %q, want the subscriber-session guidance without the raw status", got)
	}
	if got := archiveFailureMessage(twitch.NewAuthError(http.StatusNotFound, nil)); !strings.Contains(got, "deleted") {
		t.Errorf("not found message = %q", got)
	}
	if got := archiveFailureMessage(errors.New("remux: boom")); got != "remux: boom" {
		t.Errorf("plain error message = %q, want the error text", got)
	}
}

func TestContinuationResumeState(t *testing.T) {
	failedAtAuth := NewResumeState()
	failedAtAuth.PosterURL = "https://cdn.example/poster.jpg"
	fresh := continuationResumeState(failedAtAuth, nil)
	if fresh.CurrentPartIndex != 1 || fresh.PartStarted || fresh.Stage != StageAuth || fresh.SelectedQuality != "" {
		t.Fatalf("fresh state = %+v", fresh)
	}
	if fresh.PosterURL != failedAtAuth.PosterURL {
		t.Fatalf("fresh state dropped the poster url: %+v", fresh)
	}
	if nilPrev := continuationResumeState(nil, nil); nilPrev.CurrentPartIndex != 1 || nilPrev.PosterURL != "" {
		t.Fatalf("state without a checkpoint = %+v", nilPrev)
	}

	end := int64(149)
	fps := 60.0
	parts := []repository.VideoPart{
		{PartIndex: 1, SizeBytes: 4096, EndMediaSeq: &end, Quality: "1080p60", FPS: &fps, Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4},
		{PartIndex: 2, SizeBytes: 0, Quality: "720p", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4}, // created but never finalized
	}
	// The failed attempt never resolved a rendition, so its checkpoint holds
	// no lock; the continuation still locks to the part it appends to.
	cont := continuationResumeState(failedAtAuth, parts)
	if cont.CurrentPartIndex != 2 || !cont.PartStarted || cont.PartStartMediaSequence != 150 || cont.AccountedFrontierMediaSeq != 149 {
		t.Fatalf("continuation = %+v, want part 2 starting at seq 150", cont)
	}
	if cont.SelectedQuality != "1080p60" || cont.SelectedFPS == nil || *cont.SelectedFPS != 60 || cont.SelectedCodec != repository.CodecH264 || cont.SegmentFormat != repository.SegmentFormatFMP4 {
		t.Fatalf("continuation lock = %q/%v/%q/%q, want part 1's rendition", cont.SelectedQuality, cont.SelectedFPS, cont.SelectedCodec, cont.SegmentFormat)
	}
	if cont.PosterURL != failedAtAuth.PosterURL {
		t.Fatalf("continuation dropped the poster url: %+v", cont)
	}
	if cont.EndListSeen || cont.HadWindowRoll || cont.Stage != StageAuth || len(cont.Gaps) != 0 {
		t.Fatalf("continuation carried stale flags: %+v", cont)
	}
}

func collectArchiveEvents(t *testing.T, ch <-chan eventbus.ArchiveQueueEvent, n int) []eventbus.ArchiveQueueKind {
	t.Helper()
	var kinds []eventbus.ArchiveQueueKind
	deadline := time.After(5 * time.Second)
	for len(kinds) < n {
		select {
		case ev := <-ch:
			kinds = append(kinds, ev.Kind)
		case <-deadline:
			t.Fatalf("saw %v, want %d archive events", kinds, n)
		}
	}
	return kinds
}

func failArchiveAttempt(t *testing.T, f *archiveFixture, jobID string, attempt int32, cause error) *repository.Video {
	t.Helper()
	v := f.video(t, jobID)
	d := &download{jobID: jobID, videoID: v.ID, broadcasterID: v.BroadcasterID, vod: true, attempt: attempt, resume: NewResumeState()}
	f.svc.failDownload(context.Background(), d, discardLog(), cause)
	return v
}

func TestArchiveFailure_SchedulesRetryThenPumpRequeues(t *testing.T) {
	f := newArchiveFixture(t, 2, 1)
	bus := eventbus.New()
	f.svc.SetEventBus(bus)
	events := bus.ArchiveQueue.Subscribe(t.Context())
	terminal := bus.RecordingTerminal.Subscribe(t.Context())
	release := f.edge.hold()
	defer release()
	ctx := context.Background()

	jobID, err := f.svc.EnqueueVOD(ctx, vodParams("bc-1", "1"))
	if err != nil {
		t.Fatal(err)
	}
	v := failArchiveAttempt(t, f, jobID, 1, &hls.FetchError{Kind: hls.FetchKindTransport, Attempts: 5, Cause: io.ErrUnexpectedEOF, Permanent: true})

	failed := f.video(t, jobID)
	if failed.Status != repository.VideoStatusFailed || failed.NextRetryAt == nil {
		t.Fatalf("after a transient failure = %+v, want FAILED with a retry scheduled", failed)
	}
	if wait := time.Until(*failed.NextRetryAt); wait < 50*time.Second || wait > 70*time.Second {
		t.Fatalf("first retry in %v, want about a minute", wait)
	}
	if failed.Error == nil || !strings.Contains(*failed.Error, "transport") {
		t.Fatalf("error = %v, want the fetch error text", failed.Error)
	}
	if job, _ := f.repo.GetJob(ctx, jobID); job.Status != repository.JobStatusFailed {
		t.Fatalf("first job = %+v, want FAILED", job)
	}
	select {
	case ev := <-terminal:
		t.Fatalf("a retryable failure published a terminal event: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}

	// Not due yet: the pump leaves the row alone.
	f.svc.PumpArchiveQueue(ctx)
	if f.status(t, jobID) != repository.VideoStatusFailed {
		t.Fatal("the pump requeued a retry that was not due")
	}

	// Once due, the pump creates the second attempt and starts it.
	if err := f.repo.MarkArchiveFailedForRetry(ctx, v.ID, *failed.Error, failed.CompletionKind, failed.Truncated, time.Now().UTC().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	// A due retry waits through an outage without consuming its next attempt.
	setDownloaderGate(t, f.svc, gateFunc(unattached))
	f.svc.PumpArchiveQueue(ctx)
	waiting, err := f.repo.GetVideo(ctx, v.ID)
	if err != nil || waiting.JobID != jobID || waiting.Status != repository.VideoStatusFailed || waiting.NextRetryAt == nil || f.activeJobs() != 0 {
		t.Fatalf("storage outage changed due retry: %+v, %v", waiting, err)
	}
	setDownloaderGate(t, f.svc, gateFunc(func() error { return nil }))
	f.svc.PumpArchiveQueue(ctx)
	waitUntil(t, "retry claim", func() bool {
		v, e := f.repo.GetVideo(ctx, v.ID)
		return e == nil && v.Status == repository.VideoStatusRunning
	})
	retried, err := f.repo.GetVideo(ctx, v.ID)
	if err != nil || retried.Status != repository.VideoStatusRunning || retried.JobID == jobID || retried.NextRetryAt != nil || retried.Error != nil {
		t.Fatalf("retried row = %+v, %v; want RUNNING under a new job with no failure left", retried, err)
	}
	second, err := f.repo.GetJob(ctx, retried.JobID)
	if err != nil || second.Attempt != 2 || second.Status != repository.JobStatusRunning {
		t.Fatalf("second job = %+v, %v; want attempt 2 RUNNING", second, err)
	}
	if got := collectArchiveEvents(t, events, 4); got[0] != eventbus.ArchiveQueued || got[1] != eventbus.ArchiveFailed || got[2] != eventbus.ArchiveQueued || got[3] != eventbus.ArchiveStarted {
		t.Fatalf("events = %v, want queued, failed, queued, started", got)
	}

	// An operator stop never schedules a retry.
	f.svc.Cancel(retried.JobID)
	waitUntil(t, "cancel to land", func() bool { return f.status(t, retried.JobID) == repository.VideoStatusFailed })
	cancelled := f.video(t, retried.JobID)
	if cancelled.CompletionKind != repository.CompletionKindCancelled || cancelled.NextRetryAt != nil {
		t.Fatalf("cancelled row = %+v, want cancelled with no retry", cancelled)
	}
	if got := collectArchiveEvents(t, events, 1); got[0] != eventbus.ArchiveFailed {
		t.Fatalf("cancel event = %v", got)
	}
}

func TestArchiveFailure_TerminalWhenPermanentOrExhausted(t *testing.T) {
	f := newArchiveFixture(t, 2, 1)
	bus := eventbus.New()
	f.svc.SetEventBus(bus)
	terminal := bus.RecordingTerminal.Subscribe(t.Context())
	ctx := context.Background()

	gone, _ := f.svc.EnqueueVOD(ctx, vodParams("bc-1", "1"))
	failArchiveAttempt(t, f, gone, 1, &playbackResolutionError{cause: twitch.NewAuthError(http.StatusNotFound, []byte(`[{"error_code":"vod_not_found"}]`))})
	v := f.video(t, gone)
	if v.Status != repository.VideoStatusFailed || v.NextRetryAt != nil {
		t.Fatalf("permanent failure = %+v, want FAILED without a retry", v)
	}
	if v.Error == nil || !strings.Contains(*v.Error, "deleted") {
		t.Fatalf("permanent failure error = %v, want the readable message", v.Error)
	}
	select {
	case ev := <-terminal:
		if ev.VideoID != v.ID || ev.Kind != eventbus.RecordingFailed {
			t.Fatalf("terminal event = %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal failure published no recording event")
	}

	exhausted, _ := f.svc.EnqueueVOD(ctx, vodParams("bc-1", "2"))
	failArchiveAttempt(t, f, exhausted, 5, &hls.FetchError{Kind: hls.FetchKindServer, Status: 503, Permanent: true})
	if v := f.video(t, exhausted); v.NextRetryAt != nil {
		t.Fatalf("fifth attempt scheduled another retry: %+v", v)
	}

	locked, _ := f.svc.EnqueueVOD(ctx, vodParams("bc-1", "3"))
	failArchiveAttempt(t, f, locked, 1, &playbackResolutionError{cause: twitch.ErrPlaybackTokenEmpty})
	if v := f.video(t, locked); v.NextRetryAt != nil || v.Error == nil || !strings.Contains(*v.Error, "subscriber session") {
		t.Fatalf("empty token failure = %+v, want a terminal readable failure", v)
	}
}

func TestRetryArchive_ManualAndGuards(t *testing.T) {
	f := newArchiveFixture(t, 2, 1)
	bus := eventbus.New()
	f.svc.SetEventBus(bus)
	events := bus.ArchiveQueue.Subscribe(t.Context())
	release := f.edge.hold()
	defer release()
	ctx := context.Background()

	jobID, _ := f.svc.EnqueueVOD(ctx, vodParams("bc-1", "1"))
	v := failArchiveAttempt(t, f, jobID, 5, &hls.FetchError{Kind: hls.FetchKindServer, Status: 503, Permanent: true})
	collectArchiveEvents(t, events, 2)

	if err := f.svc.RetryArchive(ctx, v.ID); err != nil {
		t.Fatalf("RetryArchive: %v", err)
	}
	waitUntil(t, "manual retry claim", func() bool {
		row, e := f.repo.GetVideo(ctx, v.ID)
		return e == nil && row.Status == repository.VideoStatusRunning
	})
	running, _ := f.repo.GetVideo(ctx, v.ID)
	job, _ := f.repo.GetJob(ctx, running.JobID)
	// The next attempt number comes from the stored job, not the in-memory
	// download that failed.
	if running.Status != repository.VideoStatusRunning || job.Attempt != 2 {
		t.Fatalf("after manual retry = %+v / job %+v, want RUNNING attempt 2", running, job)
	}
	if got := collectArchiveEvents(t, events, 2); got[0] != eventbus.ArchiveQueued || got[1] != eventbus.ArchiveStarted {
		t.Fatalf("manual retry events = %v", got)
	}
	if err := f.svc.RetryArchive(ctx, v.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("retry of a running archive err = %v, want ErrNotFound (not failed)", err)
	}
	if err := f.svc.RetryArchive(ctx, 999999); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("retry of an unknown id err = %v, want ErrNotFound", err)
	}

	liveJob, err := f.svc.Start(ctx, Params{BroadcasterID: "bc-2", BroadcasterLogin: "bc-2", DisplayName: "bc-2", Quality: repository.QualityHigh})
	if err != nil {
		t.Fatal(err)
	}
	live := f.video(t, liveJob)
	f.svc.Cancel(liveJob)
	waitUntil(t, "live cancel", func() bool { return f.status(t, liveJob) == repository.VideoStatusFailed })
	if err := f.svc.RetryArchive(ctx, live.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("retry of a live recording err = %v, want ErrNotFound", err)
	}

	// Cancelling a scheduled retry releases the VOD and is reported once.
	f.svc.Cancel(running.JobID)
	waitUntil(t, "archive cancel", func() bool { return f.activeJobs() == 0 })
	if err := f.repo.MarkArchiveFailedForRetry(ctx, v.ID, "x", repository.CompletionKindComplete, false, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.CancelArchiveRetry(ctx, v.ID); err != nil {
		t.Fatalf("CancelArchiveRetry: %v", err)
	}
	if err := f.svc.CancelArchiveRetry(ctx, v.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("second cancel err = %v, want ErrNotFound", err)
	}
	if row, _ := f.repo.GetVideo(ctx, v.ID); row.NextRetryAt != nil {
		t.Fatalf("retry still scheduled after cancel: %+v", row)
	}
	if _, err := f.repo.GetOpenVideoByTwitchVideoID(ctx, "1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("cancelled retry still holds the VOD: %v", err)
	}
}

func TestEnqueueVOD_OpensNoMetadataSpans(t *testing.T) {
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	hydrator := streammeta.NewHydrator(repo, nil, streammeta.Config{}, log)
	cfg := &config.Config{
		Env:        config.Environment{ScratchDir: t.TempDir()},
		App:        config.AppConfig{Download: config.DownloadConfig{MaxConcurrent: 2, ArchiveMaxConcurrent: 1, SegmentConcurrency: 2}},
		ServerMode: config.ServerModeConfig{Mode: config.ServerModeOff},
	}
	svc := NewService(cfg, repo, mediatest.NewAt(t, repo, store, nil, nil, cfg.Env.ScratchDir), hydrator, nil, nil, log)
	edge := newVODEdge(t)
	svc.twitch = twitch.New(twitch.Config{
		HTTPClient: &http.Client{Timeout: 10 * time.Second}, ClientID: "test", UserAgent: "test", DeviceID: "test-device",
		GQLURL: edge.srv.URL + "/gql", IntegrityURL: edge.srv.URL + "/integrity", UsherBaseURL: edge.srv.URL,
	}, log)
	t.Cleanup(svc.Shutdown)
	release := edge.hold()
	defer release()
	ctx := context.Background()
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "bc-1", BroadcasterLogin: "bc-1", BroadcasterName: "bc-1"}); err != nil {
		t.Fatal(err)
	}

	jobID, err := svc.EnqueueVOD(ctx, vodParams("bc-1", "77"))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := repo.GetVideoByJobID(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	if titles, err := repo.ListTitlesForVideo(ctx, archive.ID); err != nil || len(titles) != 0 {
		t.Fatalf("archive title spans = %v, %v; want none (the row title is the title)", titles, err)
	}
	if changes, err := repo.ListVideoMetadataChanges(ctx, archive.ID); err != nil || len(changes) != 0 {
		t.Fatalf("archive timeline = %v, %v; want empty", changes, err)
	}
	if archive.Title != "vod 77" {
		t.Fatalf("archive title = %q", archive.Title)
	}

	// The same hydrator does open a span for a live recording, so the archive
	// assertion above is not a no-op.
	liveJob, err := svc.Start(ctx, Params{BroadcasterID: "bc-1", BroadcasterLogin: "bc-1", DisplayName: "bc-1", Title: "live title", Quality: repository.QualityHigh})
	if err != nil {
		t.Fatal(err)
	}
	live, _ := repo.GetVideoByJobID(ctx, liveJob)
	if titles, _ := repo.ListTitlesForVideo(ctx, live.ID); len(titles) != 1 {
		t.Fatalf("live title spans = %v, want the opening title", titles)
	}
}

func TestRestartJob_OnlyArchivesGetTheRateLimiter(t *testing.T) {
	f := newArchiveFixture(t, 2, 1)
	f.svc.cfg.App.Download.ArchiveMaxBytesPerSecond = 512 << 10
	release := f.edge.hold()
	defer release()
	ctx := context.Background()

	archiveJob, err := enqueueAndPump(f.svc, ctx, vodParams("bc-1", "1"))
	if err != nil {
		t.Fatal(err)
	}
	liveJob, err := f.svc.Start(ctx, Params{BroadcasterID: "bc-2", BroadcasterLogin: "bc-2", DisplayName: "bc-2", Quality: repository.QualityHigh})
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "both jobs active", func() bool { return f.activeJobs() == 2 })
	f.svc.mu.Lock()
	archive, live := f.svc.active[archiveJob], f.svc.active[liveJob]
	f.svc.mu.Unlock()
	if archive.limiter == nil || archive.limiter.Burst() != 512<<10 {
		t.Fatalf("archive limiter = %v, want a 512 KiB bucket", archive.limiter)
	}
	if live.limiter != nil {
		t.Fatalf("live recording got a limiter: %v", live.limiter)
	}
}

// TestSealedCaptureKeepsItsRetryability pins that sealing a capture (storing
// the media before failing the job) does not turn a retryable cause into a
// permanent failure: the classification made with the typed cause in hand
// rides the checkpoint, and the row text stays the readable reason.
func TestSealedCaptureKeepsItsRetryability(t *testing.T) {
	retryable := &playbackResolutionError{cause: twitch.NewAuthError(http.StatusServiceUnavailable, nil)}
	permanent := &playbackResolutionError{cause: twitch.ErrPlaybackTokenEmpty}
	for _, tc := range []struct {
		name  string
		cause error
		want  bool
	}{
		{name: "transient", cause: retryable, want: true},
		{name: "permanent", cause: permanent, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := NewResumeState()
			state.CaptureError = playbackCaptureFailure(tc.cause)
			state.CaptureRetryable = archiveRetryable(tc.cause)
			raw, err := state.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			restored, err := UnmarshalResumeState(raw)
			if err != nil {
				t.Fatal(err)
			}
			sealed := sealedCaptureFailure(restored)
			if got := archiveRetryable(sealed); got != tc.want {
				t.Fatalf("retryable after the checkpoint round trip = %v, want %v", got, tc.want)
			}
			if got := archiveFailureMessage(sealed); got != state.CaptureError {
				t.Fatalf("row message = %q, want the sealed reason %q", got, state.CaptureError)
			}
		})
	}
}

// TestArchiveFailureMessage_NeverLeaksUpstreamText pins the row's contract for
// the errors the resolver and poller actually produce: a transport failure
// carrying the signed usher URL, a 5xx with an HTML body, and a poller status
// line with a body preview. None of that text may reach the row.
func TestArchiveFailureMessage_NeverLeaksUpstreamText(t *testing.T) {
	const (
		tokenValue = `{"adblock":false,"device_id":"dev-abc123","user_id":424242,"vod_id":2380152875}`
		tokenSig   = "9f2c0deadbeefcafe1234567890abcdef0fedcba"
	)
	token := twitch.PlaybackToken{Value: tokenValue, Signature: tokenSig}
	log := slog.New(slog.DiscardHandler)

	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := closed.URL
	closed.Close()
	_, transportErr := twitch.New(twitch.Config{UsherBaseURL: closedURL}, log).FetchVODMasterPlaylist(context.Background(), "2380152875", token, twitch.SelectOptions{})
	if transportErr == nil {
		t.Fatal("expected a transport error")
	}

	unavailable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("<html><body>internal-edge-node-77 diagnostic</body></html>"))
	}))
	defer unavailable.Close()
	_, serverErr := twitch.New(twitch.Config{UsherBaseURL: unavailable.URL}, log).FetchVODMasterPlaylist(context.Background(), "2380152875", token, twitch.SelectOptions{})
	if serverErr == nil {
		t.Fatal("expected a 503 error")
	}

	cases := []struct {
		name  string
		err   error
		leaks []string
		want  string
	}{
		{name: "transport with signed url", err: &playbackResolutionError{cause: fmt.Errorf("master playlist: %w", transportErr)}, leaks: []string{tokenSig, "user_id", "http"}, want: "network error reaching"},
		{name: "usher 503 body", err: &playbackResolutionError{cause: fmt.Errorf("master playlist: %w", serverErr)}, leaks: []string{"internal-edge-node-77", "<html"}, want: "HTTP 503"},
		{name: "poller status body", err: fmt.Errorf("hls poller: status 503: %s", "<html>cloudfront-diagnostic-99</html>"), leaks: []string{"cloudfront-diagnostic-99", "<html"}, want: "status 503"},
		{name: "fetch error", err: &hls.FetchError{Kind: hls.FetchKindTransport, Attempts: 5, Cause: fmt.Errorf("Get %q: dial tcp: refused", "https://edge.example/seg.ts?sig="+tokenSig)}, leaks: []string{tokenSig, "edge.example"}, want: "download interrupted"},
		{name: "unknown error with url", err: fmt.Errorf("remux: input %s unreadable", "https://cdn.example/x.m3u8?token="+tokenSig), leaks: []string{tokenSig}, want: "[url]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := archiveFailureMessage(tc.err)
			for _, leak := range tc.leaks {
				if strings.Contains(msg, leak) {
					t.Fatalf("message %q leaks %q", msg, leak)
				}
			}
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("message %q, want it to mention %q", msg, tc.want)
			}
		})
	}
}

func TestArchiveRetryFailureWritesCommitTogether(t *testing.T) {
	f := newArchiveFixture(t, 1, 1)
	ctx := t.Context()
	id, err := f.svc.EnqueueVOD(ctx, vodParams("bc-1", "rollback"))
	if err != nil {
		t.Fatal(err)
	}
	v := f.video(t, id)
	if err := f.repo.SetJobExecution(ctx, id, "", false); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.UpdateVideoStatus(ctx, v.ID, repository.VideoStatusRunning); err != nil {
		t.Fatal(err)
	}
	bus := eventbus.New()
	f.svc.SetEventBus(bus)
	changes := bus.ArchiveQueue.Subscribe(ctx)
	terminals := bus.RecordingTerminal.Subscribe(ctx)
	f.svc.repo = &archiveFaultRepo{Repository: f.repo, failJobFailed: true}
	d := &download{jobID: id, videoID: v.ID, broadcasterID: v.BroadcasterID, vod: true, attempt: 1, resume: NewResumeState()}
	settleCtx, stopSettlement := context.WithCancel(ctx)
	stopSettlement()
	d.runCtx = settleCtx
	f.svc.failDownload(ctx, d, discardLog(), fakeNetError{})
	got := f.video(t, id)
	job, err := f.repo.GetJob(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != repository.VideoStatusRunning || got.NextRetryAt != nil || job.Status != repository.JobStatusRunning || d.cleanupScratch {
		t.Fatalf("failed transaction lost recovery state: video=%+v job=%+v cleanup=%v", got, job, d.cleanupScratch)
	}
	select {
	case ev := <-changes:
		t.Fatalf("uncommitted retry announced: %+v", ev)
	default:
	}
	select {
	case ev := <-terminals:
		t.Fatalf("DB failure announced as terminal: %+v", ev)
	default:
	}
	f.svc.repo = f.repo
	f.svc.failDownload(ctx, d, discardLog(), fakeNetError{})
	got = f.video(t, id)
	job, _ = f.repo.GetJob(ctx, id)
	if got.NextRetryAt == nil || job.Status != repository.JobStatusFailed {
		t.Fatalf("successful retry bookkeeping: %+v %+v", got, job)
	}
}

func TestArchiveStorageOutageLeavesSameAttemptQueued(t *testing.T) {
	for _, failAt := range []int{2, 3, 4} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			f := newArchiveFixture(t, 1, 1)
			id, err := f.svc.EnqueueVOD(t.Context(), vodParams("bc-1", "paused"))
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			setDownloaderGate(t, f.svc, gateFunc(func() error {
				calls++
				if calls >= failAt {
					return unattached()
				}
				return nil
			}))
			f.svc.PumpArchiveQueue(t.Context())
			v := f.video(t, id)
			job, err := f.repo.GetJob(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			if v.Status != repository.VideoStatusPending || v.NextRetryAt != nil || v.Error != nil || job.Status != repository.JobStatusPending || job.Attempt != 1 || f.activeJobs() != 0 {
				t.Fatalf("storage outage consumed/failed attempt: %+v %+v", v, job)
			}
			release := f.edge.hold()
			defer release()
			setDownloaderGate(t, f.svc, gateFunc(func() error { return nil }))
			f.svc.PumpArchiveQueue(t.Context())
			waitUntil(t, "archive claim", func() bool { return f.status(t, id) == repository.VideoStatusRunning })
			job, err = f.repo.GetJob(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			if job.Status != repository.JobStatusRunning || job.Attempt != 1 || f.activeJobs() != 1 {
				t.Fatalf("recovery did not start original attempt: %+v", job)
			}
		})
	}
}

type blockingRetryRepo struct {
	repository.Repository
	block   atomic.Bool
	entered chan struct{}
	calls   atomic.Int32
}

func (r *blockingRetryRepo) ListArchivesDueForRetry(ctx context.Context, before, after time.Time, afterID int64, limit int) ([]repository.Video, error) {
	if !r.block.Load() {
		return r.Repository.ListArchivesDueForRetry(ctx, before, after, afterID, limit)
	}
	r.calls.Add(1)
	select {
	case r.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestArchiveRetryLoopRunsOnceAndShutdownWaitsForIt(t *testing.T) {
	f := newArchiveFixture(t, 1, 1)
	f.svc.retryInterval = time.Millisecond
	repo := &blockingRetryRepo{Repository: f.repo, entered: make(chan struct{}, 2)}
	f.svc.repo = repo
	setDownloaderGate(t, f.svc, gateFunc(func() error { return nil }))
	if err := f.svc.Resume(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Resume(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo.block.Store(true)
	select {
	case <-repo.entered:
	case <-time.After(time.Second):
		t.Fatal("Resume did not start the periodic retry pump")
	}
	done := make(chan struct{})
	go func() { f.svc.Shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Shutdown did not cancel and await the in-flight pump")
	}
	if repo.calls.Load() != 1 {
		t.Fatalf("concurrent/repeated retry workers: %d", repo.calls.Load())
	}
	f.svc.startArchiveRetryLoop()
	f.svc.Shutdown()
}

func TestArchiveRetryWaitsForMediaOwnership(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		t.Run(fmt.Sprintf("scheduled=%v", scheduled), func(t *testing.T) {
			f := newArchiveFixture(t, 1, 1)
			jobID, err := f.svc.EnqueueVOD(t.Context(), vodParams("bc-1", "locked-retry"))
			if err != nil {
				t.Fatal(err)
			}
			v := failArchiveAttempt(t, f, jobID, 1, fakeNetError{})
			owned, err := f.svc.storage.Lock(t.Context(), v.ID)
			if err != nil {
				t.Fatal(err)
			}
			defer owned.Close()

			// Retention may already be deleting the previous attempt's parts.
			// A retry cannot publish PENDING until that ownership is released.
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			if _, err := f.svc.requeueArchive(ctx, v, scheduled); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("retry bypassed media ownership: %v", err)
			}
			current, err := f.repo.GetVideo(t.Context(), v.ID)
			if err != nil || current.JobID != jobID || current.Status != repository.VideoStatusFailed {
				t.Fatalf("blocked retry changed the recording: %+v, %v", current, err)
			}
			if err := f.repo.FinalizeDelete(t.Context(), v.ID, repository.DeletionKindRetention); err != nil {
				t.Fatal(err)
			}
			owned.Close()
			if _, err := f.svc.requeueArchive(t.Context(), v, scheduled); !errors.Is(err, repository.ErrNotFound) {
				t.Fatalf("stale retry revived the deleted recording: %v", err)
			}
		})
	}
}
