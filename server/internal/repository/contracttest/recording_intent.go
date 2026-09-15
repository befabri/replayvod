package contracttest

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// seedIntentAttempt admits the first attempt of a manual recording intent on
// its own channel and returns the admitted video.
func seedIntentAttempt(t *testing.T, ctx context.Context, repo repository.Repository, channel, intentID, jobID string) *repository.Video {
	t.Helper()
	SeedUserChannel(t, ctx, repo, "owner", channel)
	input := executionInput(jobID)
	input.BroadcasterID = channel
	input.IntentID, input.IntentParams, input.RestartWaitSeconds = intentID, json.RawMessage(`{}`), 120
	v, err := repository.CreateAttempt(ctx, repo, input, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("admit %s: %v", jobID, err)
	}
	return v
}

// seedIntentSuccessor parks the intent behind previous and admits the next
// broadcast within the restart window.
func seedIntentSuccessor(t *testing.T, ctx context.Context, repo repository.Repository, channel, intentID string, previous *repository.Video, jobID string) *repository.Video {
	t.Helper()
	deadline := time.Now().UTC().Truncate(time.Second).Add(2 * time.Minute)
	if err := repo.SetRecordingIntentWaiting(ctx, intentID, previous.JobID, deadline); err != nil {
		t.Fatalf("park %s: %v", intentID, err)
	}
	input := executionInput(jobID)
	input.BroadcasterID = channel
	input.IntentID, input.IntentPreviousJobID, input.IntentObservedAt = intentID, previous.JobID, deadline.Add(-time.Second)
	stream := jobID + "-stream"
	input.StreamID = &stream
	v, err := repository.CreateAttempt(ctx, repo, input, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("admit successor %s: %v", jobID, err)
	}
	return v
}

func failAttempt(t *testing.T, ctx context.Context, repo repository.Repository, v *repository.Video) {
	t.Helper()
	if err := repo.MarkVideoFailed(ctx, v.ID, "failed", repository.CompletionKindCancelled, false); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkJobFailed(ctx, v.JobID, "failed"); err != nil {
		t.Fatal(err)
	}
}

func testRecordingIntentActivationWindow(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	first := seedIntentAttempt(t, ctx, repo, "activation-channel", "activation", "activation-first")
	deadline := time.Now().UTC().Truncate(time.Second).Add(2 * time.Minute)
	if err := repo.ActivateRecordingIntent(ctx, "activation", first.JobID, "activation-second", "stream-2", deadline.Add(-time.Second)); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("active intent accepted a successor: %v", err)
	}
	if err := repo.SetRecordingIntentWaiting(ctx, "activation", first.JobID, deadline); err != nil {
		t.Fatal(err)
	}
	for name, err := range map[string]error{
		"wrong previous":   repo.ActivateRecordingIntent(ctx, "activation", "activation-other", "activation-second", "stream-2", deadline.Add(-time.Second)),
		"late observation": repo.ActivateRecordingIntent(ctx, "activation", first.JobID, "activation-second", "stream-2", deadline.Add(time.Second)),
		"missing intent":   repo.ActivateRecordingIntent(ctx, "missing", first.JobID, "activation-second", "stream-2", deadline.Add(-time.Second)),
	} {
		if !errors.Is(err, repository.ErrStaleExecution) {
			t.Fatalf("%s activated: %v", name, err)
		}
	}
	intent, err := repo.GetRecordingIntent(ctx, "activation")
	if err != nil || intent.Status != repository.RecordingIntentStatusWaiting || intent.CurrentJobID != first.JobID || intent.WaitUntil == nil || !intent.WaitUntil.Equal(deadline) {
		t.Fatalf("rejected activations changed intent: %+v, %v", intent, err)
	}
	if err := repo.ActivateRecordingIntent(ctx, "activation", first.JobID, "activation-second", "stream-2", deadline); err != nil {
		t.Fatalf("deadline is inclusive: %v", err)
	}
	intent, err = repo.GetRecordingIntent(ctx, "activation")
	if err != nil || intent.Status != repository.RecordingIntentStatusActive || intent.CurrentJobID != "activation-second" || intent.LastStreamID != "stream-2" || intent.WaitUntil != nil || intent.StopRequested {
		t.Fatalf("activated intent = %+v, %v", intent, err)
	}
	if err := repo.ActivateRecordingIntent(ctx, "activation", "activation-second", "activation-third", "", deadline); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("activation replayed on active intent: %v", err)
	}
	if err := repo.SetRecordingIntentWaiting(ctx, "activation", "activation-second", deadline); err != nil {
		t.Fatal(err)
	}
	if err := repo.RequestRecordingIntentStop(ctx, "activation"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ActivateRecordingIntent(ctx, "activation", "activation-second", "activation-third", "", deadline); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("stopped intent activated: %v", err)
	}
	intent, err = repo.GetRecordingIntent(ctx, "activation")
	if err != nil || intent.Status != repository.RecordingIntentStatusWaiting || intent.CurrentJobID != "activation-second" || !intent.StopRequested {
		t.Fatalf("stop lost: %+v, %v", intent, err)
	}
}

