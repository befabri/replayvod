package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/mediastore"

	"github.com/befabri/replayvod/server/internal/testutil/mediatest"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/service/storagehealth"
	"github.com/befabri/replayvod/server/internal/storage"
)

type gateFunc func() error

func (f gateFunc) Ready() error                 { return f() }
func (f gateFunc) Verify(context.Context) error { return f() }

func unattached() error { return fmt.Errorf("%w: marker missing", storage.ErrUnattached) }

func TestStartRefusedWhileStorageUnattached(t *testing.T) {
	s := &Service{
		active: map[string]*download{},
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	setDownloaderGate(t, s, gateFunc(unattached))
	_, err := s.Start(context.Background(), Params{BroadcasterID: "b-1"})
	if !errors.Is(err, ErrStorageUnavailable) || !errors.Is(err, storage.ErrUnattached) {
		t.Fatalf("Start err = %v, want ErrStorageUnavailable wrapping the gate verdict", err)
	}
	if len(s.active) != 0 {
		t.Fatal("a refused start reserved a slot")
	}
	// Read-only storage refuses too: a recording is a write.
	setDownloaderGate(t, s, gateFunc(func() error { return storage.ErrReadOnly }))
	if _, err := s.Start(context.Background(), Params{BroadcasterID: "b-1"}); !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("Start on read-only storage err = %v, want ErrStorageUnavailable", err)
	}
}

type downloaderBlockedRoot struct {
	storage.Identity
	block   atomic.Bool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *downloaderBlockedRoot) ProbeRoot(ctx context.Context) error {
	if s.block.Load() {
		s.once.Do(func() { close(s.entered) })
		<-s.release
	}
	return s.Identity.ProbeRoot(ctx)
}

func TestStartAndProgressDoNotBlockBehindStorageProbe(t *testing.T) {
	s := newTestService(t, t.TempDir())
	store := &downloaderBlockedRoot{Identity: mediatest.Raw(s.storage).(storage.Identity), entered: make(chan struct{}), release: make(chan struct{})}
	mon := storagehealth.New(s.repo, store, nil, s.log, "local", "test", storagehealth.WithProbeTimeout(200*time.Millisecond))
	if _, err := mon.Attach(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(t.Context(), storage.MarkerPath); err != nil {
		t.Fatal(err)
	}
	if mon.Check(t.Context()).State != storagehealth.StateUnattached {
		t.Fatal("missing marker did not mark storage unattached")
	}
	setDownloaderGate(t, s, mon)
	store.block.Store(true)
	done := make(chan struct{})
	go func() { mon.Check(t.Context()); close(done) }()
	t.Cleanup(func() {
		close(store.release)
		<-done
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = mon.Verify(ctx)
	})
	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	started := make(chan error, 1)
	go func() {
		_, err := s.Start(t.Context(), Params{BroadcasterID: "b-1"})
		started <- err
	}()
	select {
	case err := <-started:
		if !errors.Is(err, ErrStorageUnavailable) {
			t.Fatalf("Start = %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Start waited for the storage probe while holding the downloader lock")
	}
	progress := make(chan []Progress, 1)
	go func() { progress <- s.ListActiveProgress() }()
	select {
	case got := <-progress:
		if len(got) != 0 {
			t.Fatal("refused Start reserved a recording slot")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("progress request waited for the downloader lock")
	}
}

func TestRestartJobRefusedWhileStorageUnattached(t *testing.T) {
	s := &Service{
		active: map[string]*download{},
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	setDownloaderGate(t, s, gateFunc(unattached))
	if err := s.restartJob(context.Background(), &repository.Job{ID: "job-1"}); !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("restartJob err = %v, want ErrStorageUnavailable", err)
	}
}

// TestResumeLeavesJobsRunningWhileStorageUnattached pins the boot-with-volume-
// missing case: an in-flight job must not be failed for a disk that is not
// there, so it stays RUNNING for the resume that follows the attach.
func TestResumeLeavesJobsRunningWhileStorageUnattached(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t, t.TempDir())
	if _, err := s.repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "b-1", BroadcasterLogin: "b1", BroadcasterName: "B1"}); err != nil {
		t.Fatal(err)
	}
	v, err := s.repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-1", Filename: "rec", DisplayName: "B1", Status: repository.VideoStatusRunning,
		Quality: repository.QualityHigh, BroadcasterID: "b-1", RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatal(err)
	}
	state, _ := NewResumeState().MarshalJSON()
	if _, err := s.repo.CreateJob(ctx, &repository.JobInput{ID: "job-1", VideoID: v.ID, BroadcasterID: "b-1", ResumeState: state}); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.SetJobExecution(ctx, "job-1", "", false); err != nil {
		t.Fatal(err)
	}
	setDownloaderGate(t, s, gateFunc(unattached))
	if err := s.Resume(ctx); err != nil {
		t.Fatalf("Resume with unattached storage = %v, want nil", err)
	}
	job, err := s.repo.GetJob(ctx, "job-1")
	if err != nil || job.Status != repository.JobStatusRunning {
		t.Fatalf("job after deferred resume = %+v, %v; want RUNNING", job, err)
	}
	if len(s.active) != 0 {
		t.Fatal("a deferred resume started the job anyway")
	}
}

