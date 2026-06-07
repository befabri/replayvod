package sqliteadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) GetChannelUserState(ctx context.Context, userID string, broadcasterID string) (*repository.ChannelUserState, error) {
	row, err := a.queries.GetChannelUserState(ctx, sqlitegen.GetChannelUserStateParams{
		UserID:        userID,
		BroadcasterID: broadcasterID,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteChannelUserStateToDomain(row), nil
}

func (a *SQLiteAdapter) ListChannelUserStatesForChannels(ctx context.Context, userID string, broadcasterIDs []string) ([]repository.ChannelUserState, error) {
	if userID == "" || len(broadcasterIDs) == 0 {
		return []repository.ChannelUserState{}, nil
	}
	rows, err := a.queries.ListChannelUserStatesForChannels(ctx, sqlitegen.ListChannelUserStatesForChannelsParams{
		UserID:         userID,
		BroadcasterIds: broadcasterIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list channel user states: %w", err)
	}
	out := make([]repository.ChannelUserState, len(rows))
	for i, row := range rows {
		out[i] = *sqliteChannelUserStateToDomain(row)
	}
	return out, nil
}

func (a *SQLiteAdapter) SetChannelFavorite(ctx context.Context, userID string, broadcasterID string, favorite bool) (*repository.ChannelUserState, error) {
	row, err := a.queries.SetChannelFavorite(ctx, sqlitegen.SetChannelFavoriteParams{
		UserID:        userID,
		BroadcasterID: broadcasterID,
		Favorite:      boolToInt64(favorite),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite set channel favorite: %w", err)
	}
	return sqliteChannelUserStateToDomain(row), nil
}

func sqliteChannelUserStateToDomain(row sqlitegen.ChannelUserState) *repository.ChannelUserState {
	return &repository.ChannelUserState{
		UserID:        row.UserID,
		BroadcasterID: row.BroadcasterID,
		Favorite:      row.Favorite != 0,
		CreatedAt:     row.CreatedAt.Time,
		UpdatedAt:     row.UpdatedAt.Time,
	}
}
