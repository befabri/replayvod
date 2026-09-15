package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

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