func TestArchivePumpPausesUntilStorageAttached(t *testing.T) {
	f := newArchiveFixture(t, 2, 1)
	release := f.edge.hold()
	defer release()
	ctx := context.Background()
	setDownloaderGate(t, f.svc, gateFunc(unattached))

	jobID, err := enqueueAndPump(f.svc, ctx, vodParams("bc-1", "1001"))
	if err != nil {
		t.Fatalf("EnqueueVOD: %v", err)
	}
	if f.activeJobs() != 0 || f.status(t, jobID) != repository.VideoStatusPending {
		t.Fatalf("archive started while storage was unattached: active=%d status=%s", f.activeJobs(), f.status(t, jobID))
	}

	setDownloaderGate(t, f.svc, gateFunc(func() error { return nil }))
	f.svc.PumpArchiveQueue(ctx)
	waitUntil(t, "job to go active once storage attached", func() bool { return f.activeJobs() == 1 })
}

func setDownloaderGate(t *testing.T, s *Service, gate mediastore.Gate) {
	t.Helper()
	if s.storage == nil {
		s.storage = mediatest.New(t, s.repo, nil, gate, nil)
		return
	}
	mediatest.SetGate(s.storage, gate)
}

func TestDeferredLiveRecoveryPollsWithoutStorageEvents(t *testing.T) {
	s := newTestService(t, t.TempDir())
	defer s.Shutdown()
	s.retryInterval = 20 * time.Millisecond
	d := seedWebhookAttempt(t, s, "deferred-without-event")
	at := time.Now().UTC()
	d.resume.SetStage(StagePrepareInput)
	d.resume.CaptureStoppedAt = &at
	checkpoint, err := d.resume.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.repo.CheckpointAttempt(t.Context(), d.jobID, d.executionID, checkpoint); err != nil {
		t.Fatal(err)
	}
	var writable atomic.Bool
	setDownloaderGate(t, s, gateFunc(func() error {
		if !writable.Load() {
			return storage.ErrUnattached
		}
		return nil
	}))
	if err := s.Resume(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := s.work.Used("live"); got != 0 {
		t.Fatalf("unattached recording started: %d", got)
	}
	// No bus is installed and no second Resume is called. The durable recovery
	// poll must claim the existing live job after the storage fault clears.
	writable.Store(true)
	waitUntil(t, "live recovery without attach event", func() bool {
		job, err := s.repo.GetJob(t.Context(), d.jobID)
		return err == nil && job.ExecutionID != d.executionID
	})
}
