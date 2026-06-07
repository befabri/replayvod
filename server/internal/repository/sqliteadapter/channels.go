package sqliteadapter

import (
	"context"
	"database/sql"
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

func (a *SQLiteAdapter) ListChannelsPage(ctx context.Context, limit int, sort string, filter string, userID string, cursor *repository.ChannelPageCursor) (*repository.ChannelPage, error) {
	params := sqlitegen.ListChannelsPageAscParams{
		LiveOnly:       boolToInt64(filter == repository.ChannelFilterLive),
		DownloadedOnly: boolToInt64(filter == repository.ChannelFilterDownloaded),
		FavoriteOnly:   boolToInt64(filter == repository.ChannelFilterFavorites),
		UserID:         userID,
		CursorName:     sqliteChannelCursorName(cursor),
		CursorID:       sqliteChannelCursorID(cursor),
		RowLimit:       int64(limit + 1),
	}
	var rows []sqlitegen.Channel
	var err error
	if sort == "name_desc" {
		rows, err = a.queries.ListChannelsPageDesc(ctx, sqlitegen.ListChannelsPageDescParams(params))
	} else {
		rows, err = a.queries.ListChannelsPageAsc(ctx, params)
	}
	if err != nil {
		return nil, fmt.Errorf("sqlite list channels page: %w", err)
	}
	items := make([]repository.Channel, len(rows))
	for i, row := range rows {
		items[i] = *sqliteChannelToDomain(row)
	}
	return repository.ToChannelPage(items, limit), nil
}

func (a *SQLiteAdapter) ListChannelsByIDs(ctx context.Context, ids []string) ([]repository.Channel, error) {
	if len(ids) == 0 {
		return []repository.Channel{}, nil
	}
	rows, err := a.queries.ListChannelsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("sqlite list channels by ids: %w", err)
	}
	channels := make([]repository.Channel, len(rows))
	for i, row := range rows {
		channels[i] = *sqliteChannelToDomain(row)
	}
	return channels, nil
}

func (a *SQLiteAdapter) SearchChannels(ctx context.Context, query string, limit int) ([]repository.Channel, error) {
	rows, err := a.queries.SearchChannels(ctx, sqlitegen.SearchChannelsParams{
		Query:    query,
		RowLimit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite search channels: %w", err)
	}
	out := make([]repository.Channel, len(rows))
	for i, row := range rows {
		out[i] = *sqliteChannelToDomain(row)
	}
	return out, nil
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
		FollowedAt:    sqliteTime(f.FollowedAt),
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

func sqliteChannelCursorName(cursor *repository.ChannelPageCursor) sql.NullString {
	if cursor == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: cursor.BroadcasterName, Valid: true}
}

func sqliteChannelCursorID(cursor *repository.ChannelPageCursor) string {
	if cursor == nil {
		return ""
	}
	return cursor.BroadcasterID
}

// toChannelPage now lives in repository (pagination.go), shared by both adapters.
