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

func (a *PGAdapter) GetSubscription(ctx context.Context, id string) (*repository.Subscription, error) {
	row, err := a.queries.GetSubscription(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgSubscriptionToDomain(row), nil
}

func (a *PGAdapter) GetActiveSubscriptionForBroadcasterType(ctx context.Context, broadcasterID, subType string) (*repository.Subscription, error) {
	// sqlc generates broadcaster_id as *string (nullable column); pass by pointer.
	bid := broadcasterID
	row, err := a.queries.GetActiveSubscriptionForBroadcasterType(ctx, pggen.GetActiveSubscriptionForBroadcasterTypeParams{
		BroadcasterID: &bid,
		Type:          subType,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return pgSubscriptionToDomain(row), nil
}

func (a *PGAdapter) ListActiveSubscriptions(ctx context.Context, limit, offset int) ([]repository.Subscription, error) {
	rows, err := a.queries.ListActiveSubscriptions(ctx, pggen.ListActiveSubscriptionsParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list active subscriptions: %w", err)
	}
	return pgSubscriptionsToDomain(rows), nil
}

func (a *PGAdapter) ListSubscriptionsByBroadcaster(ctx context.Context, broadcasterID string) ([]repository.Subscription, error) {
	bid := broadcasterID
	rows, err := a.queries.ListSubscriptionsByBroadcaster(ctx, &bid)
	if err != nil {
		return nil, fmt.Errorf("pg list subs by broadcaster: %w", err)
	}
	return pgSubscriptionsToDomain(rows), nil
}

func (a *PGAdapter) ListSubscriptionsByType(ctx context.Context, subType string) ([]repository.Subscription, error) {
	rows, err := a.queries.ListSubscriptionsByType(ctx, subType)
	if err != nil {
		return nil, fmt.Errorf("pg list subs by type: %w", err)
	}
	return pgSubscriptionsToDomain(rows), nil
}

func (a *PGAdapter) UpdateSubscriptionStatus(ctx context.Context, id, status string) error {
	return a.queries.UpdateSubscriptionStatus(ctx, pggen.UpdateSubscriptionStatusParams{
		ID:     id,
		Status: status,
	})
}

func (a *PGAdapter) MarkSubscriptionRevoked(ctx context.Context, id, reason string) error {
	return a.queries.MarkSubscriptionRevoked(ctx, pggen.MarkSubscriptionRevokedParams{
		ID:            id,
		RevokedReason: &reason,
	})
}

func (a *PGAdapter) DeleteSubscription(ctx context.Context, id string) error {
	return a.queries.DeleteSubscription(ctx, id)
}

func (a *PGAdapter) CountActiveSubscriptions(ctx context.Context) (int64, error) {
	return a.queries.CountActiveSubscriptions(ctx)
}

func pgSubscriptionToDomain(s pggen.Subscription) *repository.Subscription {
	return &repository.Subscription{
		ID:                s.ID,
		Status:            s.Status,
		Type:              s.Type,
		Version:           s.Version,
		Cost:              int64(s.Cost),
		Condition:         s.Condition,
		BroadcasterID:     s.BroadcasterID,
		TransportMethod:   s.TransportMethod,
		TransportCallback: s.TransportCallback,
		TwitchCreatedAt:   s.TwitchCreatedAt,
		CreatedAt:         s.CreatedAt,
		RevokedAt:         s.RevokedAt,
		RevokedReason:     s.RevokedReason,
	}
}

func pgSubscriptionsToDomain(rows []pggen.Subscription) []repository.Subscription {
	out := make([]repository.Subscription, len(rows))
	for i, r := range rows {
		out[i] = *pgSubscriptionToDomain(r)
	}
	return out
}
