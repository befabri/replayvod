package contracttest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testSetSchedulesPausedRoundTripAndIsolation(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	saved, err := repo.SetSchedulesPaused(ctx, true)
	if err != nil {
		t.Fatalf("SetSchedulesPaused(true): %v", err)
	}
	if !saved.SchedulesPaused {
		t.Fatal("SetSchedulesPaused(true) returned SchedulesPaused=false")
	}
	if reloaded, err := repo.GetServerSettings(ctx); err != nil {
		t.Fatalf("GetServerSettings: %v", err)
	} else if !reloaded.SchedulesPaused {
		t.Fatal("schedules_paused not persisted")
	}

	if off, err := repo.SetSchedulesPaused(ctx, false); err != nil {
		t.Fatalf("SetSchedulesPaused(false): %v", err)
	} else if off.SchedulesPaused {
		t.Fatal("SetSchedulesPaused(false) left the flag on")
	}

	// Isolation: pausing must not clobber an unrelated setting.
	if _, err := repo.UpsertServerSettings(ctx, &repository.ServerSettings{ServerMode: "relay"}); err != nil {
		t.Fatalf("UpsertServerSettings: %v", err)
	}
	if _, err := repo.SetSchedulesPaused(ctx, true); err != nil {
		t.Fatalf("SetSchedulesPaused after upsert: %v", err)
	}
	afterPause, err := repo.GetServerSettings(ctx)
	if err != nil {
		t.Fatalf("GetServerSettings: %v", err)
	}
	if !afterPause.SchedulesPaused {
		t.Fatal("pause flag lost")
	}
	if afterPause.ServerMode != "relay" {
		t.Fatalf("ServerMode = %q, want relay (pause write clobbered it)", afterPause.ServerMode)
	}

	// And an unrelated settings write must leave the pause flag on.
	if _, err := repo.UpsertServerSettings(ctx, &repository.ServerSettings{ServerMode: "direct"}); err != nil {
		t.Fatalf("second UpsertServerSettings: %v", err)
	}
	afterUpsert, err := repo.GetServerSettings(ctx)
	if err != nil {
		t.Fatalf("GetServerSettings: %v", err)
	}
	if !afterUpsert.SchedulesPaused {
		t.Fatal("unrelated settings write clobbered schedules_paused")
	}
	if afterUpsert.ServerMode != "direct" {
		t.Fatalf("ServerMode = %q, want direct", afterUpsert.ServerMode)
	}
}

func testServerHMACSecretPreservedAcrossUpsert(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	if got, err := repo.GetServerHMACSecret(ctx); err != nil || got != "" {
		t.Fatalf("GetServerHMACSecret on empty = (%q, %v), want (\"\", nil)", got, err)
	}

	if err := repo.EnsureServerHMACSecret(ctx, "secret-one"); err != nil {
		t.Fatalf("EnsureServerHMACSecret: %v", err)
	}
	if err := repo.EnsureServerHMACSecret(ctx, "secret-two"); err != nil {
		t.Fatalf("EnsureServerHMACSecret (second): %v", err)
	}
	if got, _ := repo.GetServerHMACSecret(ctx); got != "secret-one" {
		t.Fatalf("hmac after second Ensure = %q, want secret-one (CAS must not overwrite)", got)
	}

	if _, err := repo.UpsertServerSettings(ctx, &repository.ServerSettings{ServerMode: "poll"}); err != nil {
		t.Fatalf("UpsertServerSettings: %v", err)
	}
	if got, _ := repo.GetServerHMACSecret(ctx); got != "secret-one" {
		t.Fatalf("hmac after UpsertServerSettings = %q, want secret-one (UI save must preserve it)", got)
	}
}

