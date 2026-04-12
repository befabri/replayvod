package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

// Channels

func (a *PGAdapter) GetChannel(ctx context.Context, broadcasterID string) (*repository.Channel, error) {
	row, err := a.queries.GetChannel(ctx, broadcasterID)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgChannelToDomain(row), nil
}

func (a *PGAdapter) GetChannelByLogin(ctx context.Context, login string) (*repository.Channel, error) {
	row, err := a.queries.GetChannelByLogin(ctx, login)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgChannelToDomain(row), nil
}

func (a *PGAdapter) UpsertChannel(ctx context.Context, c *repository.Channel) (*repository.Channel, error) {
	row, err := a.queries.UpsertChannel(ctx, pggen.UpsertChannelParams{
		BroadcasterID:       c.BroadcasterID,
		BroadcasterLogin:    c.BroadcasterLogin,
		BroadcasterName:     c.BroadcasterName,
		BroadcasterLanguage: toPgText(c.BroadcasterLanguage),
		ProfileImageUrl:     toPgText(c.ProfileImageURL),
		OfflineImageUrl:     toPgText(c.OfflineImageURL),
		Description:         toPgText(c.Description),
		BroadcasterType:     toPgText(c.BroadcasterType),
		ViewCount:           int32(c.ViewCount),
	})
	if err != nil {
		return nil, fmt.Errorf("pg upsert channel %s: %w", c.BroadcasterID, err)
	}
	return pgChannelToDomain(row), nil
}

func (a *PGAdapter) ListChannels(ctx context.Context) ([]repository.Channel, error) {
	rows, err := a.queries.ListChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg list channels: %w", err)
	}
	channels := make([]repository.Channel, len(rows))
	for i, row := range rows {
		channels[i] = *pgChannelToDomain(row)
	}
	return channels, nil
}

func (a *PGAdapter) DeleteChannel(ctx context.Context, broadcasterID string) error {
	return a.queries.DeleteChannel(ctx, broadcasterID)
}

// User follows

func (a *PGAdapter) UpsertUserFollow(ctx context.Context, f *repository.UserFollow) error {
	return a.queries.UpsertUserFollow(ctx, pggen.UpsertUserFollowParams{
		UserID:        f.UserID,
		BroadcasterID: f.BroadcasterID,
		FollowedAt:    toPgTimestamptz(f.FollowedAt),
		Followed:      f.Followed,
	})
}

func (a *PGAdapter) ListUserFollows(ctx context.Context, userID string) ([]repository.Channel, error) {
	rows, err := a.queries.ListUserFollows(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("pg list user follows: %w", err)
	}
	channels := make([]repository.Channel, len(rows))
	for i, row := range rows {
		channels[i] = *pgChannelToDomain(row)
	}
	return channels, nil
}

func (a *PGAdapter) UnfollowChannel(ctx context.Context, userID, broadcasterID string) error {
	return a.queries.UnfollowChannel(ctx, pggen.UnfollowChannelParams{
		UserID:        userID,
		BroadcasterID: broadcasterID,
	})
}

func pgChannelToDomain(c pggen.Channel) *repository.Channel {
	return &repository.Channel{
		BroadcasterID:       c.BroadcasterID,
		BroadcasterLogin:    c.BroadcasterLogin,
		BroadcasterName:     c.BroadcasterName,
		BroadcasterLanguage: fromPgText(c.BroadcasterLanguage),
		ProfileImageURL:     fromPgText(c.ProfileImageUrl),
		OfflineImageURL:     fromPgText(c.OfflineImageUrl),
		Description:         fromPgText(c.Description),
		BroadcasterType:     fromPgText(c.BroadcasterType),
		ViewCount:           int64(c.ViewCount),
		CreatedAt:           c.CreatedAt.Time,
		UpdatedAt:           c.UpdatedAt.Time,
	}
}
