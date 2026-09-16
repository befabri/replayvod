package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) CreateSubscription(ctx context.Context, input *repository.SubscriptionInput) (*repository.Subscription, error) {
	row, err := a.queries.CreateSubscription(ctx, pggen.CreateSubscriptionParams{
		ID:                input.ID,
		Status:            input.Status,
		Type:              input.Type,
		Version:           input.Version,
		Cost:              int32(input.Cost),
		Condition:         input.Condition,
		BroadcasterID:     input.BroadcasterID,
		TransportMethod:   input.TransportMethod,
		TransportCallback: input.TransportCallback,
		TwitchCreatedAt:   input.TwitchCreatedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("pg create subscription: %w", err)
	}
	return pgSubscriptionToDomain(row), nil
}

func (a *PGAdapter) UpsertSubscription(ctx context.Context, input *repository.SubscriptionInput) (*repository.Subscription, error) {
	row, err := a.queries.UpsertSubscription(ctx, pggen.UpsertSubscriptionParams{
		ID:                input.ID,
		Status:            input.Status,
		Type:              input.Type,
		Version:           input.Version,
		Cost:              int32(input.Cost),
		Condition:         input.Condition,
		BroadcasterID:     input.BroadcasterID,
		TransportMethod:   input.TransportMethod,
		TransportCallback: input.TransportCallback,
		TwitchCreatedAt:   input.TwitchCreatedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("pg upsert subscription: %w", err)
	}
	return pgSubscriptionToDomain(row), nil
}
