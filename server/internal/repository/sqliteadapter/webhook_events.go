package sqliteadapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) CreateWebhookEvent(ctx context.Context, input *repository.WebhookEventInput) (*repository.WebhookEvent, error) {
	var payload sql.NullString
	if len(input.Payload) > 0 {
		payload = sql.NullString{String: string(input.Payload), Valid: true}
	}
	row, err := a.queries.CreateWebhookEvent(ctx, sqlitegen.CreateWebhookEventParams{
		EventID:          input.EventID,
		MessageType:      input.MessageType,
		EventType:        stringPtrToNullString(input.EventType),
		SubscriptionID:   stringPtrToNullString(input.SubscriptionID),
		BroadcasterID:    stringPtrToNullString(input.BroadcasterID),
		MessageTimestamp: formatTime(input.MessageTimestamp),
		Payload:          payload,
	})
	if err != nil {
		// ON CONFLICT DO NOTHING + RETURNING yields sql.ErrNoRows when the
		// event was already recorded. mapErr turns that into
		// repository.ErrNotFound so the webhook handler can bail on dedup.
		return nil, mapErr(err)
	}
	return sqliteWebhookEventToDomain(row), nil
}

func (a *SQLiteAdapter) GetWebhookEvent(ctx context.Context, id int64) (*repository.WebhookEvent, error) {
	row, err := a.queries.GetWebhookEvent(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteWebhookEventToDomain(row), nil
}

func (a *SQLiteAdapter) GetWebhookEventByEventID(ctx context.Context, eventID string) (*repository.WebhookEvent, error) {
	row, err := a.queries.GetWebhookEventByEventID(ctx, eventID)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteWebhookEventToDomain(row), nil
}

func (a *SQLiteAdapter) MarkWebhookEventProcessed(ctx context.Context, id int64) error {
	return a.queries.MarkWebhookEventProcessed(ctx, id)
}

func (a *SQLiteAdapter) MarkWebhookEventFailed(ctx context.Context, id int64, errMsg string) error {
	return a.queries.MarkWebhookEventFailed(ctx, sqlitegen.MarkWebhookEventFailedParams{
		ID:    id,
		Error: sql.NullString{String: errMsg, Valid: true},
	})
}

func (a *SQLiteAdapter) ListWebhookEvents(ctx context.Context, limit, offset int) ([]repository.WebhookEvent, error) {
	rows, err := a.queries.ListWebhookEvents(ctx, sqlitegen.ListWebhookEventsParams{
		Limit:  int64(limit),
		Offset: int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list webhook events: %w", err)
	}
	return sqliteWebhookEventsToDomain(rows), nil
}

func (a *SQLiteAdapter) ListWebhookEventsByBroadcaster(ctx context.Context, broadcasterID string, limit, offset int) ([]repository.WebhookEvent, error) {
	rows, err := a.queries.ListWebhookEventsByBroadcaster(ctx, sqlitegen.ListWebhookEventsByBroadcasterParams{
		BroadcasterID: sql.NullString{String: broadcasterID, Valid: true},
		Limit:         int64(limit),
		Offset:        int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list webhook events by broadcaster: %w", err)
	}
	return sqliteWebhookEventsToDomain(rows), nil
}

func (a *SQLiteAdapter) ListWebhookEventsByType(ctx context.Context, eventType string, limit, offset int) ([]repository.WebhookEvent, error) {
	rows, err := a.queries.ListWebhookEventsByType(ctx, sqlitegen.ListWebhookEventsByTypeParams{
		EventType: sql.NullString{String: eventType, Valid: true},
		Limit:     int64(limit),
		Offset:    int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list webhook events by type: %w", err)
	}
	return sqliteWebhookEventsToDomain(rows), nil
}

func (a *SQLiteAdapter) ListStuckWebhookEvents(ctx context.Context, before time.Time, limit int) ([]repository.WebhookEvent, error) {
	rows, err := a.queries.ListStuckWebhookEvents(ctx, sqlitegen.ListStuckWebhookEventsParams{
		ReceivedAt: formatTime(before),
		Limit:      int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list stuck webhook events: %w", err)
	}
	return sqliteWebhookEventsToDomain(rows), nil
}

func (a *SQLiteAdapter) ClearWebhookEventPayload(ctx context.Context, before time.Time) error {
	return a.queries.ClearWebhookEventPayload(ctx, formatTime(before))
}

func (a *SQLiteAdapter) CountWebhookEvents(ctx context.Context) (int64, error) {
	return a.queries.CountWebhookEvents(ctx)
}

func (a *SQLiteAdapter) CountWebhookEventsByType(ctx context.Context, eventType string) (int64, error) {
	return a.queries.CountWebhookEventsByType(ctx, sql.NullString{String: eventType, Valid: true})
}

func sqliteWebhookEventToDomain(w sqlitegen.WebhookEvent) *repository.WebhookEvent {
	var payload json.RawMessage
	if w.Payload.Valid {
		payload = json.RawMessage(w.Payload.String)
	}
	return &repository.WebhookEvent{
		ID:               w.ID,
		EventID:          w.EventID,
		MessageType:      w.MessageType,
		EventType:        fromNullString(w.EventType),
		SubscriptionID:   fromNullString(w.SubscriptionID),
		BroadcasterID:    fromNullString(w.BroadcasterID),
		MessageTimestamp: parseTime(w.MessageTimestamp),
		Payload:          payload,
		Status:           w.Status,
		Error:            fromNullString(w.Error),
		ReceivedAt:       parseTime(w.ReceivedAt),
		ProcessedAt:      parseNullTime(w.ProcessedAt),
	}
}

func sqliteWebhookEventsToDomain(rows []sqlitegen.WebhookEvent) []repository.WebhookEvent {
	out := make([]repository.WebhookEvent, len(rows))
	for i, r := range rows {
		out[i] = *sqliteWebhookEventToDomain(r)
	}
	return out
}
