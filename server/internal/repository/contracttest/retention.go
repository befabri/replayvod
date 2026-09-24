package contracttest

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testDeleteOldFetchLogsPrunesByFetchedAt(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	for _, fetchType := range []string{"stale", "stale", "fresh"} {
		if err := repo.CreateFetchLog(ctx, &repository.FetchLogInput{FetchType: fetchType, Status: 200, DurationMs: 1}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	h.BackdateFetchLogsByType(t, "stale", now.Add(-48*time.Hour))
	count := func(want int64, when string) {
		t.Helper()
		if got, err := repo.CountFetchLogs(ctx); err != nil || got != want {
			t.Fatalf("fetch logs %s = %d, %v, want %d", when, got, err, want)
		}
	}
	if err := repo.DeleteOldFetchLogs(ctx, now.Add(-72*time.Hour)); err != nil {
		t.Fatal(err)
	}
	count(3, "after a cutoff older than every row")
	if err := repo.DeleteOldFetchLogs(ctx, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	count(1, "after pruning stale rows")
	if stale, err := repo.CountFetchLogsByType(ctx, "stale"); err != nil || stale != 0 {
		t.Fatalf("stale rows survived: %d, %v", stale, err)
	}
	if rows, err := repo.ListFetchLogs(ctx, 10, 0); err != nil || len(rows) != 1 || rows[0].FetchType != "fresh" {
		t.Fatalf("remaining rows = %+v, %v", rows, err)
	}
	if err := repo.DeleteOldFetchLogs(ctx, now.Add(-24*time.Hour)); err != nil {
		t.Fatalf("repeated prune: %v", err)
	}
	count(1, "after a repeated prune")
	if err := repo.DeleteOldFetchLogs(ctx, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	count(0, "after a future cutoff")
	if err := repo.DeleteOldFetchLogs(ctx, now.Add(time.Hour)); err != nil {
		t.Fatalf("pruning an empty table: %v", err)
	}
}

func testClearWebhookEventPayloadKeepsAuditRows(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	now := time.Now().UTC()
	create := func(id string) *repository.WebhookEvent {
		t.Helper()
		created, err := repo.CreateWebhookEvent(ctx, &repository.WebhookEventInput{
			EventID: id, MessageType: repository.WebhookMessageNotification, MessageTimestamp: now, Payload: json.RawMessage(`{"event":"` + id + `"}`),
		})
		if err != nil {
			t.Fatal(err)
		}
		return created
	}
	stale, fresh := create("payload-stale"), create("payload-fresh")
	received := now.Add(-48 * time.Hour).Truncate(time.Second)
	h.BackdateWebhookEventReceived(t, stale.ID, received)
	payloadOf := func(event *repository.WebhookEvent) json.RawMessage {
		t.Helper()
		row, err := repo.GetWebhookEventByEventID(ctx, event.EventID)
		if err != nil {
			t.Fatal(err)
		}
		return row.Payload
	}
	if err := repo.ClearWebhookEventPayload(ctx, now.Add(-72*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(payloadOf(stale)) == 0 || len(payloadOf(fresh)) == 0 {
		t.Fatal("cutoff older than every row cleared a payload")
	}
	if err := repo.ClearWebhookEventPayload(ctx, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	trimmed, err := repo.GetWebhookEventByEventID(ctx, stale.EventID)
	if err != nil {
		t.Fatal(err)
	}
	if len(trimmed.Payload) != 0 {
		t.Fatalf("stale payload survived: %s", trimmed.Payload)
	}
	if trimmed.EventID != stale.EventID || trimmed.MessageType != stale.MessageType || trimmed.Status != stale.Status || !trimmed.ReceivedAt.Equal(received) {
		t.Fatalf("trim changed audit columns: %+v", trimmed)
	}
	if !jsonEqual(payloadOf(fresh), json.RawMessage(`{"event":"payload-fresh"}`)) {
		t.Fatalf("fresh payload changed: %s", payloadOf(fresh))
	}
	if err := repo.ClearWebhookEventPayload(ctx, now.Add(-24*time.Hour)); err != nil {
		t.Fatalf("repeated trim: %v", err)
	}
	if n, err := repo.CountWebhookEvents(ctx); err != nil || n != 2 {
		t.Fatalf("rows after trim = %d, %v", n, err)
	}
	if err := repo.ClearWebhookEventPayload(ctx, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(payloadOf(fresh)) != 0 {
		t.Fatal("future cutoff kept a payload")
	}
}

func testDeleteOldWebhookEventsRemovesOnlyExpiredRows(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	now := time.Now().UTC()
	for _, id := range []string{"rows-stale", "rows-fresh"} {
		created, err := repo.CreateWebhookEvent(ctx, &repository.WebhookEventInput{
			EventID: id, MessageType: repository.WebhookMessageNotification, MessageTimestamp: now, Payload: json.RawMessage(`{}`),
		})
		if err != nil {
			t.Fatal(err)
		}
		if id == "rows-stale" {
			h.BackdateWebhookEventReceived(t, created.ID, now.Add(-48*time.Hour).Truncate(time.Second))
		}
	}
	if err := repo.DeleteOldWebhookEvents(ctx, now.Add(-72*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n, err := repo.CountWebhookEvents(ctx); err != nil || n != 2 {
		t.Fatalf("rows after a cutoff older than every row = %d, %v", n, err)
	}
	if err := repo.DeleteOldWebhookEvents(ctx, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetWebhookEventByEventID(ctx, "rows-stale"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("stale row = %v, want ErrNotFound", err)
	}
	if _, err := repo.GetWebhookEventByEventID(ctx, "rows-fresh"); err != nil {
		t.Fatalf("fresh row: %v", err)
	}
	if err := repo.DeleteOldWebhookEvents(ctx, now.Add(-24*time.Hour)); err != nil {
		t.Fatalf("repeated sweep: %v", err)
	}
	if n, err := repo.CountWebhookEvents(ctx); err != nil || n != 1 {
		t.Fatalf("rows after sweep = %d, %v", n, err)
	}
}
