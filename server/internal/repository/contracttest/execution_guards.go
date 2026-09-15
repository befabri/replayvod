package contracttest

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testStoppedAttemptDiscovery(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "execution-channel")
	want := []string{"a-live-pending", "b-archive-pending", "c-live-running", "d-archive-running"}
	for _, id := range append(slices.Clone(want), "e-unstopped", "f-terminal", "g-deleted") {
		input := executionInput(id)
		if id == "b-archive-pending" || id == "d-archive-running" {
			input.Source, input.TwitchVideoID = repository.VideoSourceVOD, &id
		}
		v, err := repository.CreateAttempt(ctx, repo, input, json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		if id == "c-live-running" || id == "d-archive-running" {
			if err := repository.ClaimAttempt(ctx, repo, repository.AttemptClaim{JobID: id, VideoID: v.ID, ExecutionID: "owner"}, ""); err != nil {
				t.Fatal(err)
			}
		}
		if id != "e-unstopped" {
			if err := repository.RequestAttemptStop(ctx, repo, id); err != nil {
				t.Fatal(err)
			}
		}
		if id == "f-terminal" {
			if err := repo.MarkVideoFailed(ctx, v.ID, "cancelled", repository.CompletionKindCancelled, false); err != nil {
				t.Fatal(err)
			}
			if err := repo.MarkJobFailed(ctx, id, "cancelled"); err != nil {
				t.Fatal(err)
			}
		}
		if id == "g-deleted" {
			if err := repo.SoftDeleteVideo(ctx, v.ID, "manual"); err != nil {
				t.Fatal(err)
			}
		}
	}
	var got []string
	for after := ""; ; {
		jobs, err := repo.ListStoppedJobs(ctx, after, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(jobs) > 2 {
			t.Fatalf("unbounded stop discovery: %d", len(jobs))
		}
		for _, job := range jobs {
			if !job.StopRequested || job.ID <= after {
				t.Fatalf("invalid stop discovery cursor or row: %+v", job)
			}
			got = append(got, job.ID)
			after = job.ID
		}
		if len(jobs) < 2 {
			break
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("stopped attempts = %v, want %v", got, want)
	}
}

func testAttemptStopSurvivesCheckpointsAndFencesWriters(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "execution-channel")
	v, err := repository.CreateAttempt(ctx, repo, executionInput("stopped"), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	claim := repository.AttemptClaim{JobID: v.JobID, VideoID: v.ID, ExecutionID: "first"}
	if err := repository.ClaimAttempt(ctx, repo, claim, ""); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := repository.RequestAttemptStop(ctx, repo, v.JobID); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.CheckpointAttempt(ctx, v.JobID, claim.ExecutionID, json.RawMessage(`{"stage":"STORE"}`)); err != nil {
		t.Fatal(err)
	}
	job, err := repo.GetJob(ctx, v.JobID)
	if err != nil || !job.StopRequested || job.AcceptsMetadata {
		t.Fatalf("checkpoint erased stop: %+v, %v", job, err)
	}
	if err := repository.WithAttempt(ctx, repo, claim, func(repository.Repository) error { t.Fatal("stopped writer ran"); return nil }); !errors.Is(err, repository.ErrStopRequested) {
		t.Fatalf("stopped write: %v", err)
	}
	if err := repository.ClaimAttempt(ctx, repo, claim, ""); !errors.Is(err, repository.ErrStopRequested) {
		t.Fatalf("stopped claim: %v", err)
	}
	claim.AllowStopRequested = true
	if err := repo.WithTx(ctx, func(tx repository.Repository) error { _, err := repository.GuardAttempt(ctx, tx, claim); return err }); err != nil {
		t.Fatalf("cancellation cannot settle: %v", err)
	}
	claim.ExecutionID = "obsolete"
	if err := repo.WithTx(ctx, func(tx repository.Repository) error { _, err := repository.GuardAttempt(ctx, tx, claim); return err }); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("stop bypassed execution ownership: %v", err)
	}
}

func testStoppedAdmissionRejectsInitialMetadata(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "execution-channel")
	v, err := repository.CreateAttempt(ctx, repo, executionInput("stopped-admission"), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.RequestAttemptStop(ctx, repo, v.JobID); err != nil {
		t.Fatal(err)
	}
	_, err = repo.RecordVideoMetadataChange(ctx, repository.VideoMetadataChangeInput{
		VideoID: v.ID, JobID: v.JobID, Initial: true, Title: "Late snapshot", OccurredAt: time.Now().UTC(),
	})
	if !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("stopped admission accepted initial metadata: %v", err)
	}
	if events, err := repo.ListVideoMetadataChanges(ctx, v.ID); err != nil || len(events) != 0 {
		t.Fatalf("stopped admission changed timeline: %+v, %v", events, err)
	}
	if spans, err := repo.ListTitlesForVideo(ctx, v.ID); err != nil || len(spans) != 0 {
		t.Fatalf("stopped admission opened title spans: %+v, %v", spans, err)
	}
}