func testRecordingWebhookSecretEnsureIsCASSetIsUnconditional(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	if err := repo.EnsureRecordingWebhookSecret(ctx, "first"); err != nil {
		t.Fatalf("EnsureRecordingWebhookSecret: %v", err)
	}
	if row, _ := repo.GetServerSettings(ctx); row.RecordingWebhookSecret != "first" {
		t.Fatalf("ensure should seed an empty slot, got %q", row.RecordingWebhookSecret)
	}
	if err := repo.EnsureRecordingWebhookSecret(ctx, "second"); err != nil {
		t.Fatalf("EnsureRecordingWebhookSecret (2): %v", err)
	}
	if row, _ := repo.GetServerSettings(ctx); row.RecordingWebhookSecret != "first" {
		t.Fatalf("ensure must not overwrite an existing secret, got %q", row.RecordingWebhookSecret)
	}
	if err := repo.SetRecordingWebhookSecret(ctx, "rotated"); err != nil {
		t.Fatalf("SetRecordingWebhookSecret: %v", err)
	}
	if row, _ := repo.GetServerSettings(ctx); row.RecordingWebhookSecret != "rotated" {
		t.Fatalf("set should rotate, got %q", row.RecordingWebhookSecret)
	}
	if _, err := repo.UpsertRecordingWebhookConfig(ctx, false, "", ""); err != nil {
		t.Fatalf("UpsertRecordingWebhookConfig: %v", err)
	}
	if row, _ := repo.GetServerSettings(ctx); row.RecordingWebhookSecret != "rotated" {
		t.Fatalf("config save wiped the secret, got %q", row.RecordingWebhookSecret)
	}
}

func testRecordingWebhookConfigRoundTrip(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	saved, err := repo.UpsertRecordingWebhookConfig(ctx, true,
		"https://hooks.example/recordings", "recording.completed,recording.failed")
	if err != nil {
		t.Fatalf("UpsertRecordingWebhookConfig: %v", err)
	}
	if !saved.RecordingWebhookEnabled {
		t.Fatal("enabled should round-trip as true")
	}
	if saved.RecordingWebhookURL != "https://hooks.example/recordings" {
		t.Fatalf("url = %q", saved.RecordingWebhookURL)
	}
	if saved.RecordingWebhookEvents != "recording.completed,recording.failed" {
		t.Fatalf("events = %q", saved.RecordingWebhookEvents)
	}
	if saved.RecordingWebhookSecret != "" {
		t.Fatalf("config upsert must not set a secret, got %q", saved.RecordingWebhookSecret)
	}

	// Re-read through GetServerSettings (SELECT *) to confirm the columns are
	// readable by the path the config service and dispatcher use, and that the
	// enabled bool maps back from its stored representation.
	row, err := repo.GetServerSettings(ctx)
	if err != nil {
		t.Fatalf("GetServerSettings: %v", err)
	}
	if !row.RecordingWebhookEnabled || row.RecordingWebhookURL != "https://hooks.example/recordings" {
		t.Fatalf("GetServerSettings did not reflect webhook config: %+v", row)
	}

	disabled, err := repo.UpsertRecordingWebhookConfig(ctx, false, "", "")
	if err != nil {
		t.Fatalf("UpsertRecordingWebhookConfig (disable): %v", err)
	}
	if disabled.RecordingWebhookEnabled {
		t.Fatal("enabled should round-trip as false")
	}
}

func testRecordingWebhookConfigPreservedAcrossServerModeUpsert(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	if err := repo.EnsureServerHMACSecret(ctx, "hmac-keep"); err != nil {
		t.Fatalf("EnsureServerHMACSecret: %v", err)
	}
	if _, err := repo.UpsertRecordingWebhookConfig(ctx, true, "https://hooks.example/x", "recording.failed"); err != nil {
		t.Fatalf("UpsertRecordingWebhookConfig: %v", err)
	}
	if err := repo.EnsureRecordingWebhookSecret(ctx, "webhook-keep"); err != nil {
		t.Fatalf("EnsureRecordingWebhookSecret: %v", err)
	}

	if _, err := repo.UpsertServerSettings(ctx, &repository.ServerSettings{ServerMode: "poll"}); err != nil {
		t.Fatalf("UpsertServerSettings: %v", err)
	}
	row, _ := repo.GetServerSettings(ctx)
	if !row.RecordingWebhookEnabled || row.RecordingWebhookURL != "https://hooks.example/x" || row.RecordingWebhookSecret != "webhook-keep" {
		t.Fatalf("server-mode save clobbered webhook config: %+v", row)
	}

	if _, err := repo.UpsertRecordingWebhookConfig(ctx, true, "https://hooks.example/y", ""); err != nil {
		t.Fatalf("UpsertRecordingWebhookConfig (2): %v", err)
	}
	row, _ = repo.GetServerSettings(ctx)
	if row.ServerMode != "poll" {
		t.Fatalf("webhook save clobbered server_mode: %q", row.ServerMode)
	}
	if row.RecordingWebhookSecret != "webhook-keep" {
		t.Fatalf("webhook config save clobbered the webhook secret: %q", row.RecordingWebhookSecret)
	}
	if got, _ := repo.GetServerHMACSecret(ctx); got != "hmac-keep" {
		t.Fatalf("webhook save clobbered hmac secret: %q", got)
	}
}

