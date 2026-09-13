package downloader

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// manualAdmissionReadFailure lets a subscriber attach before the initial intent read fails.
type manualAdmissionReadFailure struct {
	repository.Repository
	once             sync.Once
	entered, release chan struct{}
	panicOnRead      bool
}

func (r *manualAdmissionReadFailure) GetRecordingIntent(ctx context.Context, id string) (*repository.RecordingIntent, error) {
	initial := false
	r.once.Do(func() {
		initial = true
		close(r.entered)
		select {
		case <-r.release:
		case <-ctx.Done():
		}
	})
	if initial {
		if r.panicOnRead {
			panic("initial manual intent read panicked")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("temporary initial manual intent read failure")
	}
	return r.Repository.GetRecordingIntent(ctx, id)
}

func TestManualAdmissionFailureClosesProgressAndPreservesRecovery(t *testing.T) {
	for _, failure := range []string{"read error", "read panic", "shutdown during read"} {
		t.Run(failure, func(t *testing.T) {
			f := newArchiveFixture(t, 1, 1)
			s := f.svc
			s.cfg.App.Download.StreamerRestartWaitSeconds = 120
			barrier := &manualAdmissionReadFailure{
				Repository: f.repo, entered: make(chan struct{}), release: make(chan struct{}),
				panicOnRead: failure == "read panic",
			}
			s.repo = barrier
			var release sync.Once
			t.Cleanup(func() { release.Do(func() { close(barrier.release) }) })
			// Recovery must stay in capture long enough to observe its new
			// progress stream and cancel it through the real intent owner.
			releasePlayback := f.edge.hold()
			t.Cleanup(releasePlayback)
			jobID, err := s.Start(t.Context(), Params{BroadcasterID: "bc-1", BroadcasterLogin: "bc-1", Quality: repository.QualityHigh})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-barrier.entered:
			case <-time.After(3 * time.Second):
				t.Fatal("manual owner did not reach its initial intent read")
			}
			progress := s.Subscribe(jobID)
			if progress == nil {
				t.Fatal("accepted recording has no progress stream")
			}
			if failure == "shutdown during read" {
				s.Shutdown()
			} else {
				release.Do(func() { close(barrier.release) })
			}
			waitCtx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			if err := s.work.WaitIdle(waitCtx); err != nil {
				t.Fatal(err)
			}
			if s.Subscribe(jobID) != nil {
				t.Fatal("failed admission still registered as active")
			}
			assertManualProgressClosed(t, "failed admission", progress)
			job, err := f.repo.GetJob(t.Context(), jobID)
			if err != nil || job.Status != repository.JobStatusPending || job.ExecutionID != "" {
				t.Fatalf("unstarted recording lost its recoverable admission: %+v, %v", job, err)
			}
			intent, err := f.repo.GetRecordingIntent(t.Context(), jobID)
			if err != nil || intent.Status != "active" || intent.StopRequested {
				t.Fatalf("admission failure stopped the manual intent: %+v, %v", intent, err)
			}
			if f.edge.lastGQL() != nil {
				t.Fatal("failed admission started playback acquisition")
			}
			if failure == "shutdown during read" {
				return
			}

			if err := s.resumeManualIntents(t.Context()); err != nil {
				t.Fatal(err)
			}
			waitUntil(t, "recovered manual capture", func() bool { return f.edge.lastGQL() != nil })
			recovered := s.Subscribe(jobID)
			if recovered == nil || recovered == progress {
				t.Fatal("recovery did not create a new progress stream")
			}
			select {
			case p, ok := <-recovered:
				if !ok || p.JobID != jobID {
					t.Fatalf("recovery lost progress ownership: %+v, open=%v", p, ok)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("recovered capture did not publish progress")
			}
			if s.work.Used("live") != 1 {
				t.Fatal("recovered capture released its reservation before settlement")
			}
			if err := s.Cancel(jobID); err != nil {
				t.Fatal(err)
			}
			waitCtx, cancel = context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			if err := s.work.WaitIdle(waitCtx); err != nil {
				t.Fatal(err)
			}
			assertManualProgressClosed(t, "cancelled recovery", recovered)
			if v := f.video(t, jobID); v.Status != repository.VideoStatusFailed || v.CompletionKind != repository.CompletionKindCancelled {
				t.Fatalf("recovered capture did not settle cancellation: %+v", v)
			}
		})
	}
}

// assertManualProgressClosed requires runner idle, after every progress writer has exited.
func assertManualProgressClosed(t *testing.T, phase string, progress <-chan Progress) {
	t.Helper()
	for {
		select {
		case _, ok := <-progress:
			if !ok {
				return
			}
		default:
			t.Errorf("%s released runner ownership but left its progress stream open", phase)
			return
		}
	}
}