func testExecutionRejectsStaleTransitions(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "execution-channel")
	v, err := repository.CreateAttempt(ctx, repo, executionInput("guarded"), json.RawMessage(`{"stage":"AUTH","current_part_index":1}`))
	if err != nil {
		t.Fatal(err)
	}
	first := repository.AttemptClaim{JobID: v.JobID, VideoID: v.ID, ExecutionID: "first"}
	if err := repository.ClaimAttempt(ctx, repo, first, ""); err != nil {
		t.Fatal(err)
	}
	second := first
	second.ExecutionID = "second"
	if err := repository.ClaimAttempt(ctx, repo, second, "wrong-previous"); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("stale discoverer replaced owner: %v", err)
	}
	job, err := repo.GetJob(ctx, v.JobID)
	if err != nil || job.ExecutionID != first.ExecutionID {
		t.Fatalf("rejected claim mutated owner: %+v, %v", job, err)
	}
	if err := repository.ClaimAttempt(ctx, repo, second, first.ExecutionID); err != nil {
		t.Fatal(err)
	}
	if err := repository.ClaimAttempt(ctx, repo, first, ""); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("old execution reclaimed current owner: %v", err)
	}
	if _, err := repo.UpsertTask(ctx, "guarded-task", "Guarded task", 60); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClaimTask(ctx, "guarded-task", "first"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClaimTask(ctx, "guarded-task", "competing"); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("competing task execution replaced running owner: %v", err)
	}
	if err := repo.SetTaskNextRun(ctx, "guarded-task"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClaimTask(ctx, "guarded-task", "queued"); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("queued task run replaced running owner: %v", err)
	}
	if due, err := repo.ListDueTasks(ctx); err != nil || len(due) != 0 {
		t.Fatalf("running task is due: %+v, %v", due, err)
	}
	if err := repo.ClaimTask(ctx, "guarded-task", "first"); err != nil {
		t.Fatalf("same task execution cannot confirm its claim: %v", err)
	}
	if err := repo.SettleTask(ctx, "guarded-task", "wrong-token", repository.TaskStatusSuccess, 20, ""); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("wrong task token settled: %v", err)
	}
	row, err := repo.GetTask(ctx, "guarded-task")
	if err != nil || row.ExecutionID != "first" || row.LastStatus != repository.TaskStatusRunning {
		t.Fatalf("rejected settlement changed task: %+v, %v", row, err)
	}
	if err := repo.SettleTask(ctx, "guarded-task", "first", repository.TaskStatusSuccess, 20, ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.SettleTask(ctx, "guarded-task", "first", repository.TaskStatusFailed, 30, "late failure"); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("terminal task accepted conflicting outcome: %v", err)
	}
	if err := repo.SetTaskNextRun(ctx, "guarded-task"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClaimTask(ctx, "guarded-task", "second"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SettleTask(ctx, "guarded-task", "first", repository.TaskStatusSuccess, 20, ""); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("old task execution settled replacement: %v", err)
	}
}