func testCreateClaimedRecordingWebhookDeliveryNotClaimable(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	now := time.Now().UTC().Truncate(time.Second)

	claimed, err := repo.CreateClaimedRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
		MessageID: "test-msg", DedupeKey: "test:abc", Event: "recording.test", Test: true, NextAttemptAt: now,
	})
	if err != nil {
		t.Fatalf("CreateClaimedRecordingWebhookDelivery: %v", err)
	}
	if claimed.Status != repository.RecordingWebhookDeliveryDelivering || claimed.Attempts != 1 {
		t.Fatalf("claimed row = %+v, want status=delivering attempts=1", claimed)
	}
	got, err := repo.ClaimDueRecordingWebhookDeliveries(ctx, now, 10)
	if err != nil {
		t.Fatalf("ClaimDueRecordingWebhookDeliveries: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a pre-claimed row must not be claimable by the poller, got %+v", got)
	}

	if _, err := repo.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
		MessageID: "pending-msg", DedupeKey: "recording.completed:7", Event: "recording.completed", VideoID: 7, NextAttemptAt: now,
	}); err != nil {
		t.Fatalf("CreateRecordingWebhookDelivery: %v", err)
	}
	got, err = repo.ClaimDueRecordingWebhookDeliveries(ctx, now, 10)
	if err != nil {
		t.Fatalf("ClaimDueRecordingWebhookDeliveries (2): %v", err)
	}
	if len(got) != 1 || got[0].VideoID != 7 {
		t.Fatalf("only the pending row should be claimable, got %+v", got)
	}
}

func testMarkVideoDoneAndEnqueueRecordingWebhookConditionalAndDedupe(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	if _, err := repo.UpsertRecordingWebhookConfig(ctx, true, "https://hooks.example/x", "recording.completed"); err != nil {
		t.Fatalf("UpsertRecordingWebhookConfig: %v", err)
	}
	if err := repo.EnsureRecordingWebhookSecret(ctx, "secret"); err != nil {
		t.Fatalf("EnsureRecordingWebhookSecret: %v", err)
	}
	video := createWebhookOutboxVideo(t, repo, "job-webhook-done")
	input := &repository.RecordingWebhookDeliveryInput{
		MessageID:     "msg-terminal",
		DedupeKey:     "recording.completed:1",
		Event:         "recording.completed",
		VideoID:       video.ID,
		NextAttemptAt: time.Now().UTC(),
	}
	if err := repo.MarkVideoDoneAndEnqueueRecordingWebhook(ctx, video.ID, 60, 1024, nil, repository.CompletionKindComplete, false, input); err != nil {
		t.Fatalf("MarkVideoDoneAndEnqueueRecordingWebhook: %v", err)
	}
	if err := repo.MarkVideoDoneAndEnqueueRecordingWebhook(ctx, video.ID, 60, 1024, nil, repository.CompletionKindComplete, false, input); err != nil {
		t.Fatalf("MarkVideoDoneAndEnqueueRecordingWebhook duplicate: %v", err)
	}
	rows, err := repo.ListRecordingWebhookDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecordingWebhookDeliveries: %v", err)
	}
	if len(rows) != 1 || rows[0].Event != "recording.completed" || rows[0].VideoID != video.ID {
		t.Fatalf("expected one deduped completed delivery, got %+v", rows)
	}

	failedVideo := createWebhookOutboxVideo(t, repo, "job-webhook-failed")
	failedInput := &repository.RecordingWebhookDeliveryInput{
		MessageID:     "msg-failed",
		DedupeKey:     "recording.failed:2",
		Event:         "recording.failed",
		VideoID:       failedVideo.ID,
		NextAttemptAt: time.Now().UTC(),
	}
	if err := repo.MarkVideoFailedAndEnqueueRecordingWebhook(ctx, failedVideo.ID, "boom", repository.CompletionKindComplete, true, failedInput); err != nil {
		t.Fatalf("MarkVideoFailedAndEnqueueRecordingWebhook: %v", err)
	}
	rows, err = repo.ListRecordingWebhookDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecordingWebhookDeliveries: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("failed event outside allowlist should not enqueue, got %+v", rows)
	}
}

