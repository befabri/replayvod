package contracttest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// A real unique-constraint violation in the outbox must undo earlier recording
// and attempt writes, just as an application error after those writes does.
func testRecordingTerminalOutboxRollback(t *testing.T, h Harness) {
	ctx := t.Context()
	repo := h.Repo()
	if _, err := repo.UpsertRecordingWebhookConfig(ctx, true, "https://hooks.example/test", "recording.completed,recording.failed"); err != nil {
		t.Fatal(err)
	}
	if err := repo.EnsureRecordingWebhookSecret(ctx, "secret"); err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []string{"completed", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			jobID := "outbox-failure-" + outcome
			v := createWebhookOutboxVideo(t, repo, jobID)
			if _, err := repo.CreateJob(ctx, &repository.JobInput{ID: jobID, VideoID: v.ID, BroadcasterID: v.BroadcasterID}); err != nil {
				t.Fatal(err)
			}
			if err := repo.MarkJobRunning(ctx, jobID); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{MessageID: jobID, DedupeKey: "existing-" + jobID, Event: "recording.test", NextAttemptAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
			delivery := &repository.RecordingWebhookDeliveryInput{MessageID: jobID, DedupeKey: "terminal-" + jobID, Event: "recording." + outcome, VideoID: v.ID, NextAttemptAt: time.Now()}
			err := repo.WithTx(ctx, func(tx repository.Repository) error {
				if outcome == "completed" {
					if err := tx.MarkJobDone(ctx, jobID); err != nil {
						return err
					}
					return tx.MarkVideoDoneAndEnqueueRecordingWebhook(ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false, delivery)
				}
				if err := tx.MarkJobFailed(ctx, jobID, "capture failed"); err != nil {
					return err
				}
				return tx.MarkVideoFailedAndEnqueueRecordingWebhook(ctx, v.ID, "capture failed", repository.CompletionKindPartial, true, delivery)
			})
			if err == nil {
				t.Fatal("outbox constraint violation did not fail the transaction")
			}
			got, err := repo.GetVideo(ctx, v.ID)
			if err != nil {
				t.Fatal(err)
			}
			job, err := repo.GetJob(ctx, jobID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != repository.VideoStatusRunning || job.Status != repository.JobStatusRunning || got.Error != nil || got.DownloadedAt != nil {
				t.Fatalf("outbox failure left terminal writes: video=%+v job=%+v", got, job)
			}
			for _, row := range deliveriesByID(t, repo, ctx) {
				if row.VideoID == v.ID {
					t.Fatalf("unexpected terminal delivery: %+v", row)
				}
			}
		})
	}
}

func testNestedTransactionRejected(t *testing.T, h Harness) {
	repo := h.Repo()
	called := false
	err := repo.WithTx(t.Context(), func(tx repository.Repository) error {
		return tx.WithTx(context.Background(), func(repository.Repository) error { called = true; return nil })
	})
	if err == nil || called {
		t.Fatalf("nested WithTx was accepted: err=%v called=%v", err, called)
	}
}

func testRecordingTerminalTransaction(t *testing.T, h Harness) {
	ctx := t.Context()
	repo := h.Repo()
	if _, err := repo.UpsertRecordingWebhookConfig(ctx, true, "https://hooks.example/test", "recording.completed,recording.failed"); err != nil {
		t.Fatal(err)
	}
	if err := repo.EnsureRecordingWebhookSecret(ctx, "secret"); err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []string{"completed", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			jobID := "terminal-" + outcome
			v := createWebhookOutboxVideo(t, repo, jobID)
			if _, err := repo.CreateJob(ctx, &repository.JobInput{ID: jobID, VideoID: v.ID, BroadcasterID: v.BroadcasterID}); err != nil {
				t.Fatal(err)
			}
			if err := repo.MarkJobRunning(ctx, jobID); err != nil {
				t.Fatal(err)
			}
			delivery := &repository.RecordingWebhookDeliveryInput{MessageID: jobID, DedupeKey: jobID, Event: "recording." + outcome, VideoID: v.ID, NextAttemptAt: time.Now()}
			abort := errors.New("abort terminal transaction")
			persist := func(failAfter string) error {
				return repo.WithTx(ctx, func(tx repository.Repository) error {
					var err error
					if outcome == "completed" {
						err = tx.MarkVideoDoneAndEnqueueRecordingWebhook(ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false, delivery)
					} else {
						err = tx.MarkVideoFailedAndEnqueueRecordingWebhook(ctx, v.ID, "capture failed", repository.CompletionKindPartial, true, delivery)
					}
					if err != nil {
						return err
					}
					if failAfter == "video" {
						return abort
					}
					if outcome == "completed" {
						err = tx.MarkJobDone(ctx, jobID)
					} else {
						err = tx.MarkJobFailed(ctx, jobID, "capture failed")
					}
					if err != nil {
						return err
					}
					if failAfter == "job" {
						return abort
					}
					return nil
				})
			}
			for _, failAfter := range []string{"video", "job"} {
				if err := persist(failAfter); !errors.Is(err, abort) {
					t.Fatalf("expected rollback after %s, got %v", failAfter, err)
				}
				got, err := repo.GetVideo(ctx, v.ID)
				if err != nil {
					t.Fatal(err)
				}
				job, err := repo.GetJob(ctx, jobID)
				if err != nil {
					t.Fatal(err)
				}
				if got.Status != repository.VideoStatusRunning || job.Status != repository.JobStatusRunning || got.Error != nil || got.DownloadedAt != nil {
					t.Fatalf("terminal writes escaped rollback: video=%+v job=%+v", got, job)
				}
				for _, row := range deliveriesByID(t, repo, ctx) {
					if row.VideoID == v.ID {
						t.Fatalf("webhook escaped rollback: %+v", row)
					}
				}
			}
			if err := persist(""); err != nil {
				t.Fatal(err)
			}
			wantStatus := repository.VideoStatusDone
			if outcome == "failed" {
				wantStatus = repository.VideoStatusFailed
			}
			got, err := repo.GetVideo(ctx, v.ID)
			if err != nil {
				t.Fatal(err)
			}
			job, err := repo.GetJob(ctx, jobID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != wantStatus || job.Status != wantStatus {
				t.Fatalf("terminal commit incomplete: video=%s job=%s", got.Status, job.Status)
			}
			count := 0
			for _, row := range deliveriesByID(t, repo, ctx) {
				if row.VideoID == v.ID {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("committed webhook count=%d, want 1", count)
			}
		})
	}
}