func testRecordingIntentConstraints(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "execution-channel")
	for _, seconds := range []int64{0, -1} {
		err := repo.CreateRecordingIntent(ctx, repository.RecordingIntent{ID: "invalid", BroadcasterID: "execution-channel", CurrentJobID: "invalid", Params: json.RawMessage(`{}`), WaitSeconds: seconds})
		if err == nil {
			t.Fatalf("accepted nonpositive restart window %d", seconds)
		}
	}
	input := executionInput("intent-first")
	input.IntentID, input.IntentParams, input.RestartWaitSeconds = "intent", json.RawMessage(`{}`), 120
	firstStream := "first-broadcast"
	input.StreamID = &firstStream
	state := json.RawMessage(`{"stage":"AUTH","current_part_index":1}`)
	first, err := repository.CreateAttempt(ctx, repo, input, state)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"active", "waiting"} {
		if status == "waiting" {
			if err := repo.SetRecordingIntentWaiting(ctx, "intent", first.JobID, time.Now().Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
		}
		other := executionInput("competing-" + status)
		other.IntentID, other.IntentParams, other.RestartWaitSeconds = "other-"+status, json.RawMessage(`{}`), 120
		if _, err := repository.CreateAttempt(ctx, repo, other, state); !errors.Is(err, repository.ErrDuplicate) {
			t.Fatalf("second %s intent admitted for channel: %v", status, err)
		}
		if _, err := repo.GetVideoByJobID(ctx, other.JobID); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("rejected channel intent left a video: %v", err)
		}
	}
	intent, err := repo.GetRecordingIntent(ctx, "intent")
	if err != nil || intent.WaitUntil == nil {
		t.Fatalf("waiting intent = %+v, %v", intent, err)
	}
	SeedUserChannel(t, ctx, repo, "other-owner", "other-channel")
	wrongChannel := executionInput("wrong-channel")
	wrongChannel.BroadcasterID = "other-channel"
	wrongChannel.IntentID, wrongChannel.IntentPreviousJobID = "intent", first.JobID
	wrongChannel.IntentObservedAt = intent.WaitUntil.Add(-time.Second)
	if _, err := repository.CreateAttempt(ctx, repo, wrongChannel, state); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("intent admitted another broadcaster: %v", err)
	}
	if _, err := repo.GetVideoByJobID(ctx, wrongChannel.JobID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("rejected broadcaster left an admission: %v", err)
	}
	next := executionInput("too-late")
	next.IntentID, next.IntentPreviousJobID = "intent", first.JobID
	next.IntentObservedAt = intent.WaitUntil.Add(time.Millisecond)
	if _, err := repository.CreateAttempt(ctx, repo, next, state); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("late observation extended restart deadline: %v", err)
	}
	next.JobID, next.Filename = "second", "second"
	next.IntentObservedAt = *intent.WaitUntil // The deadline itself is inclusive.
	secondStream := "second-broadcast"
	next.StreamID = &secondStream
	second, err := repository.CreateAttempt(ctx, repo, next, state)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetRecordingIntentWaiting(ctx, "intent", second.JobID, intent.WaitUntil.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	next.JobID, next.Filename, next.IntentPreviousJobID, next.StreamID = "duplicate-broadcast", "duplicate-broadcast", second.JobID, &firstStream
	if _, err := repository.CreateAttempt(ctx, repo, next, state); !errors.Is(err, repository.ErrDuplicate) {
		t.Fatalf("durable broadcast uniqueness not enforced: %v", err)
	}
	intent, err = repo.GetRecordingIntent(ctx, "intent")
	if err != nil || intent.CurrentJobID != second.JobID || intent.Status != "waiting" {
		t.Fatalf("duplicate broadcast changed intent: %+v, %v", intent, err)
	}
	rows, err := repo.ListRelatedRecordings(ctx, first.ID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("failed admissions changed relationship: %+v, %v", rows, err)
	}
}