func testCloseRecordingIntentReleasesChannel(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	first := seedIntentAttempt(t, ctx, repo, "close-channel", "close", "close-first")
	deadline := time.Now().UTC().Truncate(time.Second).Add(2 * time.Minute)
	if err := repo.SetRecordingIntentWaiting(ctx, "close", first.JobID, deadline); err != nil {
		t.Fatal(err)
	}
	if err := repo.CloseRecordingIntent(ctx, "missing", repository.RecordingIntentStatusStopped); err != nil {
		t.Fatalf("closing a missing intent: %v", err)
	}
	if err := repo.CloseRecordingIntent(ctx, "close", "bogus"); err == nil {
		t.Fatal("accepted an unknown terminal status")
	}
	intent, err := repo.GetRecordingIntent(ctx, "close")
	if err != nil || intent.Status != repository.RecordingIntentStatusWaiting || intent.WaitUntil == nil {
		t.Fatalf("rejected close changed intent: %+v, %v", intent, err)
	}
	if err := repo.CloseRecordingIntent(ctx, "close", repository.RecordingIntentStatusStopped); err != nil {
		t.Fatal(err)
	}
	intent, err = repo.GetRecordingIntent(ctx, "close")
	if err != nil || intent.Status != repository.RecordingIntentStatusStopped || intent.WaitUntil != nil || intent.CurrentJobID != first.JobID {
		t.Fatalf("closed intent = %+v, %v", intent, err)
	}
	if err := repo.SetRecordingIntentWaiting(ctx, "close", first.JobID, deadline); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("closed intent reopened: %v", err)
	}
	if err := repo.ActivateRecordingIntent(ctx, "close", first.JobID, "close-second", "", deadline); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("closed intent activated: %v", err)
	}
	seedIntentAttempt(t, ctx, repo, "close-channel", "close-next", "close-next-first")
	if err := repo.CloseRecordingIntent(ctx, "close-next", repository.RecordingIntentStatusExpired); err != nil {
		t.Fatal(err)
	}
	intent, err = repo.GetRecordingIntent(ctx, "close-next")
	if err != nil || intent.Status != repository.RecordingIntentStatusExpired {
		t.Fatalf("expired intent = %+v, %v", intent, err)
	}
}

func testGetRecordingIntentByJob(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "execution-channel")
	if _, err := repo.GetRecordingIntentByJob(ctx, "missing"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing job: %v", err)
	}
	if _, err := repository.CreateAttempt(ctx, repo, executionInput("no-intent"), json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetRecordingIntentByJob(ctx, "no-intent"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("job without intent: %v", err)
	}
	first := seedIntentAttempt(t, ctx, repo, "byjob-channel", "byjob", "byjob-first")
	second := seedIntentSuccessor(t, ctx, repo, "byjob-channel", "byjob", first, "byjob-second")
	for _, job := range []string{first.JobID, second.JobID} {
		intent, err := repo.GetRecordingIntentByJob(ctx, job)
		if err != nil || intent.ID != "byjob" || intent.BroadcasterID != "byjob-channel" || intent.CurrentJobID != second.JobID {
			t.Fatalf("intent by %s = %+v, %v", job, intent, err)
		}
	}
}

