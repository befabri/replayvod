package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	provider "github.com/befabri/replayvod/server/internal/twitch"
)

func TestStopSurvivesFailedSettlementAndRestart(t *testing.T) {
	s := newTestService(t, t.TempDir())
	d := seedWebhookAttempt(t, s, "durable-stop")
	scratch := filepath.Join(s.cfg.Env.ScratchDir, d.jobID, "part01", "segments", "saved.ts")
	if err := os.MkdirAll(filepath.Dir(scratch), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scratch, []byte("saved capture"), 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	d.runCtx, d.cancel = ctx, cancel
	s.active[d.jobID] = d
	if err := s.Cancel(d.jobID); err != nil {
		t.Fatal(err)
	}
	// A checkpoint that was already in flight cannot erase the durable stop.
	// It is deliberately unreadable by ResumeState: cancellation must not need
	// a provider connection, storage, or a usable capture checkpoint to settle.
	if err := s.repo.CheckpointAttempt(t.Context(), d.jobID, d.executionID, json.RawMessage(`{"current_part_index":"broken"}`)); err != nil {
		t.Fatal(err)
	}
	repo := s.repo
	s.repo = &terminalFaultRepo{Repository: repo, fail: "job"}
	d.persistenceErr = context.Canceled
	s.failDownload(t.Context(), d, s.log, context.Canceled)
	job, err := repo.GetJob(t.Context(), d.jobID)
	if err != nil || !job.StopRequested || job.Status != repository.JobStatusRunning || d.cleanupScratch {
		t.Fatalf("uncommitted cancellation lost recovery: %+v, cleanup=%v, %v", job, d.cleanupScratch, err)
	}
	next := NewService(s.cfg, repo, s.storage, nil, nil, nil, discardLog())
	defer next.Shutdown()
	if err := next.PrepareScratch(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(scratch); err != nil {
		t.Fatalf("uncommitted stop lost saved capture: %v", err)
	}
	if err := next.resumeRunning(t.Context()); err != nil {
		t.Fatal(err)
	}
	job, err = repo.GetJob(t.Context(), d.jobID)
	video, videoErr := repo.GetVideo(t.Context(), d.videoID)
	if err != nil || videoErr != nil || job.Status != repository.JobStatusFailed || video.CompletionKind != repository.CompletionKindCancelled || next.work.Used("live") != 0 {
		t.Fatalf("restart did not settle Stop: %+v, %+v, %v, %v", job, video, err, videoErr)
	}
	if _, err := os.Stat(filepath.Join(s.cfg.Env.ScratchDir, d.jobID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("settled stop left scratch until another restart: %v", err)
	}
}

func TestQueuedStopSettlementBypassesCaptureGates(t *testing.T) {
	for _, gate := range []string{"archive capacity", "storage unavailable"} {
		for _, recovery := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/recovery=%v", gate, recovery), func(t *testing.T) {
				f := newArchiveFixture(t, 1, 1)
				// An older executable request must not hide the stopped member.
				if _, err := f.svc.EnqueueVOD(t.Context(), vodParams("bc-1", "older")); err != nil {
					t.Fatal(err)
				}
				jobID, err := f.svc.EnqueueVOD(t.Context(), vodParams("bc-2", "stopped"))
				if err != nil {
					t.Fatal(err)
				}
				if gate == "archive capacity" {
					reservation, err := f.svc.work.Reserve("archive", "occupied")
					if err != nil {
						t.Fatal(err)
					}
					defer reservation.Release()
				} else {
					setDownloaderGate(t, f.svc, gateFunc(func() error { return storage.ErrUnattached }))
				}
				if recovery {
					err = repository.RequestAttemptStop(t.Context(), f.repo, jobID)
				} else {
					err = f.svc.Cancel(jobID)
				}
				if err != nil {
					t.Fatal(err)
				}
				f.svc.PumpArchiveQueue(t.Context())
				job, err := f.repo.GetJob(t.Context(), jobID)
				v := f.video(t, jobID)
				if err != nil || job.Status != repository.JobStatusFailed || v.CompletionKind != repository.CompletionKindCancelled {
					t.Fatalf("capture gate blocked terminal settlement: job=%+v video=%+v err=%v", job, v, err)
				}
				if f.edge.lastGQL() != nil {
					t.Fatal("stop settlement acquired playback")
				}
			})
		}
	}
}

