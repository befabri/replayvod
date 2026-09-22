package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

// Channels

func (a *SQLiteAdapter) ListChannelsPage(ctx context.Context, limit int, sort string, filter string, userID string, cursor *repository.ChannelPageCursor) (*repository.ChannelPage, error) {
	params := sqlitegen.ListChannelsPageAscParams{
		LiveOnly:       boolToInt64(filter == repository.ChannelFilterLive),
		DownloadedOnly: boolToInt64(filter == repository.ChannelFilterDownloaded),
		FavoriteOnly:   boolToInt64(filter == repository.ChannelFilterFavorites),
		UserID:         userID,
		CursorName:     sqliteChannelCursorName(cursor),
		CursorID:       sqliteChannelCursorID(cursor),
		Limit:          int64(limit + 1),
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

// User follows

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