func testLinkRecordingIntentVideoPositionsAndUniqueness(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	first := seedIntentAttempt(t, ctx, repo, "link-channel", "link", "link-first")
	extra := make([]*repository.Video, 4)
	for i := range extra {
		input := executionInput("link-extra-" + string(rune('a'+i)))
		input.BroadcasterID = "link-channel"
		v, err := repo.CreateVideo(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		extra[i] = v
	}
	stream := "link-stream"
	if err := repo.LinkRecordingIntentVideo(ctx, "link", extra[0].ID, nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.LinkRecordingIntentVideo(ctx, "link", extra[1].ID, &stream); err != nil {
		t.Fatal(err)
	}
	if err := repo.LinkRecordingIntentVideo(ctx, "link", extra[1].ID, nil); !errors.Is(err, repository.ErrDuplicate) {
		t.Fatalf("video linked twice: %v", err)
	}
	if err := repo.LinkRecordingIntentVideo(ctx, "link", extra[2].ID, &stream); !errors.Is(err, repository.ErrDuplicate) {
		t.Fatalf("broadcast linked twice to one intent: %v", err)
	}
	if err := repo.LinkRecordingIntentVideo(ctx, "link", extra[2].ID, nil); err != nil {
		t.Fatalf("unknown broadcasts must not collide: %v", err)
	}
	if err := repo.LinkRecordingIntentVideo(ctx, "missing", extra[3].ID, nil); err == nil {
		t.Fatal("linked to a missing intent")
	}
	rows, err := repo.ListRelatedRecordings(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{first.ID, extra[0].ID, extra[1].ID, extra[2].ID}
	got := make([]int64, len(rows))
	for i, row := range rows {
		got[i] = row.ID
		if row.Position != int64(i+1) {
			t.Fatalf("position of %d = %d, want %d", row.ID, row.Position, i+1)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("chain = %v, want %v", got, want)
	}
}

func testListRecordingIntentJobs(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	a := seedIntentAttempt(t, ctx, repo, "jobs-channel", "jobs", "jobs-a")
	b := seedIntentSuccessor(t, ctx, repo, "jobs-channel", "jobs", a, "jobs-b")
	c := seedIntentSuccessor(t, ctx, repo, "jobs-channel", "jobs", b, "jobs-c")
	d := seedIntentSuccessor(t, ctx, repo, "jobs-channel", "jobs", c, "jobs-d")
	seedIntentAttempt(t, ctx, repo, "jobs-other-channel", "jobs-other", "jobs-z")
	failAttempt(t, ctx, repo, a)
	if err := repository.ClaimAttempt(ctx, repo, repository.AttemptClaim{JobID: b.JobID, VideoID: b.ID, ExecutionID: "owner"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.SoftDeleteVideo(ctx, d.ID, repository.DeletionKindManual); err != nil {
		t.Fatal(err)
	}
	if rows, err := repo.ListRecordingIntentJobs(ctx, "missing", "", 10); err != nil || len(rows) != 0 {
		t.Fatalf("missing intent jobs = %+v, %v", rows, err)
	}
	var got []string
	for after := ""; ; {
		jobs, err := repo.ListRecordingIntentJobs(ctx, "jobs", after, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(jobs) > 1 {
			t.Fatalf("unbounded page: %d", len(jobs))
		}
		if len(jobs) == 0 {
			break
		}
		if jobs[0].ID <= after {
			t.Fatalf("cursor went backwards: %s after %s", jobs[0].ID, after)
		}
		got = append(got, jobs[0].ID)
		after = jobs[0].ID
	}
	if want := []string{b.JobID, c.JobID}; !slices.Equal(got, want) {
		t.Fatalf("live intent jobs = %v, want %v", got, want)
	}
	all, err := repo.ListRecordingIntentJobs(ctx, "jobs", "", 10)
	if err != nil || len(all) != 2 || all[0].Status != repository.JobStatusRunning || all[0].ExecutionID != "owner" || all[1].Status != repository.JobStatusPending {
		t.Fatalf("intent jobs = %+v, %v", all, err)
	}
}

func testListRecoverableRecordingIntents(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	seedIntentAttempt(t, ctx, repo, "rec-a-channel", "rec-a", "rec-a-first")
	b := seedIntentAttempt(t, ctx, repo, "rec-b-channel", "rec-b", "rec-b-first")
	if err := repo.SetRecordingIntentWaiting(ctx, "rec-b", b.JobID, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	c := seedIntentAttempt(t, ctx, repo, "rec-c-channel", "rec-c", "rec-c-first")
	if err := repository.ClaimAttempt(ctx, repo, repository.AttemptClaim{JobID: c.JobID, VideoID: c.ID, ExecutionID: "owner"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.CloseRecordingIntent(ctx, "rec-c", repository.RecordingIntentStatusStopped); err != nil {
		t.Fatal(err)
	}
	d := seedIntentAttempt(t, ctx, repo, "rec-d-channel", "rec-d", "rec-d-first")
	failAttempt(t, ctx, repo, d)
	if err := repo.CloseRecordingIntent(ctx, "rec-d", repository.RecordingIntentStatusStopped); err != nil {
		t.Fatal(err)
	}
	e := seedIntentAttempt(t, ctx, repo, "rec-e-channel", "rec-e", "rec-e-first")
	if err := repo.MarkVideoDone(ctx, e.ID, 1, 1, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkJobDone(ctx, e.JobID); err != nil {
		t.Fatal(err)
	}
	if err := repo.CloseRecordingIntent(ctx, "rec-e", repository.RecordingIntentStatusExpired); err != nil {
		t.Fatal(err)
	}
	var got []string
	for after := ""; ; {
		intents, err := repo.ListRecoverableRecordingIntents(ctx, after, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(intents) > 2 {
			t.Fatalf("unbounded page: %d", len(intents))
		}
		for _, intent := range intents {
			if intent.ID <= after {
				t.Fatalf("cursor went backwards: %s after %s", intent.ID, after)
			}
			got = append(got, intent.ID)
			after = intent.ID
		}
		if len(intents) < 2 {
			break
		}
	}
	if want := []string{"rec-a", "rec-b", "rec-c"}; !slices.Equal(got, want) {
		t.Fatalf("recoverable intents = %v, want %v", got, want)
	}
}
