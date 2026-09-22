package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

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
