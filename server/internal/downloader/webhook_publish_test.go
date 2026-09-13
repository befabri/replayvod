package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/recordingwebhook"
	"github.com/befabri/replayvod/server/internal/repository"
)

func recvTerminal(t *testing.T, ch <-chan eventbus.RecordingTerminalEvent) eventbus.RecordingTerminalEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("expected a RecordingTerminal event, got none")
		return eventbus.RecordingTerminalEvent{}
	}
}

func assertNoTerminal(t *testing.T, ch <-chan eventbus.RecordingTerminalEvent) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Fatalf("expected no RecordingTerminal event, got %+v", ev)
	case <-time.After(200 * time.Millisecond):
	}
}

func subscribedService(t *testing.T) (*Service, <-chan eventbus.RecordingTerminalEvent) {
	t.Helper()
	s := newTestService(t, t.TempDir())
	bus := eventbus.New()
	s.SetEventBus(bus)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return s, bus.RecordingTerminal.Subscribe(ctx)
}

func TestRecordingWebhookDelivery_CompletedEnqueuesDurableRow(t *testing.T) {
	s := newTestService(t, t.TempDir())
	ctx := context.Background()
	if _, err := s.repo.UpsertRecordingWebhookConfig(ctx, true, "https://hooks.example/x", "recording.completed"); err != nil {
		t.Fatalf("UpsertRecordingWebhookConfig: %v", err)
	}
	if err := s.repo.EnsureRecordingWebhookSecret(ctx, "secret"); err != nil {
		t.Fatalf("EnsureRecordingWebhookSecret: %v", err)
	}

	delivery := s.recordingWebhookDelivery(99, recordingwebhook.EventCompleted)
	if delivery == nil {
		t.Fatal("recordingWebhookDelivery returned nil; a terminal event must always enqueue a row")
	}
	if err := s.repo.MarkVideoDoneAndEnqueueRecordingWebhook(ctx, 99, 12.5, 4096, nil, repository.CompletionKindComplete, false, delivery); err != nil {
		t.Fatalf("MarkVideoDoneAndEnqueueRecordingWebhook: %v", err)
	}

	rows, err := s.repo.ListRecordingWebhookDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecordingWebhookDeliveries: %v", err)
	}
	if len(rows) != 1 || rows[0].Event != recordingwebhook.EventCompleted || rows[0].VideoID != 99 {
		t.Fatalf("terminal completion should enqueue one recording.completed row, got %+v", rows)
	}
	if rows[0].MessageID == "" {
		t.Error("enqueued row has an empty message id")
	}
}

func TestFailDownload_publishesFailedOnRealFailure(t *testing.T) {
	s, ch := subscribedService(t)
	if _, err := s.repo.UpsertRecordingWebhookConfig(context.Background(), true, "https://hooks.example/x", "recording.failed"); err != nil {
		t.Fatalf("UpsertRecordingWebhookConfig: %v", err)
	}
	if err := s.repo.EnsureRecordingWebhookSecret(context.Background(), "secret"); err != nil {
		t.Fatalf("EnsureRecordingWebhookSecret: %v", err)
	}
	d := seedWebhookAttempt(t, s, "job-1")

	s.failDownload(context.Background(), d, discardLog(), errors.New("boom"))

	ev := recvTerminal(t, ch)
	if ev.Kind != eventbus.RecordingFailed || ev.VideoID != 1 {
		t.Fatalf("got %+v, want failed for video 1", ev)
	}
	rows, err := s.repo.ListRecordingWebhookDeliveries(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListRecordingWebhookDeliveries: %v", err)
	}
	if len(rows) != 1 || rows[0].Event != "recording.failed" || rows[0].VideoID != 1 {
		t.Fatalf("terminal failure should enqueue durable webhook, got %+v", rows)
	}
}

func TestFailDownload_doesNotPublishOnShutdownInterrupt(t *testing.T) {
	s, ch := subscribedService(t)
	// A shutdown interrupt (not user-cancelled) leaves the job RUNNING for
	// resume — it is NOT a terminal failure and must not fire the webhook.
	s.shuttingDown.Store(true)
	d := seedWebhookAttempt(t, s, "job-2")

	s.failDownload(context.Background(), d, discardLog(), context.Canceled)

	assertNoTerminal(t, ch)
}

func TestFailDownload_userCancelDuringShutdownStillFires(t *testing.T) {
	s, ch := subscribedService(t)
	// The shutdown suppression applies only when the user did NOT cancel. An
	// operator cancel is a real terminal FAILED transition even mid-shutdown.
	s.shuttingDown.Store(true)
	d := seedWebhookAttempt(t, s, "job-3")
	d.userCancelled = true

	s.failDownload(context.Background(), d, discardLog(), ErrCancelled)

	ev := recvTerminal(t, ch)
	if ev.Kind != eventbus.RecordingFailed || ev.VideoID != d.videoID {
		t.Fatalf("got %+v, want failed for video 3", ev)
	}
}