func TestStopAcknowledgesOnlyPersistedJobRequest(t *testing.T) {
	s := newTestService(t, t.TempDir())
	d := seedWebhookAttempt(t, s, "refused-stop")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d.cancel = cancel
	s.active[d.jobID] = d
	repo := s.repo
	s.repo = &terminalFaultRepo{Repository: repo, fail: "commit"}
	if err := s.Cancel(d.jobID); !errors.Is(err, errTerminalPersistence) {
		t.Fatalf("failed Stop acknowledged: %v", err)
	}
	job, err := repo.GetJob(t.Context(), d.jobID)
	if err != nil || job.StopRequested || ctx.Err() != nil || d.userCancelled {
		t.Fatalf("rejected Stop changed execution: %+v, %v", job, err)
	}
}

func TestDurableStopWinsBeforeInMemoryCancellationArrives(t *testing.T) {
	for _, phase := range []string{"completion", "failure", "archive retry"} {
		t.Run(phase, func(t *testing.T) {
			s := newTestService(t, t.TempDir())
			d := seedWebhookAttempt(t, s, "stop-before-signal")
			// Stop is committed, but this execution has not received its local
			// cancellation signal yet. Transaction guards must decide the outcome.
			if err := repository.RequestAttemptStop(t.Context(), s.repo, d.jobID); err != nil {
				t.Fatal(err)
			}
			var cause error = errors.New("capture failed")
			if phase == "completion" {
				cause = s.finishAttempt(t.Context(), d, 1, 1, nil, repository.CompletionKindComplete, false)
				if !errors.Is(cause, repository.ErrStopRequested) {
					t.Fatalf("completed stopped attempt: %v", cause)
				}
			} else if phase == "archive retry" {
				d.vod, d.attempt, cause = true, 1, fakeNetError{}
			}
			s.failDownload(t.Context(), d, s.log, cause)
			v, err := s.repo.GetVideo(t.Context(), d.videoID)
			if err != nil || v.Status != repository.VideoStatusFailed || v.CompletionKind != repository.CompletionKindCancelled || v.NextRetryAt != nil {
				t.Fatalf("stop lost to %s: %+v, %v", phase, v, err)
			}
		})
	}
}

func TestStopQueuedArchiveDoesNotAcquirePlayback(t *testing.T) {
	for _, discovery := range []string{"after Stop", "before Stop"} {
		t.Run(discovery, func(t *testing.T) {
			f := newArchiveFixture(t, 1, 1)
			jobID, err := f.svc.EnqueueVOD(t.Context(), vodParams("bc-1", "queued-stop"))
			if err != nil {
				t.Fatal(err)
			}
			before, err := f.repo.GetJob(t.Context(), jobID)
			if err != nil {
				t.Fatal(err)
			}
			if discovery == "before Stop" {
				if err := repository.RequestAttemptStop(t.Context(), f.repo, jobID); err != nil {
					t.Fatal(err)
				}
				// Discovery read an executable job before Stop committed. The
				// claim must observe Stop and settle without acquiring playback.
				if err := f.svc.restartJob(t.Context(), before); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := f.svc.Cancel(jobID); err != nil {
					t.Fatal(err)
				}
				f.svc.PumpArchiveQueue(t.Context())
			}
			waitUntil(t, "stopped archive settlement", func() bool {
				job, err := f.repo.GetJob(t.Context(), jobID)
				return err == nil && job.Status == repository.JobStatusFailed
			})
			v := f.video(t, jobID)
			if v.CompletionKind != repository.CompletionKindCancelled || f.edge.lastGQL() != nil {
				t.Fatalf("stopped queued archive acquired playback: %+v", v)
			}
		})
	}
}

