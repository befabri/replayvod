package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

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

func (a *PGAdapter) ListChannelsPage(ctx context.Context, limit int, sort string, filter string, userID string, cursor *repository.ChannelPageCursor) (*repository.ChannelPage, error) {
	params := pggen.ListChannelsPageAscParams{
		LiveOnly:       filter == repository.ChannelFilterLive,
		DownloadedOnly: filter == repository.ChannelFilterDownloaded,
		FavoriteOnly:   filter == repository.ChannelFilterFavorites,
		UserID:         userID,
		CursorName:     pgChannelCursorName(cursor),
		CursorID:       pgChannelCursorID(cursor),
		Limit:          int32(limit + 1),
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

func (a *PGAdapter) UpsertUserFollow(ctx context.Context, f *repository.UserFollow) error {
	return a.queries.UpsertUserFollow(ctx, pggen.UpsertUserFollowParams{
		UserID:        f.UserID,
		BroadcasterID: f.BroadcasterID,
		FollowedAt:    f.FollowedAt,
		Followed:      f.Followed,
	})
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
