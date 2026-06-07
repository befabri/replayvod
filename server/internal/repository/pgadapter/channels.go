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
		ViewCount:           c.ViewCount,
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

func (a *PGAdapter) ListChannelsPage(ctx context.Context, limit int, sort string, filter string, userID string, cursor *repository.ChannelPageCursor) (*repository.ChannelPage, error) {
	params := pggen.ListChannelsPageAscParams{
		LiveOnly:       filter == repository.ChannelFilterLive,
		DownloadedOnly: filter == repository.ChannelFilterDownloaded,
		FavoriteOnly:   filter == repository.ChannelFilterFavorites,
		UserID:         userID,
		CursorName:     pgChannelCursorName(cursor),
		CursorID:       pgChannelCursorID(cursor),
		RowLimit:       int32(limit + 1),
	}
	var rows []pggen.Channel
	var err error
	if sort == "name_desc" {
		rows, err = a.queries.ListChannelsPageDesc(ctx, pggen.ListChannelsPageDescParams(params))
	} else {
		rows, err = a.queries.ListChannelsPageAsc(ctx, params)
	}
	if err != nil {
		return nil, fmt.Errorf("pg list channels page: %w", err)
	}
	items := make([]repository.Channel, len(rows))
	for i, row := range rows {
		items[i] = *pgChannelToDomain(row)
	}
	return repository.ToChannelPage(items, limit), nil
}

func (a *PGAdapter) ListChannelsByIDs(ctx context.Context, ids []string) ([]repository.Channel, error) {
	if len(ids) == 0 {
		return []repository.Channel{}, nil
	}
	rows, err := a.queries.ListChannelsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("pg list channels by ids: %w", err)
	}
	channels := make([]repository.Channel, len(rows))
	for i, row := range rows {
		channels[i] = *pgChannelToDomain(row)
	}
	return channels, nil
}

func (a *PGAdapter) SearchChannels(ctx context.Context, query string, limit int) ([]repository.Channel, error) {
	rows, err := a.queries.SearchChannels(ctx, pggen.SearchChannelsParams{
		Query:    query,
		RowLimit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("pg search channels: %w", err)
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
		ViewCount:           c.ViewCount,
		CreatedAt:           c.CreatedAt,
		UpdatedAt:           c.UpdatedAt,
	}
}

func pgChannelCursorName(cursor *repository.ChannelPageCursor) *string {
	if cursor == nil {
		return nil
	}
	return &cursor.BroadcasterName
}

func pgChannelCursorID(cursor *repository.ChannelPageCursor) string {
	if cursor == nil {
		return ""
	}
	return cursor.BroadcasterID
}

// toChannelPage now lives in repository (pagination.go), shared by both adapters.
