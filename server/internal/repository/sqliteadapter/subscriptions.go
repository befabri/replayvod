package sqliteadapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) CreateSubscription(ctx context.Context, input *repository.SubscriptionInput) (*repository.Subscription, error) {
	row, err := a.queries.CreateSubscription(ctx, sqlitegen.CreateSubscriptionParams{
		ID:                input.ID,
		Status:            input.Status,
		Type:              input.Type,
		Version:           input.Version,
		Cost:              input.Cost,
		Condition:         string(input.Condition), // SQLite stores JSON as TEXT
		BroadcasterID:     stringPtrToNullString(input.BroadcasterID),
		TransportMethod:   input.TransportMethod,
		TransportCallback: input.TransportCallback,
		TwitchCreatedAt:   formatTime(input.TwitchCreatedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite create subscription: %w", err)
	}
	return sqliteSubscriptionToDomain(row), nil
}

func (a *SQLiteAdapter) UpsertSubscription(ctx context.Context, input *repository.SubscriptionInput) (*repository.Subscription, error) {
	row, err := a.queries.UpsertSubscription(ctx, sqlitegen.UpsertSubscriptionParams{
		ID:                input.ID,
		Status:            input.Status,
		Type:              input.Type,
		Version:           input.Version,
		Cost:              input.Cost,
		Condition:         string(input.Condition),
		BroadcasterID:     stringPtrToNullString(input.BroadcasterID),
		TransportMethod:   input.TransportMethod,
		TransportCallback: input.TransportCallback,
		TwitchCreatedAt:   formatTime(input.TwitchCreatedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert subscription: %w", err)
	}
	return sqliteSubscriptionToDomain(row), nil
}

func (a *SQLiteAdapter) GetSubscription(ctx context.Context, id string) (*repository.Subscription, error) {
	row, err := a.queries.GetSubscription(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteSubscriptionToDomain(row), nil
}

func (a *SQLiteAdapter) GetActiveSubscriptionForBroadcasterType(ctx context.Context, broadcasterID, subType string) (*repository.Subscription, error) {
	row, err := a.queries.GetActiveSubscriptionForBroadcasterType(ctx, sqlitegen.GetActiveSubscriptionForBroadcasterTypeParams{
		BroadcasterID: sql.NullString{String: broadcasterID, Valid: true},
		Type:          subType,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteSubscriptionToDomain(row), nil
}

func (a *SQLiteAdapter) ListActiveSubscriptions(ctx context.Context, limit, offset int) ([]repository.Subscription, error) {
	rows, err := a.queries.ListActiveSubscriptions(ctx, sqlitegen.ListActiveSubscriptionsParams{
		Limit:  int64(limit),
		Offset: int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list active subscriptions: %w", err)
	}
	return sqliteSubscriptionsToDomain(rows), nil
}

func (a *SQLiteAdapter) ListSubscriptionsByBroadcaster(ctx context.Context, broadcasterID string) ([]repository.Subscription, error) {
	rows, err := a.queries.ListSubscriptionsByBroadcaster(ctx, sql.NullString{String: broadcasterID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("sqlite list subs by broadcaster: %w", err)
	}
	return sqliteSubscriptionsToDomain(rows), nil
}

func (a *SQLiteAdapter) ListSubscriptionsByType(ctx context.Context, subType string) ([]repository.Subscription, error) {
	rows, err := a.queries.ListSubscriptionsByType(ctx, subType)
	if err != nil {
		return nil, fmt.Errorf("sqlite list subs by type: %w", err)
	}
	return sqliteSubscriptionsToDomain(rows), nil
}

func (a *SQLiteAdapter) UpdateSubscriptionStatus(ctx context.Context, id, status string) error {
	return a.queries.UpdateSubscriptionStatus(ctx, sqlitegen.UpdateSubscriptionStatusParams{
		ID:     id,
		Status: status,
	})
}

func (a *SQLiteAdapter) MarkSubscriptionRevoked(ctx context.Context, id, reason string) error {
	return a.queries.MarkSubscriptionRevoked(ctx, sqlitegen.MarkSubscriptionRevokedParams{
		ID:            id,
		RevokedReason: sql.NullString{String: reason, Valid: true},
	})
}

func (a *SQLiteAdapter) DeleteSubscription(ctx context.Context, id string) error {
	return a.queries.DeleteSubscription(ctx, id)
}

func (a *SQLiteAdapter) CountActiveSubscriptions(ctx context.Context) (int64, error) {
	return a.queries.CountActiveSubscriptions(ctx)
}

func sqliteSubscriptionToDomain(s sqlitegen.Subscription) *repository.Subscription {
	return &repository.Subscription{
		ID:                s.ID,
		Status:            s.Status,
		Type:              s.Type,
		Version:           s.Version,
		Cost:              s.Cost,
		Condition:         json.RawMessage(s.Condition),
		BroadcasterID:     fromNullString(s.BroadcasterID),
		TransportMethod:   s.TransportMethod,
		TransportCallback: s.TransportCallback,
		TwitchCreatedAt:   parseTime(s.TwitchCreatedAt),
		CreatedAt:         parseTime(s.CreatedAt),
		RevokedAt:         parseNullTime(s.RevokedAt),
		RevokedReason:     fromNullString(s.RevokedReason),
	}
}

func sqliteSubscriptionsToDomain(rows []sqlitegen.Subscription) []repository.Subscription {
	out := make([]repository.Subscription, len(rows))
	for i, r := range rows {
		out[i] = *sqliteSubscriptionToDomain(r)
	}
	return out
}

func stringPtrToNullString(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}