func testRecordingWebhookDeliveryOutboxLifecycle(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	now := time.Now().UTC().Truncate(time.Second)

	created, err := repo.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
		MessageID:     "msg-1",
		DedupeKey:     "recording.completed:42",
		Event:         "recording.completed",
		VideoID:       42,
		NextAttemptAt: now,
	})
	if err != nil {
		t.Fatalf("CreateRecordingWebhookDelivery: %v", err)
	}
	if created.Status != repository.RecordingWebhookDeliveryPending {
		t.Fatalf("status = %q, want pending", created.Status)
	}

	claimed, err := repo.ClaimDueRecordingWebhookDeliveries(ctx, now, 1)
	if err != nil {
		t.Fatalf("ClaimDueRecordingWebhookDeliveries: %v", err)
	}
	if len(claimed) != 1 || claimed[0].Attempts != 1 || claimed[0].Status != repository.RecordingWebhookDeliveryDelivering {
		t.Fatalf("unexpected claim: %+v", claimed)
	}

	next := now.Add(time.Minute)
	if err := repo.MarkRecordingWebhookDeliveryFinal(ctx, created.ID, repository.RecordingWebhookDeliveryPending, 503, "HTTP 503 after 1 attempts", next, now); err != nil {
		t.Fatalf("MarkRecordingWebhookDeliveryFinal: %v", err)
	}
	claimed, err = repo.ClaimDueRecordingWebhookDeliveries(ctx, now.Add(30*time.Second), 1)
	if err != nil {
		t.Fatalf("Claim before next due: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("delivery should not be due before backoff, got %+v", claimed)
	}

	claimed, err = repo.ClaimDueRecordingWebhookDeliveries(ctx, next, 1)
	if err != nil {
		t.Fatalf("Claim after next due: %v", err)
	}
	if len(claimed) != 1 || claimed[0].Attempts != 2 {
		t.Fatalf("second claim should increment attempts, got %+v", claimed)
	}
	if err := repo.MarkRecordingWebhookDeliveryDelivered(ctx, created.ID, 204, next); err != nil {
		t.Fatalf("MarkRecordingWebhookDeliveryDelivered: %v", err)
	}
	rows, err := repo.ListRecordingWebhookDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecordingWebhookDeliveries: %v", err)
	}
	if len(rows) != 1 || rows[0].Status != repository.RecordingWebhookDeliveryDelivered || rows[0].LastStatus != 204 || rows[0].DeliveredAt == nil {
		t.Fatalf("unexpected final row: %+v", rows)
	}
}

