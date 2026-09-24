package contracttest

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testWebhookEventProcessing(t *testing.T, h Harness) {
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
	if got, err := repo.GetWebhookEventByEventID(ctx, "evt-2"); err != nil || got.ID != e2.ID || got.EventType == nil || *got.EventType != offline || got.Status != repository.WebhookStatusReceived {
		t.Fatalf("event by id = %+v, %v", got, err)
	}
	if _, err := repo.GetWebhookEventByEventID(ctx, "evt-missing"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing event id: %v", err)
	}
	if err := repo.MarkWebhookEventProcessed(ctx, e1.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetWebhookEventByEventID(ctx, e1.EventID); err != nil || got.Status != repository.WebhookStatusProcessed || got.ProcessedAt == nil || got.Error != nil {
		t.Fatalf("processed event = %+v, %v", got, err)
	}
	if err := repo.MarkWebhookEventFailed(ctx, e2.ID, "boom"); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetWebhookEventByEventID(ctx, e2.EventID); err != nil || got.Status != repository.WebhookStatusFailed || got.ProcessedAt == nil || got.Error == nil || *got.Error != "boom" {
		t.Fatalf("failed event = %+v, %v", got, err)
	}
	if err := repo.MarkWebhookEventProcessed(ctx, e2.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetWebhookEventByEventID(ctx, e2.EventID); err != nil || got.Status != repository.WebhookStatusProcessed || got.Error != nil {
		t.Fatalf("reprocessed event keeps its failure: %+v, %v", got, err)
	}
}