func testJobStopAndMetadataGuards(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "execution-channel")
	if err := repo.RequestJobStop(ctx, "missing"); err != nil {
		t.Fatalf("stopping a missing job: %v", err)
	}
	if err := repo.StopJobMetadata(ctx, "missing", "any"); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("metadata stop on a missing job: %v", err)
	}
	pending, err := repository.CreateAttempt(ctx, repo, executionInput("stop-pending"), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RequestJobStop(ctx, pending.JobID); err != nil {
		t.Fatal(err)
	}
	job, err := repo.GetJob(ctx, pending.JobID)
	if err != nil || !job.StopRequested || job.AcceptsMetadata || job.Status != repository.JobStatusPending {
		t.Fatalf("stopped pending job = %+v, %v", job, err)
	}
	running, err := repository.CreateAttempt(ctx, repo, executionInput("stop-running"), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ClaimAttempt(ctx, repo, repository.AttemptClaim{JobID: running.JobID, VideoID: running.ID, ExecutionID: "first"}, ""); err != nil {
		t.Fatal(err)
	}
	if job, err = repo.GetJob(ctx, running.JobID); err != nil || !job.AcceptsMetadata {
		t.Fatalf("claimed live job = %+v, %v", job, err)
	}
	if err := repo.StopJobMetadata(ctx, running.JobID, "second"); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("foreign execution stopped metadata: %v", err)
	}
	if job, err = repo.GetJob(ctx, running.JobID); err != nil || !job.AcceptsMetadata {
		t.Fatalf("rejected metadata stop applied: %+v, %v", job, err)
	}
	for range 2 {
		if err := repo.StopJobMetadata(ctx, running.JobID, "first"); err != nil {
			t.Fatalf("owner metadata stop: %v", err)
		}
	}
	job, err = repo.GetJob(ctx, running.JobID)
	if err != nil || job.AcceptsMetadata || job.StopRequested || job.Status != repository.JobStatusRunning || job.ExecutionID != "first" {
		t.Fatalf("metadata stop changed more than admission: %+v, %v", job, err)
	}
	for range 2 {
		if err := repo.RequestJobStop(ctx, running.JobID); err != nil {
			t.Fatal(err)
		}
	}
	job, err = repo.GetJob(ctx, running.JobID)
	if err != nil || !job.StopRequested || job.Status != repository.JobStatusRunning || job.ExecutionID != "first" {
		t.Fatalf("stopped running job = %+v, %v", job, err)
	}
	terminal, err := repository.CreateAttempt(ctx, repo, executionInput("stop-terminal"), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	failAttempt(t, ctx, repo, terminal)
	if err := repo.RequestJobStop(ctx, terminal.JobID); err != nil {
		t.Fatalf("stopping a terminal job: %v", err)
	}
	if job, err = repo.GetJob(ctx, terminal.JobID); err != nil || job.StopRequested || job.Status != repository.JobStatusFailed {
		t.Fatalf("terminal job changed by stop: %+v, %v", job, err)
	}
	if err := repo.StopJobMetadata(ctx, terminal.JobID, job.ExecutionID); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("metadata stop on an unclaimed terminal job: %v", err)
	}
	claimedTerminal, err := repository.CreateAttempt(ctx, repo, executionInput("stop-claimed-terminal"), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ClaimAttempt(ctx, repo, repository.AttemptClaim{JobID: claimedTerminal.JobID, VideoID: claimedTerminal.ID, ExecutionID: "owner"}, ""); err != nil {
		t.Fatal(err)
	}
	failAttempt(t, ctx, repo, claimedTerminal)
	if err := repo.StopJobMetadata(ctx, claimedTerminal.JobID, "owner"); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("metadata stop by the former owner of a terminal job: %v", err)
	}
}