func seedStoppedScratch(t *testing.T, s *Service) (*repository.Job, string) {
	t.Helper()
	d := seedWebhookAttempt(t, s, "stopped-scratch")
	if err := repository.RequestAttemptStop(t.Context(), s.repo, d.jobID); err != nil {
		t.Fatal(err)
	}
	job, err := s.repo.GetJob(t.Context(), d.jobID)
	if err != nil {
		t.Fatal(err)
	}
	saved := filepath.Join(s.cfg.Env.ScratchDir, job.ID, "saved.ts")
	if err := os.MkdirAll(filepath.Dir(saved), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(saved, []byte("saved media"), 0600); err != nil {
		t.Fatal(err)
	}
	return job, saved
}

func TestStoppedRecoveryRetainsScratchOnRejectedSettlement(t *testing.T) {
	for _, failure := range []string{"job", "commit", "stale execution"} {
		t.Run(failure, func(t *testing.T) {
			s := newTestService(t, t.TempDir())
			defer s.Shutdown()
			job, saved := seedStoppedScratch(t, s)
			repo, claim := s.repo, *job
			if failure == "stale execution" {
				claim.ExecutionID = "obsolete"
			} else {
				s.repo = &terminalFaultRepo{Repository: repo, fail: failure}
			}
			if err := s.settleUnownedAttempt(t.Context(), &claim, ErrCancelled, true); err == nil {
				t.Fatal("injected settlement failure was accepted")
			}
			row, err := repo.GetJob(t.Context(), job.ID)
			if err != nil || row.Status != repository.JobStatusRunning {
				t.Fatalf("rejected settlement changed job: %+v, %v", row, err)
			}
			if _, err := os.Stat(saved); err != nil {
				t.Fatalf("uncommitted or stale settlement removed scratch: %v", err)
			}
			s.repo = repo
			setDownloaderGate(t, s, gateFunc(func() error { return storage.ErrUnattached }))
			if err := s.settleStoppedJobs(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Dir(saved)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("committed cancellation retained scratch: %v", err)
			}
		})
	}
}

type lostCancellationCommit struct {
	repository.Repository
	lost bool
}

func (r *lostCancellationCommit) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	err := r.Repository.WithTx(ctx, fn)
	if err == nil && !r.lost {
		r.lost = true
		return repository.ErrCommitUncertain
	}
	return err
}

func TestStoppedRecoveryConfirmsCommitBeforeReleasingScratch(t *testing.T) {
	s := newTestService(t, t.TempDir())
	defer s.Shutdown()
	job, saved := seedStoppedScratch(t, s)
	s.repo = &lostCancellationCommit{Repository: s.repo}
	if err := s.settleUnownedAttempt(t.Context(), job, ErrCancelled, true); err != nil {
		t.Fatal(err)
	}
	row, err := s.repo.GetJob(t.Context(), job.ID)
	if err != nil || row.Status != repository.JobStatusFailed {
		t.Fatalf("terminal commit not confirmed: %+v, %v", row, err)
	}
	if _, err := os.Stat(filepath.Dir(saved)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lost acknowledgement leaked scratch: %v", err)
	}
}