func testRetryRecordingWebhookDeliveryOnlyFailedOrRejected(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	now := time.Now().UTC().Truncate(time.Second)

	mk := func(dk string, vid int64) *repository.RecordingWebhookDelivery {
		row, err := repo.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
			MessageID: dk, DedupeKey: dk, Event: "recording.completed", VideoID: vid, NextAttemptAt: now,
		})
		if err != nil {
			t.Fatalf("create %s: %v", dk, err)
		}
		return row
	}

	d1 := mk("recording.completed:1", 1)
	if _, err := repo.ClaimDueRecordingWebhookDeliveries(ctx, now, 1); err != nil {
		t.Fatalf("claim d1: %v", err)
	}
	if err := repo.MarkRecordingWebhookDeliveryDelivered(ctx, d1.ID, 200, now); err != nil {
		t.Fatalf("deliver d1: %v", err)
	}
	if _, err := repo.RetryRecordingWebhookDelivery(ctx, d1.ID, now); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("retry of a delivered row = %v, want ErrNotFound", err)
	}

	d2 := mk("recording.completed:2", 2)
	if _, err := repo.ClaimDueRecordingWebhookDeliveries(ctx, now, 1); err != nil {
		t.Fatalf("claim d2: %v", err)
	}
	if err := repo.MarkRecordingWebhookDeliveryFinal(ctx, d2.ID, repository.RecordingWebhookDeliveryFailed, 500, "boom", now, now); err != nil {
		t.Fatalf("fail d2: %v", err)
	}
	retried, err := repo.RetryRecordingWebhookDelivery(ctx, d2.ID, now)
	if err != nil {
		t.Fatalf("retry of a failed row: %v", err)
	}
	if retried.Status != repository.RecordingWebhookDeliveryPending || retried.Attempts != 0 || retried.LastStatus != 0 {
		t.Fatalf("retry should reset to pending/attempts=0/last_status=0, got %+v", retried)
	}

	if _, err := repo.RetryRecordingWebhookDelivery(ctx, 999999, now); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("retry of a missing id = %v, want ErrNotFound", err)
	}
}

func testResetStaleRecordingWebhookDeliveries(t *testing.T, h Harness) {
	ctx := context.Background()

	t.Run("re-arms only stale delivering rows, leaves terminal and pending alone", func(t *testing.T) {
		repo := h.Repo()

		stale, err := repo.CreateClaimedRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
			MessageID: "stale", DedupeKey: "stale", Event: "recording.completed", VideoID: 1,
		})
		if err != nil {
			t.Fatalf("create stale delivering: %v", err)
		}
		if stale.Status != repository.RecordingWebhookDeliveryDelivering {
			t.Fatalf("precondition: stale row status = %q, want delivering", stale.Status)
		}

		pending, err := repo.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
			MessageID: "pending", DedupeKey: "pending", Event: "recording.completed", VideoID: 2,
		})
		if err != nil {
			t.Fatalf("create pending: %v", err)
		}

		failed, err := repo.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
			MessageID: "failed", DedupeKey: "failed", Event: "recording.completed", VideoID: 3,
		})
		if err != nil {
			t.Fatalf("create failed: %v", err)
		}
		fin := time.Now().UTC().Truncate(time.Second)
		if err := repo.MarkRecordingWebhookDeliveryFinal(ctx, failed.ID, repository.RecordingWebhookDeliveryFailed, 500, "boom", fin, fin); err != nil {
			t.Fatalf("mark failed: %v", err)
		}

		delivered, err := repo.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
			MessageID: "delivered", DedupeKey: "delivered", Event: "recording.completed", VideoID: 4, NextAttemptAt: fin,
		})
		if err != nil {
			t.Fatalf("create delivered: %v", err)
		}
		if _, err := repo.ClaimDueRecordingWebhookDeliveries(ctx, fin, 1); err != nil {
			t.Fatalf("claim delivered: %v", err)
		}
		if err := repo.MarkRecordingWebhookDeliveryDelivered(ctx, delivered.ID, 204, fin); err != nil {
			t.Fatalf("mark delivered: %v", err)
		}

		resetNow := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
		before := time.Now().UTC().Add(24 * time.Hour)
		if err := repo.ResetStaleRecordingWebhookDeliveries(ctx, before, resetNow); err != nil {
			t.Fatalf("ResetStaleRecordingWebhookDeliveries: %v", err)
		}

		byID := deliveriesByID(t, repo, ctx)
		if got := byID[stale.ID].Status; got != repository.RecordingWebhookDeliveryPending {
			t.Fatalf("stale delivering row status = %q, want pending (re-armed)", got)
		}
		if got := byID[stale.ID].NextAttemptAt; !got.Equal(resetNow) {
			t.Fatalf("re-armed row next_attempt_at = %v, want %v (due immediately)", got, resetNow)
		}
		if got := byID[pending.ID].Status; got != repository.RecordingWebhookDeliveryPending {
			t.Fatalf("pending row status = %q, want pending (untouched)", got)
		}
		if got := byID[failed.ID].Status; got != repository.RecordingWebhookDeliveryFailed {
			t.Fatalf("failed row status = %q, want failed (untouched)", got)
		}
		if got := byID[delivered.ID].Status; got != repository.RecordingWebhookDeliveryDelivered {
			t.Fatalf("delivered row status = %q, want delivered (untouched)", got)
		}
	})

	t.Run("leaves fresh delivering rows untouched", func(t *testing.T) {
		repo := h.Repo()
		fresh, err := repo.CreateClaimedRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
			MessageID: "fresh", DedupeKey: "fresh", Event: "recording.completed", VideoID: 1,
		})
		if err != nil {
			t.Fatalf("create fresh delivering: %v", err)
		}
		before := time.Now().UTC().Add(-24 * time.Hour)
		if err := repo.ResetStaleRecordingWebhookDeliveries(ctx, before, time.Now().UTC()); err != nil {
			t.Fatalf("ResetStaleRecordingWebhookDeliveries: %v", err)
		}
		if got := deliveriesByID(t, repo, ctx)[fresh.ID].Status; got != repository.RecordingWebhookDeliveryDelivering {
			t.Fatalf("fresh delivering row status = %q, want delivering (not yet stale)", got)
		}
	})
}

