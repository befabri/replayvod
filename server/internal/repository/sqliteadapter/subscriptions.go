package sqliteadapter

import (
	"context"
	"database/sql"
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
		TwitchCreatedAt:   sqliteTime(input.TwitchCreatedAt),
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
		TwitchCreatedAt:   sqliteTime(input.TwitchCreatedAt),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert subscription: %w", err)
	}
	return sqliteSubscriptionToDomain(row), nil
}

func stringPtrToNullString(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}
