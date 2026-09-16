package contracttest

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func webhookEventIDs(events []repository.WebhookEvent) map[int64]bool {
	out := make(map[int64]bool, len(events))
	for _, e := range events {
		out[e.ID] = true
	}
	return out
}

func testWebhookEventProcessingAndListing(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	online, offline := "stream.online", "stream.offline"
	bc1, bc2 := "bc-1", "bc-2"
	now := time.Now().UTC().Truncate(time.Second)
	create := func(eventID string, eventType, broadcasterID *string) *repository.WebhookEvent {
		t.Helper()
		e, err := repo.CreateWebhookEvent(ctx, &repository.WebhookEventInput{
			EventID: eventID, MessageType: repository.WebhookMessageNotification, EventType: eventType, BroadcasterID: broadcasterID,
			MessageTimestamp: now, Payload: json.RawMessage(`{"event":"` + eventID + `"}`),
		})
		if err != nil {
			t.Fatalf("create %s: %v", eventID, err)
		}
		return e
	}
	e1 := create("evt-1", &online, &bc1)
	e2 := create("evt-2", &offline, &bc2)
	e3 := create("evt-3", &online, &bc1)
	if got, err := repo.GetWebhookEventByEventID(ctx, "evt-2"); err != nil || got.ID != e2.ID || got.EventType == nil || *got.EventType != offline || got.Status != repository.WebhookStatusReceived {
		t.Fatalf("event by id = %+v, %v", got, err)
	}
	if _, err := repo.GetWebhookEventByEventID(ctx, "evt-missing"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing event id: %v", err)
	}
	for eventType, want := range map[string]int64{online: 2, offline: 1, "channel.update": 0} {
		if n, err := repo.CountWebhookEventsByType(ctx, eventType); err != nil || n != want {
			t.Fatalf("count of %s = %d, %v; want %d", eventType, n, err, want)
		}
	}
	if all, err := repo.ListWebhookEvents(ctx, 10, 0); err != nil || len(all) != 3 {
		t.Fatalf("events = %+v, %v", all, err)
	}
	if page, err := repo.ListWebhookEvents(ctx, 2, 2); err != nil || len(page) != 1 {
		t.Fatalf("last event page = %+v, %v", page, err)
	}
	byBroadcaster, err := repo.ListWebhookEventsByBroadcaster(ctx, bc1, 10, 0)
	if err != nil || len(byBroadcaster) != 2 || !webhookEventIDs(byBroadcaster)[e1.ID] || !webhookEventIDs(byBroadcaster)[e3.ID] {
		t.Fatalf("events of bc-1 = %+v, %v", byBroadcaster, err)
	}
	if byType, err := repo.ListWebhookEventsByType(ctx, offline, 10, 0); err != nil || len(byType) != 1 || byType[0].ID != e2.ID {
		t.Fatalf("offline events = %+v, %v", byType, err)
	}
	if err := repo.MarkWebhookEventProcessed(ctx, e1.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetWebhookEvent(ctx, e1.ID); err != nil || got.Status != repository.WebhookStatusProcessed || got.ProcessedAt == nil || got.Error != nil {
		t.Fatalf("processed event = %+v, %v", got, err)
	}
	if err := repo.MarkWebhookEventFailed(ctx, e2.ID, "boom"); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetWebhookEvent(ctx, e2.ID); err != nil || got.Status != repository.WebhookStatusFailed || got.ProcessedAt == nil || got.Error == nil || *got.Error != "boom" {
		t.Fatalf("failed event = %+v, %v", got, err)
	}
	if err := repo.MarkWebhookEventProcessed(ctx, e2.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetWebhookEvent(ctx, e2.ID); err != nil || got.Status != repository.WebhookStatusProcessed || got.Error != nil {
		t.Fatalf("reprocessed event keeps its failure: %+v, %v", got, err)
	}
	if stuck, err := repo.ListStuckWebhookEvents(ctx, now.Add(time.Hour), 10); err != nil || len(stuck) != 1 || stuck[0].ID != e3.ID {
		t.Fatalf("stuck events = %+v, %v", stuck, err)
	}
	h.BackdateWebhookEventReceived(t, e3.ID, now.Add(-2*time.Hour))
	if stuck, err := repo.ListStuckWebhookEvents(ctx, now.Add(-time.Hour), 10); err != nil || len(stuck) != 1 || stuck[0].ID != e3.ID {
		t.Fatalf("stuck events past the threshold = %+v, %v", stuck, err)
	}
	if stuck, err := repo.ListStuckWebhookEvents(ctx, now.Add(-3*time.Hour), 10); err != nil || len(stuck) != 0 {
		t.Fatalf("events newer than the threshold reported stuck: %+v, %v", stuck, err)
	}
}
