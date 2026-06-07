package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) GetChannelUserState(ctx context.Context, userID string, broadcasterID string) (*repository.ChannelUserState, error) {
	row, err := a.queries.GetChannelUserState(ctx, pggen.GetChannelUserStateParams{
		UserID:        userID,
		BroadcasterID: broadcasterID,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return pgChannelUserStateToDomain(row), nil
}

func (a *PGAdapter) ListChannelUserStatesForChannels(ctx context.Context, userID string, broadcasterIDs []string) ([]repository.ChannelUserState, error) {
	if userID == "" || len(broadcasterIDs) == 0 {
		return []repository.ChannelUserState{}, nil
	}
	rows, err := a.queries.ListChannelUserStatesForChannels(ctx, pggen.ListChannelUserStatesForChannelsParams{
		UserID:         userID,
		BroadcasterIds: broadcasterIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("pg list channel user states: %w", err)
	}
	out := make([]repository.ChannelUserState, len(rows))
	for i, row := range rows {
		out[i] = *pgChannelUserStateToDomain(row)
	}
	return out, nil
}

func (a *PGAdapter) SetChannelFavorite(ctx context.Context, userID string, broadcasterID string, favorite bool) (*repository.ChannelUserState, error) {
	row, err := a.queries.SetChannelFavorite(ctx, pggen.SetChannelFavoriteParams{
		UserID:        userID,
		BroadcasterID: broadcasterID,
		Favorite:      favorite,
	})
	if err != nil {
		return nil, fmt.Errorf("pg set channel favorite: %w", err)
	}
	return pgChannelUserStateToDomain(row), nil
}