func TestPublishRecordingTerminal_completed(t *testing.T) {
	s, ch := subscribedService(t)
	s.publishRecordingTerminal(7, eventbus.RecordingCompleted)
	ev := recvTerminal(t, ch)
	if ev.Kind != eventbus.RecordingCompleted || ev.VideoID != 7 {
		t.Fatalf("got %+v, want completed for video 7", ev)
	}
}

func TestPublishRecordingTerminal_nilBusIsSafe(t *testing.T) {
	s := newTestService(t, t.TempDir()) // SetEventBus never called
	// Must be a no-op, not a panic — tests and bus-less deployments rely on it.
	s.publishRecordingTerminal(9, eventbus.RecordingCompleted)
}

// TestResume_UnresumableRunningJobFailsAndEnqueuesWebhook covers Resume's crash-
// recovery branch: a job left RUNNING by a prior process that can't be restarted
// (here an unparseable resume_state) is a terminal FAILED transition. It must
// mark the video FAILED, enqueue a durable recording.failed outbox row in the
// same transaction, and fire one RecordingFailed wake-up — exactly what a
// recording.failed consumer expects when a recording dies across a restart.
func TestResume_UnresumableRunningJobFailsAndEnqueuesWebhook(t *testing.T) {
	s, ch := subscribedService(t)
	ctx := context.Background()

	if _, err := s.repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID:    "b1",
		BroadcasterLogin: "streamer",
		BroadcasterName:  "Streamer",
	}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	vid, err := s.repo.CreateVideo(ctx, &repository.VideoInput{
		JobID:         "job-resume",
		Filename:      "rec",
		DisplayName:   "Streamer",
		Status:        repository.VideoStatusRunning,
		Quality:       repository.QualityHigh,
		BroadcasterID: "b1",
		Language:      "en",
		RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("CreateVideo: %v", err)
	}
	if _, err := s.repo.CreateJob(ctx, &repository.JobInput{
		ID:            "job-resume",
		VideoID:       vid.ID,
		BroadcasterID: "b1",
		// Unparseable resume_state: restartJob fails at UnmarshalResumeState,
		// before reserving a slot or spawning run(), so no ffmpeg is needed.
		ResumeState: json.RawMessage(`{"corrupt":`),
	}); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if err := s.repo.SetJobExecution(ctx, "job-resume", "", false); err != nil {
		t.Fatalf("SetJobExecution: %v", err)
	}
	if _, err := s.repo.UpsertRecordingWebhookConfig(ctx, true, "https://hooks.example/x", "recording.failed"); err != nil {
		t.Fatalf("UpsertRecordingWebhookConfig: %v", err)
	}
	if err := s.repo.EnsureRecordingWebhookSecret(ctx, "secret"); err != nil {
		t.Fatalf("EnsureRecordingWebhookSecret: %v", err)
	}

	if err := s.Resume(ctx); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	got, err := s.repo.GetVideo(ctx, vid.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.Status != repository.VideoStatusFailed {
		t.Errorf("video status = %q, want %q", got.Status, repository.VideoStatusFailed)
	}
	rows, err := s.repo.ListRecordingWebhookDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecordingWebhookDeliveries: %v", err)
	}
	if len(rows) != 1 || rows[0].Event != recordingwebhook.EventFailed || rows[0].VideoID != vid.ID {
		t.Fatalf("unresumable job should enqueue one recording.failed row, got %+v", rows)
	}
	ev := recvTerminal(t, ch)
	if ev.Kind != eventbus.RecordingFailed || ev.VideoID != vid.ID {
		t.Fatalf("got %+v, want failed for video %d", ev, vid.ID)
	}
}

func seedWebhookAttempt(t *testing.T, s *Service, jobID string) *download {
	t.Helper()
	ctx := t.Context()
	if _, err := s.repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "webhook", BroadcasterLogin: "webhook", BroadcasterName: "Webhook"}); err != nil {
		t.Fatal(err)
	}
	v, err := repository.CreateAttempt(ctx, s.repo, &repository.VideoInput{JobID: jobID, Filename: jobID, BroadcasterID: "webhook", DisplayName: "Webhook", Status: repository.VideoStatusPending, Quality: repository.QualityHigh}, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	d := &download{jobID: jobID, videoID: v.ID, executionID: "webhook-execution", resume: NewResumeState()}
	if err := repository.ClaimAttempt(ctx, s.repo, d.claim(), ""); err != nil {
		t.Fatal(err)
	}
	return d
}
