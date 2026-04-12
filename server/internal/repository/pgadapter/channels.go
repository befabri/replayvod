package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

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
		BroadcasterLanguage: c.BroadcasterLanguage,
		ProfileImageUrl:     c.ProfileImageURL,
		OfflineImageUrl:     c.OfflineImageURL,
		Description:         c.Description,
		BroadcasterType:     c.BroadcasterType,
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

func (a *PGAdapter) UpsertUserFollow(ctx context.Context, f *repository.UserFollow) error {
	return a.queries.UpsertUserFollow(ctx, pggen.UpsertUserFollowParams{
		UserID:        f.UserID,
		BroadcasterID: f.BroadcasterID,
		FollowedAt:    f.FollowedAt,
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
		BroadcasterLanguage: c.BroadcasterLanguage,
		ProfileImageURL:     c.ProfileImageUrl,
		OfflineImageURL:     c.OfflineImageUrl,
		Description:         c.Description,
		BroadcasterType:     c.BroadcasterType,
		ViewCount:           int64(c.ViewCount),
		CreatedAt:           c.CreatedAt,
		UpdatedAt:           c.UpdatedAt,
	}
}
