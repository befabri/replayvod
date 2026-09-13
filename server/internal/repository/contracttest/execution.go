package contracttest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

type admissionFailure struct{ repository.Repository }

func (f admissionFailure) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return f.Repository.WithTx(ctx, func(tx repository.Repository) error { return fn(admissionFailure{tx}) })
}

func (admissionFailure) CreateJob(context.Context, *repository.JobInput) (*repository.Job, error) {
	return nil, errors.New("injected job insertion failure")
}

type lostCommit struct {
	repository.Repository
	lose bool
}

func (f *lostCommit) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	err := f.Repository.WithTx(ctx, fn)
	if err == nil && f.lose {
		f.lose = false
		return repository.ErrCommitUncertain
	}
	return err
}

func executionInput(job string) *repository.VideoInput {
	return &repository.VideoInput{JobID: job, Filename: job, DisplayName: "Broadcaster", BroadcasterID: "execution-channel", Quality: repository.QualityHigh, Status: repository.VideoStatusPending}
}

func testCreateAttemptAtomic(t *testing.T, h Harness) {
	ctx := t.Context()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "execution-channel")
	input := executionInput("admission")
	checkpoint := json.RawMessage(`{"stage":"AUTH","current_part_index":1}`)
	if _, err := repository.CreateAttempt(ctx, admissionFailure{repo}, input, checkpoint); err == nil {
		t.Fatal("injected failure accepted")
	}
	if _, err := repo.GetVideoByJobID(ctx, input.JobID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("orphan video survived: %v", err)
	}
	v, err := repository.CreateAttempt(ctx, repo, input, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	j, err := repo.GetJob(ctx, input.JobID)
	if err != nil || j.VideoID != v.ID || !jsonEqual(json.RawMessage(j.ResumeState), checkpoint) {
		t.Fatalf("attempt lost checkpoint: %+v %v", j, err)
	}
	if v.StreamID != nil {
		t.Fatal("optional broadcast identity became a requirement")
	}
}

func testAttemptCommitConfirmationLost(t *testing.T, h Harness) {
	ctx := t.Context()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "execution-channel")
	fault := &lostCommit{Repository: repo, lose: true}
	input := executionInput("lost-admission")
	checkpoint := json.RawMessage(`{"stage":"AUTH","current_part_index":1}`)
	if _, err := repository.CreateAttempt(ctx, fault, input, checkpoint); !errors.Is(err, repository.ErrCommitUncertain) {
		t.Fatalf("error=%v", err)
	}
	v, err := repository.CreateAttempt(ctx, fault, input, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	claim := repository.AttemptClaim{VideoID: v.ID, JobID: input.JobID, ExecutionID: "execution-1"}
	fault.lose = true
	if err := repository.ClaimAttempt(ctx, fault, claim, ""); !errors.Is(err, repository.ErrCommitUncertain) {
		t.Fatalf("error=%v", err)
	}
	if err := repository.ClaimAttempt(ctx, fault, claim, ""); err != nil {
		t.Fatal(err)
	}
	j, err := repo.GetJob(ctx, input.JobID)
	if err != nil || j.ExecutionID != claim.ExecutionID || j.Status != repository.JobStatusRunning {
		t.Fatalf("claim=%+v %v", j, err)
	}
	v2, err := repo.GetVideoByJobID(ctx, input.JobID)
	if err != nil || v2.ID != v.ID {
		t.Fatalf("admission identity changed: %+v %v", v2, err)
	}
}

func testRecordingIntentAtomicity(t *testing.T, h Harness) {
	ctx := t.Context()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "execution-channel")
	input := executionInput("intent-first")
	input.IntentID = "manual-intent"
	input.IntentParams = json.RawMessage(`{"Quality":"HIGH"}`)
	input.RestartWaitSeconds = 120
	state := json.RawMessage(`{"stage":"AUTH","current_part_index":1}`)
	fault := &lostCommit{Repository: repo, lose: true}
	if _, err := repository.CreateAttempt(ctx, fault, input, state); !errors.Is(err, repository.ErrCommitUncertain) {
		t.Fatalf("admission=%v", err)
	}
	first, err := repository.CreateAttempt(ctx, repo, input, state)
	if err != nil {
		t.Fatal(err)
	}
	stopped := time.Now().UTC().Truncate(time.Second)
	deadline := stopped.Add(120 * time.Second)
	if err := repo.SetRecordingIntentWaiting(ctx, input.IntentID, first.JobID, deadline); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetRecordingIntentWaiting(ctx, input.IntentID, first.JobID, deadline.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	intent, err := repo.GetRecordingIntent(ctx, input.IntentID)
	if err != nil || intent.WaitUntil == nil || !intent.WaitUntil.Equal(deadline) {
		t.Fatalf("replay extended deadline: %+v %v", intent, err)
	}
	streamID := "next-broadcast"
	next := executionInput("intent-next")
	next.IntentID = input.IntentID
	next.IntentPreviousJobID = first.JobID
	next.IntentObservedAt = deadline.Add(-time.Second)
	next.StreamID = &streamID
	next.StreamStartedAt = stopped.Add(time.Second)
	if _, err := repository.CreateAttempt(ctx, admissionFailure{repo}, next, state); err == nil {
		t.Fatal("successor job failure ignored")
	}
	intent, _ = repo.GetRecordingIntent(ctx, input.IntentID)
	if intent.CurrentJobID != first.JobID || intent.Status != "waiting" {
		t.Fatalf("failed successor advanced intent: %+v", intent)
	}
	second, err := repository.CreateAttempt(ctx, repo, next, state)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || first.JobID == second.JobID {
		t.Fatal("return reused recording or attempt")
	}
	rows, err := repo.ListRelatedRecordings(ctx, first.ID)
	if err != nil || len(rows) != 2 || rows[0].ID != first.ID || rows[1].ID != second.ID {
		t.Fatalf("chain %+v %v", rows, err)
	}
	linked, err := repo.GetStream(ctx, streamID)
	if err != nil || !linked.StartedAt.Equal(next.StreamStartedAt) {
		t.Fatalf("known identity not linked: %+v %v", linked, err)
	}
	if err := repo.SetRecordingIntentWaiting(ctx, input.IntentID, second.JobID, deadline.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.RequestRecordingIntentStop(ctx, input.IntentID); err != nil {
		t.Fatal(err)
	}
	third := executionInput("intent-rejected")
	third.IntentID = input.IntentID
	third.IntentPreviousJobID = second.JobID
	third.IntentObservedAt = stopped
	if _, err := repository.CreateAttempt(ctx, repo, third, state); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("stop did not win admission: %v", err)
	}
	if _, err := repo.GetVideoByJobID(ctx, third.JobID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("stopped intent left orphan: %v", err)
	}
}
