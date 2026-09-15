package contracttest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// holdLock runs lock inside a transaction on repo and closes locked once it
// returns. The transaction then blocks until release is closed, runs then, and
// commits unless then fails.
func holdLock(ctx context.Context, repo repository.Repository, lock, then func(tx repository.Repository) error) (locked chan struct{}, release chan struct{}, finished chan error) {
	locked, release, finished = make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		finished <- repo.WithTx(ctx, func(tx repository.Repository) error {
			if err := lock(tx); err != nil {
				return err
			}
			close(locked)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			return then(tx)
		})
	}()
	return locked, release, finished
}

// awaitLock fails the test unless the holder started by holdLock reports the
// lock before its transaction ends.
func awaitLock(t *testing.T, ctx context.Context, locked chan struct{}, finished chan error) {
	t.Helper()
	select {
	case <-locked:
	case err := <-finished:
		t.Fatalf("lock failed: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

// assertBlocked fails the test if the concurrent writer returns while the
// holder still owns the lock.
func assertBlocked(t *testing.T, what string, writer chan error, release chan struct{}, finished chan error) {
	t.Helper()
	select {
	case err := <-writer:
		close(release)
		<-finished
		t.Fatalf("%s escaped the lock: %v", what, err)
	case <-time.After(100 * time.Millisecond):
	}
}

func testRowLocksRequireTransaction(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "execution-channel")
	locks := map[string]func(repository.Repository) error{
		"GetVideoForUpdate":   func(r repository.Repository) error { _, err := r.GetVideoForUpdate(ctx, 404); return err },
		"LockRecordingIntent": func(r repository.Repository) error { _, err := r.LockRecordingIntent(ctx, "missing"); return err },
		"GetUserForUpdate":    func(r repository.Repository) error { _, err := r.GetUserForUpdate(ctx, "missing"); return err },
	}
	for name, lock := range locks {
		if err := lock(repo); !errors.Is(err, repository.ErrNoTransaction) {
			t.Errorf("%s outside a transaction: %v", name, err)
		}
		err := repo.WithTx(ctx, func(tx repository.Repository) error { return lock(tx) })
		if !errors.Is(err, repository.ErrNotFound) {
			t.Errorf("%s inside a transaction: %v", name, err)
		}
	}
}

func testVideoLockSerializesStopAndClaim(t *testing.T, h Harness) {
	repo := h.Repo()
	writerRepo := h.ConcurrentRepo(t)
	SeedUserChannel(t, t.Context(), repo, "owner", "execution-channel")
	if err := repo.WithTx(t.Context(), func(tx repository.Repository) error {
		_, err := tx.GetVideoForUpdate(t.Context(), 404)
		return err
	}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing video lock: %v", err)
	}
	abort := errors.New("roll back")
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollback=%t", rollback), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			id := fmt.Sprintf("video-lock-%t", rollback)
			v, err := repository.CreateAttempt(ctx, repo, executionInput(id), json.RawMessage(`{}`))
			if err != nil {
				t.Fatal(err)
			}
			locked, release, finished := holdLock(ctx, repo, func(tx repository.Repository) error {
				got, err := tx.GetVideoForUpdate(ctx, v.ID)
				if err != nil || got.ID != v.ID || got.JobID != id || got.Status != repository.VideoStatusPending {
					return fmt.Errorf("locked video: %+v, %v", got, err)
				}
				return nil
			}, func(tx repository.Repository) error {
				job, err := tx.GetJob(ctx, id)
				if err != nil || job.StopRequested {
					return fmt.Errorf("stop escaped video lock: %+v, %v", job, err)
				}
				if err := tx.SetJobExecution(ctx, id, "holder", true); err != nil {
					return err
				}
				if err := tx.UpdateVideoStatus(ctx, v.ID, repository.VideoStatusRunning); err != nil {
					return err
				}
				if rollback {
					return abort
				}
				return nil
			})
			awaitLock(t, ctx, locked, finished)
			stopper := make(chan error, 1)
			go func() { stopper <- repository.RequestAttemptStop(ctx, writerRepo, id) }()
			assertBlocked(t, "stop", stopper, release, finished)
			close(release)
			err = <-finished
			if rollback {
				if !errors.Is(err, abort) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if err := <-stopper; err != nil {
				t.Fatalf("stop after release: %v", err)
			}
			job, err := repo.GetJob(ctx, id)
			if err != nil || !job.StopRequested {
				t.Fatalf("stop lost after release: %+v, %v", job, err)
			}
			video, err := repo.GetVideo(ctx, v.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantExecution, wantStatus := "holder", repository.VideoStatusRunning
			if rollback {
				wantExecution, wantStatus = "", repository.VideoStatusPending
			}
			if job.ExecutionID != wantExecution || video.Status != wantStatus {
				t.Fatalf("rollback=%t left job %+v with video status %s", rollback, job, video.Status)
			}
		})
	}
}

func testRecordingIntentLockSerializesStopAndWait(t *testing.T, h Harness) {
	repo := h.Repo()
	writerRepo := h.ConcurrentRepo(t)
	if err := repo.WithTx(t.Context(), func(tx repository.Repository) error {
		_, err := tx.LockRecordingIntent(t.Context(), "missing")
		return err
	}); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing intent lock: %v", err)
	}
	abort := errors.New("roll back")
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollback=%t", rollback), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			id := fmt.Sprintf("intent-lock-%t", rollback)
			v := seedIntentAttempt(t, ctx, repo, id+"-channel", id, id+"-first")
			deadline := time.Now().UTC().Truncate(time.Second).Add(2 * time.Minute)
			locked, release, finished := holdLock(ctx, repo, func(tx repository.Repository) error {
				intent, err := tx.LockRecordingIntent(ctx, id)
				if err != nil || intent.ID != id || intent.CurrentJobID != v.JobID || intent.StopRequested || intent.Status != repository.RecordingIntentStatusActive {
					return fmt.Errorf("locked intent: %+v, %v", intent, err)
				}
				return nil
			}, func(tx repository.Repository) error {
				intent, err := tx.GetRecordingIntent(ctx, id)
				if err != nil || intent.StopRequested {
					return fmt.Errorf("stop escaped intent lock: %+v, %v", intent, err)
				}
				if err := tx.SetRecordingIntentWaiting(ctx, id, v.JobID, deadline); err != nil {
					return err
				}
				if rollback {
					return abort
				}
				return nil
			})
			awaitLock(t, ctx, locked, finished)
			stopper := make(chan error, 1)
			go func() { stopper <- writerRepo.RequestRecordingIntentStop(ctx, id) }()
			assertBlocked(t, "stop", stopper, release, finished)
			close(release)
			err := <-finished
			if rollback {
				if !errors.Is(err, abort) {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if err := <-stopper; err != nil {
				t.Fatalf("stop after release: %v", err)
			}
			intent, err := repo.GetRecordingIntent(ctx, id)
			if err != nil || !intent.StopRequested || intent.CurrentJobID != v.JobID {
				t.Fatalf("stop lost after release: %+v, %v", intent, err)
			}
			if rollback {
				if intent.Status != repository.RecordingIntentStatusActive || intent.WaitUntil != nil {
					t.Fatalf("rolled back wait transition persisted: %+v", intent)
				}
			} else if intent.Status != repository.RecordingIntentStatusWaiting || intent.WaitUntil == nil || !intent.WaitUntil.Equal(deadline) {
				t.Fatalf("committed wait transition lost: %+v", intent)
			}
		})
	}
}
