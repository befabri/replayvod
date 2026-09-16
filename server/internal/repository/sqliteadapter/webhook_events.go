package sqliteadapter

import (
	"context"
	"database/sql"

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
		MessageTimestamp: sqliteTime(input.MessageTimestamp),
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
