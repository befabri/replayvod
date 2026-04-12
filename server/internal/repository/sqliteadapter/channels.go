package sqliteadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

// Channels

func (a *SQLiteAdapter) GetChannel(ctx context.Context, broadcasterID string) (*repository.Channel, error) {
	row, err := a.queries.GetChannel(ctx, broadcasterID)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteChannelToDomain(row), nil
}

func (a *SQLiteAdapter) GetChannelByLogin(ctx context.Context, login string) (*repository.Channel, error) {
	row, err := a.queries.GetChannelByLogin(ctx, login)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteChannelToDomain(row), nil
}

func (a *SQLiteAdapter) UpsertChannel(ctx context.Context, c *repository.Channel) (*repository.Channel, error) {
	row, err := a.queries.UpsertChannel(ctx, sqlitegen.UpsertChannelParams{
		BroadcasterID:       c.BroadcasterID,
		BroadcasterLogin:    c.BroadcasterLogin,
		BroadcasterName:     c.BroadcasterName,
		BroadcasterLanguage: toNullString(c.BroadcasterLanguage),
		ProfileImageUrl:     toNullString(c.ProfileImageURL),
		OfflineImageUrl:     toNullString(c.OfflineImageURL),
		Description:         toNullString(c.Description),
		BroadcasterType:     toNullString(c.BroadcasterType),
		ViewCount:           c.ViewCount,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert channel %s: %w", c.BroadcasterID, err)
	}
	return sqliteChannelToDomain(row), nil
}

func (a *SQLiteAdapter) ListChannels(ctx context.Context) ([]repository.Channel, error) {
	rows, err := a.queries.ListChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite list channels: %w", err)
	}
	channels := make([]repository.Channel, len(rows))
	for i, row := range rows {
		channels[i] = *sqliteChannelToDomain(row)
	}
	return channels, nil
}

func (a *SQLiteAdapter) DeleteChannel(ctx context.Context, broadcasterID string) error {
	return a.queries.DeleteChannel(ctx, broadcasterID)
}

// User follows

func (a *SQLiteAdapter) UpsertUserFollow(ctx context.Context, f *repository.UserFollow) error {
	followed := int64(0)
	if f.Followed {
		followed = 1
	}
	return a.queries.UpsertUserFollow(ctx, sqlitegen.UpsertUserFollowParams{
		UserID:        f.UserID,
		BroadcasterID: f.BroadcasterID,
		FollowedAt:    formatTime(f.FollowedAt),
		Followed:      followed,
	})
}

func (a *SQLiteAdapter) ListUserFollows(ctx context.Context, userID string) ([]repository.Channel, error) {
	rows, err := a.queries.ListUserFollows(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("sqlite list user follows: %w", err)
	}
	channels := make([]repository.Channel, len(rows))
	for i, row := range rows {
		channels[i] = *sqliteChannelToDomain(row)
	}
	return channels, nil
}

func (a *SQLiteAdapter) UnfollowChannel(ctx context.Context, userID, broadcasterID string) error {
	return a.queries.UnfollowChannel(ctx, sqlitegen.UnfollowChannelParams{
		UserID:        userID,
		BroadcasterID: broadcasterID,
	})
}

func sqliteChannelToDomain(c sqlitegen.Channel) *repository.Channel {
	return &repository.Channel{
		BroadcasterID:       c.BroadcasterID,
		BroadcasterLogin:    c.BroadcasterLogin,
		BroadcasterName:     c.BroadcasterName,
		BroadcasterLanguage: fromNullString(c.BroadcasterLanguage),
		ProfileImageURL:     fromNullString(c.ProfileImageUrl),
		OfflineImageURL:     fromNullString(c.OfflineImageUrl),
		Description:         fromNullString(c.Description),
		BroadcasterType:     fromNullString(c.BroadcasterType),
		ViewCount:           c.ViewCount,
		CreatedAt:           parseTime(c.CreatedAt),
		UpdatedAt:           parseTime(c.UpdatedAt),
	}
}