func testManualDeleteQueueWaitsForWebhookFrozenParts(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-delete", BroadcasterLogin: "delete", BroadcasterName: "Delete",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	mkDone := func(jobID string) *repository.Video {
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: "Delete", Status: repository.VideoStatusPending,
			Quality: repository.QualityHigh, BroadcasterID: "bc-delete", RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("create %s: %v", jobID, err)
		}
		if err := repo.MarkVideoDone(ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
			t.Fatalf("mark done %s: %v", jobID, err)
		}
		return v
	}
	ready := mkDone("job-delete-ready")
	blocked := mkDone("job-delete-blocked")
	pending, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-delete-pending", Filename: "job-delete-pending", DisplayName: "Delete",
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		BroadcasterID: "bc-delete", RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("create pending: %v", err)
	}

	queued, err := repo.RequestVideoDelete(ctx, ready.ID)
	if err != nil {
		t.Fatalf("RequestVideoDelete ready: %v", err)
	}
	if queued.DeleteRequestedAt == nil {
		t.Fatal("ready DeleteRequestedAt is nil")
	}
	queuedAgain, err := repo.RequestVideoDelete(ctx, ready.ID)
	if err != nil {
		t.Fatalf("RequestVideoDelete ready again: %v", err)
	}
	if queuedAgain.DeleteRequestedAt == nil || !queuedAgain.DeleteRequestedAt.Equal(*queued.DeleteRequestedAt) {
		t.Fatalf("RequestVideoDelete not idempotent: first %v second %v", queued.DeleteRequestedAt, queuedAgain.DeleteRequestedAt)
	}
	if _, err := repo.RequestVideoDelete(ctx, pending.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("RequestVideoDelete pending err = %v, want ErrNotFound", err)
	}
	if _, err := repo.RequestVideoDelete(ctx, blocked.ID); err != nil {
		t.Fatalf("RequestVideoDelete blocked: %v", err)
	}
	delivery, err := repo.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
		MessageID:     "msg-delete-blocked",
		DedupeKey:     "dedupe-delete-blocked",
		Event:         "recording.completed",
		VideoID:       blocked.ID,
		NextAttemptAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateRecordingWebhookDelivery: %v", err)
	}

	rows, err := repo.ListVideosPendingManualDelete(ctx, 10)
	if err != nil {
		t.Fatalf("ListVideosPendingManualDelete before freeze: %v", err)
	}
	assertStringSlice(t, videoJobIDs(rows), []string{"job-delete-ready"})

	if err := repo.SoftDeleteVideo(ctx, ready.ID, repository.DeletionKindManual); err != nil {
		t.Fatalf("soft delete ready: %v", err)
	}
	readyGone, err := repo.GetVideo(ctx, ready.ID)
	if err != nil {
		t.Fatalf("GetVideo ready after soft delete: %v", err)
	}
	if readyGone.DeleteRequestedAt != nil {
		t.Fatalf("ready DeleteRequestedAt after tombstone = %v, want nil", readyGone.DeleteRequestedAt)
	}
	if err := repo.SetRecordingWebhookDeliveryFrozenParts(ctx, delivery.ID, "[]"); err != nil {
		t.Fatalf("SetRecordingWebhookDeliveryFrozenParts: %v", err)
	}
	rows, err = repo.ListVideosPendingManualDelete(ctx, 10)
	if err != nil {
		t.Fatalf("ListVideosPendingManualDelete after freeze: %v", err)
	}
	assertStringSlice(t, videoJobIDs(rows), []string{"job-delete-blocked"})

	race := mkDone("job-delete-race")
	if _, err := repo.RequestVideoDelete(ctx, race.ID); err != nil {
		t.Fatalf("RequestVideoDelete race: %v", err)
	}
	if err := repo.SoftDeleteVideo(ctx, race.ID, repository.DeletionKindRetention); err != nil {
		t.Fatalf("soft delete race as retention: %v", err)
	}
	raceGone, err := repo.GetVideo(ctx, race.ID)
	if err != nil {
		t.Fatalf("GetVideo race after soft delete: %v", err)
	}
	if raceGone.DeleteRequestedAt != nil {
		t.Fatalf("race DeleteRequestedAt after tombstone = %v, want nil", raceGone.DeleteRequestedAt)
	}
	if raceGone.DeletionKind == nil || *raceGone.DeletionKind != repository.DeletionKindManual {
		t.Fatalf("race DeletionKind = %v, want %q because manual intent wins", raceGone.DeletionKind, repository.DeletionKindManual)
	}

	failed, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-delete-failed", Filename: "job-delete-failed", DisplayName: "Delete",
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		BroadcasterID: "bc-delete", RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("create failed terminal: %v", err)
	}
	if err := repo.MarkVideoFailed(ctx, failed.ID, "seed-failed", repository.CompletionKindPartial, true); err != nil {
		t.Fatalf("MarkVideoFailed failed terminal: %v", err)
	}
	failedQueued, err := repo.RequestVideoDelete(ctx, failed.ID)
	if err != nil {
		t.Fatalf("RequestVideoDelete failed terminal: %v", err)
	}
	if failedQueued.DeleteRequestedAt == nil {
		t.Fatal("failed terminal DeleteRequestedAt is nil")
	}
}

