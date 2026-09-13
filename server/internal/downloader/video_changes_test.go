package downloader

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/hls"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
)

func recvVideoChange(t *testing.T, events <-chan eventbus.VideoChangeEvent) {
	t.Helper()
	select {
	case <-events:
	case <-time.After(2 * time.Second):
		t.Fatal("missing committed video change")
	}
}

func TestVideoChangesFollowCommittedAttemptTransitions(t *testing.T) {
	for _, status := range []string{repository.VideoStatusRunning, repository.VideoStatusDone, repository.VideoStatusFailed} {
		t.Run(status, func(t *testing.T) {
			s := newTestService(t, t.TempDir())
			defer s.Shutdown()
			d := seedWebhookAttempt(t, s, "changed")
			bus := eventbus.New()
			s.SetEventBus(bus)
			events := bus.VideoChanges.Subscribe(t.Context())
			var err error
			switch status {
			case repository.VideoStatusRunning:
				d.executionID = "recovered"
				err = s.claimAttempt(t.Context(), d, "webhook-execution")
			case repository.VideoStatusDone:
				err = s.finishAttempt(t.Context(), d, 10, 100, nil, repository.CompletionKindComplete, false)
			case repository.VideoStatusFailed:
				err = s.markRecordingFailed(t.Context(), d.claim(), "cancelled", repository.CompletionKindCancelled, true)
			}
			if err != nil {
				t.Fatal(err)
			}
			recvVideoChange(t, events)
			v, err := s.repo.GetVideo(t.Context(), d.videoID)
			if err != nil || v.Status != status {
				t.Fatalf("notification preceded committed status %s: %+v, %v", status, v, err)
			}
		})
	}
}

func TestVideoChangeRejectsStaleSettlementWithoutNotification(t *testing.T) {
	s := newTestService(t, t.TempDir())
	defer s.Shutdown()
	d := seedWebhookAttempt(t, s, "stale")
	bus := eventbus.New()
	s.SetEventBus(bus)
	events := bus.VideoChanges.Subscribe(t.Context())
	d.executionID = "stale-owner"
	if err := s.finishAttempt(t.Context(), d, 1, 1, nil, repository.CompletionKindComplete, false); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("stale settlement = %v", err)
	}
	if err := s.markRecordingFailed(t.Context(), d.claim(), "stale", repository.CompletionKindComplete, false); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("stale failure = %v", err)
	}
	select {
	case <-events:
		t.Fatal("published rejected transition")
	default:
	}
}

func TestVideoChangeWaitsForCommitReconciliation(t *testing.T) {
	s := newTestService(t, t.TempDir())
	defer s.Shutdown()
	bus := eventbus.New()
	s.SetEventBus(bus)
	events := bus.VideoChanges.Subscribe(t.Context())
	attempts := 0
	err := s.persistVideoChange(t.Context(), "test commit", func(context.Context) error {
		select {
		case <-events:
			t.Fatal("published before commit was acknowledged")
		default:
		}
		attempts++
		if attempts == 1 {
			return repository.ErrCommitUncertain
		}
		return nil
	})
	if err != nil || attempts != 2 {
		t.Fatalf("reconciliation = %v, attempts=%d", err, attempts)
	}
	recvVideoChange(t, events)
	select {
	case <-events:
		t.Fatal("published more than one acknowledged change")
	default:
	}
}

// Retryable failures bypass terminal settlement. Both the failure and the next
// queue transition must refresh watch pages even when no archive queue is open.
func TestVideoChangesCoverArchiveRetryLifecycle(t *testing.T) {
	for _, action := range []string{"manual retry", "automatic retry", "cancel retry"} {
		t.Run(action, func(t *testing.T) {
			f := newArchiveFixture(t, 1, 1)
			ctx := t.Context()
			jobID, err := f.svc.EnqueueVOD(ctx, vodParams("bc-1", "1"))
			if err != nil {
				t.Fatal(err)
			}
			bus := eventbus.New()
			f.svc.SetEventBus(bus)
			changes := bus.VideoChanges.Subscribe(ctx)
			failArchiveAttempt(t, f, jobID, 1, &hls.FetchError{Kind: hls.FetchKindServer, Status: 503, Permanent: true})
			recvVideoChange(t, changes)
			failed := f.video(t, jobID)
			if failed.Status != repository.VideoStatusFailed || failed.NextRetryAt == nil {
				t.Fatalf("retry failure was not committed: %+v", failed)
			}
			switch action {
			case "manual retry":
				err = f.svc.retryArchiveLocked(ctx, failed.ID)
			case "automatic retry":
				if err := f.repo.MarkArchiveFailedForRetry(ctx, failed.ID, "retry due", repository.CompletionKindComplete, false, time.Now().Add(-time.Minute)); err != nil {
					t.Fatal(err)
				}
				f.svc.requeueDueRetries(ctx)
			case "cancel retry":
				err = f.svc.CancelArchiveRetry(ctx, failed.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			recvVideoChange(t, changes)
			current, err := f.repo.GetVideo(ctx, failed.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantStatus := repository.VideoStatusPending
			if action == "cancel retry" {
				wantStatus = repository.VideoStatusFailed
			}
			if current.Status != wantStatus || current.NextRetryAt != nil {
				t.Fatalf("%s notification preceded committed state: %+v", action, current)
			}
		})
	}
}