func TestStoppedRecoveryLeavesActiveWriterInCharge(t *testing.T) {
	s := newTestService(t, t.TempDir())
	defer s.Shutdown()
	job, saved := seedStoppedScratch(t, s)
	reservation, err := s.work.Reserve("live", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer reservation.Release()
	workspace, err := s.storage.Scratch().Open(filepath.Dir(saved), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close(false)
	// The runner key is already owned, even before the active map is filled.
	if err := s.Cancel(job.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.settleStoppedJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	row, err := s.repo.GetJob(t.Context(), job.ID)
	if err != nil || row.Status != repository.JobStatusRunning {
		t.Fatalf("discovery settled another writer: %+v, %v", row, err)
	}
	if _, err := os.Stat(saved); err != nil {
		t.Fatalf("discovery removed active scratch: %v", err)
	}
	if err := workspace.Close(false); err != nil {
		t.Fatal(err)
	}
	reservation.Release()
	if err := s.settleStoppedJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(saved)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("abandoned scratch survived recovery: %v", err)
	}
}

func TestStopAfterRecoveryClaimCleansUnopenedScratch(t *testing.T) {
	s := newTestService(t, t.TempDir())
	defer s.Shutdown()
	d := seedWebhookAttempt(t, s, "stop-after-claim")
	checkpoint, err := d.resume.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.repo.CheckpointAttempt(t.Context(), d.jobID, d.executionID, checkpoint); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.cfg.Env.ScratchDir, d.jobID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "saved.ts"), []byte("saved media"), 0600); err != nil {
		t.Fatal(err)
	}
	job, err := s.repo.GetJob(t.Context(), d.jobID)
	if err != nil {
		t.Fatal(err)
	}
	var stopped atomic.Bool
	stopResult := make(chan error, 1)
	s.repo = &archiveFaultRepo{Repository: s.repo, afterClaim: func() {
		if stopped.CompareAndSwap(false, true) {
			stopResult <- s.Cancel(d.jobID)
		}
	}}
	if err := s.restartJob(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-stopResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("recovery never reached claim")
	}
	waitCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := s.work.WaitIdle(waitCtx); err != nil {
		t.Fatal(err)
	}
	v, err := s.repo.GetVideo(t.Context(), job.VideoID)
	if err != nil || v.Status != repository.VideoStatusFailed || v.CompletionKind != repository.CompletionKindCancelled {
		t.Fatalf("claimed stop did not settle: %+v, %v", v, err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stop before workspace acquisition leaked saved media: %v", err)
	}
}

func TestAttemptPanicSettlesBeforeReleasingScratch(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(fmt.Sprintf("manual=%v", manual), func(t *testing.T) {
			f := newArchiveFixture(t, 1, 1)
			if manual {
				f.svc.cfg.App.Download.StreamerRestartWaitSeconds = 120
			}
			f.svc.observe = func(context.Context, string) (*provider.Stream, error) {
				panic("provider failed during acquisition")
			}
			streamID := "broadcast"
			id, err := f.svc.Start(t.Context(), Params{BroadcasterID: "bc-1", BroadcasterLogin: "bc-1", StreamID: &streamID, Quality: repository.QualityHigh})
			if err != nil {
				t.Fatal(err)
			}
			waitCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if err := f.svc.work.WaitIdle(waitCtx); err != nil {
				t.Fatal(err)
			}
			if v := f.video(t, id); v.Status != repository.VideoStatusFailed {
				t.Fatalf("panic did not reach terminal settlement: %+v", v)
			}
			if _, err := os.Stat(filepath.Join(f.svc.cfg.Env.ScratchDir, id)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("panic released scratch before terminal cleanup: %v", err)
			}
		})
	}
}

func TestUserStopWinsDeferralButCannotSettleReplacement(t *testing.T) {
	for _, reason := range []error{context.Canceled, storage.ErrFull, errExecutionDeferred, repository.ErrStaleExecution} {
		t.Run(reason.Error(), func(t *testing.T) {
			s := newTestService(t, t.TempDir())
			d := seedWebhookAttempt(t, s, "stopped-owner")
			ctx, cancel := context.WithCancelCause(t.Context())
			cancel(reason)
			d.runCtx, d.userCancelled, d.persistenceErr = ctx, true, reason
			s.shuttingDown.Store(true)
			stale := errors.Is(reason, repository.ErrStaleExecution)
			if stale {
				replacement := d.claim()
				replacement.ExecutionID = "replacement"
				if err := repository.ClaimAttempt(t.Context(), s.repo, replacement, d.executionID); err != nil {
					t.Fatal(err)
				}
			}
			s.failDownload(t.Context(), d, s.log, reason)
			job, err := s.repo.GetJob(t.Context(), d.jobID)
			want := repository.JobStatusFailed
			if stale {
				want = repository.JobStatusRunning
			}
			if err != nil || job.Status != want || d.cleanupScratch == stale {
				t.Fatalf("Stop/ownership precedence: %+v, cleanup=%v, %v", job, d.cleanupScratch, err)
			}
		})
	}
}