// --- helpers ---

func createWebhookOutboxVideo(t *testing.T, repo repository.Repository, jobID string) *repository.Video {
	t.Helper()
	ctx := context.Background()
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID:    "broadcaster",
		BroadcasterLogin: "streamer",
		BroadcasterName:  "Streamer",
	}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID:         jobID,
		Filename:      jobID + ".mp4",
		DisplayName:   "Streamer",
		Status:        repository.VideoStatusRunning,
		Quality:       repository.QualityHigh,
		BroadcasterID: "broadcaster",
		ViewerCount:   1,
		Language:      "en",
	})
	if err != nil {
		t.Fatalf("CreateVideo: %v", err)
	}
	return v
}

func deliveriesByID(t *testing.T, repo repository.Repository, ctx context.Context) map[int64]repository.RecordingWebhookDelivery {
	t.Helper()
	rows, err := repo.ListRecordingWebhookDeliveries(ctx, 100)
	if err != nil {
		t.Fatalf("ListRecordingWebhookDeliveries: %v", err)
	}
	out := make(map[int64]repository.RecordingWebhookDelivery, len(rows))
	for _, r := range rows {
		out[r.ID] = r
	}
	return out
}

func videoJobIDs(videos []repository.Video) []string {
	out := make([]string, len(videos))
	for i, v := range videos {
		out[i] = v.JobID
	}
	return out
}
