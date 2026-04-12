package pgadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) CreateWebhookEvent(ctx context.Context, input *repository.WebhookEventInput) (*repository.WebhookEvent, error) {
	row, err := a.queries.CreateWebhookEvent(ctx, pggen.CreateWebhookEventParams{
		EventID:          input.EventID,
		MessageType:      input.MessageType,
		EventType:        input.EventType,
		SubscriptionID:   input.SubscriptionID,
		BroadcasterID:    input.BroadcasterID,
		MessageTimestamp: input.MessageTimestamp,
		Payload:          input.Payload,
	})
	if err != nil {
		// On the ON CONFLICT DO NOTHING path pgx returns ErrNoRows because
		// the RETURNING clause yields zero rows. Caller treats this as
		// "already recorded, move on" — return the sentinel so they can.
		return nil, mapErr(err)
	}
	return pgWebhookEventToDomain(row), nil
}

func (a *PGAdapter) GetWebhookEvent(ctx context.Context, id int64) (*repository.WebhookEvent, error) {
	row, err := a.queries.GetWebhookEvent(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgWebhookEventToDomain(row), nil
}

func (a *PGAdapter) GetWebhookEventByEventID(ctx context.Context, eventID string) (*repository.WebhookEvent, error) {
	row, err := a.queries.GetWebhookEventByEventID(ctx, eventID)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgWebhookEventToDomain(row), nil
}

func (a *PGAdapter) MarkWebhookEventProcessed(ctx context.Context, id int64) error {
	return a.queries.MarkWebhookEventProcessed(ctx, id)
}

func (a *PGAdapter) MarkWebhookEventFailed(ctx context.Context, id int64, errMsg string) error {
	return a.queries.MarkWebhookEventFailed(ctx, pggen.MarkWebhookEventFailedParams{
		ID:    id,
		Error: &errMsg,
	})
}

func (a *PGAdapter) ListWebhookEvents(ctx context.Context, limit, offset int) ([]repository.WebhookEvent, error) {
	rows, err := a.queries.ListWebhookEvents(ctx, pggen.ListWebhookEventsParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list webhook events: %w", err)
	}
	return pgWebhookEventsToDomain(rows), nil
}

func (a *PGAdapter) ListWebhookEventsByBroadcaster(ctx context.Context, broadcasterID string, limit, offset int) ([]repository.WebhookEvent, error) {
	bid := broadcasterID
	rows, err := a.queries.ListWebhookEventsByBroadcaster(ctx, pggen.ListWebhookEventsByBroadcasterParams{
		BroadcasterID: &bid,
		Limit:         int32(limit),
		Offset:        int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list webhook events by broadcaster: %w", err)
	}
	return pgWebhookEventsToDomain(rows), nil
}

func (a *PGAdapter) ListWebhookEventsByType(ctx context.Context, eventType string, limit, offset int) ([]repository.WebhookEvent, error) {
	et := eventType
	rows, err := a.queries.ListWebhookEventsByType(ctx, pggen.ListWebhookEventsByTypeParams{
		EventType: &et,
		Limit:     int32(limit),
		Offset:    int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list webhook events by type: %w", err)
	}
	return pgWebhookEventsToDomain(rows), nil
}

func (a *PGAdapter) ListStuckWebhookEvents(ctx context.Context, before time.Time, limit int) ([]repository.WebhookEvent, error) {
	rows, err := a.queries.ListStuckWebhookEvents(ctx, pggen.ListStuckWebhookEventsParams{
		ReceivedAt: before,
		Limit:      int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list stuck webhook events: %w", err)
	}
	return pgWebhookEventsToDomain(rows), nil
}

func (a *PGAdapter) ClearWebhookEventPayload(ctx context.Context, before time.Time) error {
	return a.queries.ClearWebhookEventPayload(ctx, before)
}

func (a *PGAdapter) CountWebhookEvents(ctx context.Context) (int64, error) {
	return a.queries.CountWebhookEvents(ctx)
}

func (a *PGAdapter) CountWebhookEventsByType(ctx context.Context, eventType string) (int64, error) {
	et := eventType
	return a.queries.CountWebhookEventsByType(ctx, &et)
}

func pgWebhookEventToDomain(w pggen.WebhookEvent) *repository.WebhookEvent {
	return &repository.WebhookEvent{
		ID:               w.ID,
		EventID:          w.EventID,
		MessageType:      w.MessageType,
		EventType:        w.EventType,
		SubscriptionID:   w.SubscriptionID,
		BroadcasterID:    w.BroadcasterID,
		MessageTimestamp: w.MessageTimestamp,
		Payload:          w.Payload,
		Status:           w.Status,
		Error:            w.Error,
		ReceivedAt:       w.ReceivedAt,
		ProcessedAt:      w.ProcessedAt,
	}
}

func pgWebhookEventsToDomain(rows []pggen.WebhookEvent) []repository.WebhookEvent {
	out := make([]repository.WebhookEvent, len(rows))
	for i, r := range rows {
		out[i] = *pgWebhookEventToDomain(r)
	}
	return out
}
