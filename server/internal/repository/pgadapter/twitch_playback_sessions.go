package pgadapter

import (
	"context"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) GetTwitchPlaybackSession(ctx context.Context) (*repository.TwitchPlaybackSession, error) {
	row, err := a.queries.GetTwitchPlaybackSession(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return &repository.TwitchPlaybackSession{
		TwitchUserID: row.TwitchUserID, TwitchLogin: row.TwitchLogin,
		EncryptedToken: row.EncryptedToken, ExpiresAt: row.ExpiresAt,
		CheckedAt: row.CheckedAt, NeedsReconnect: row.NeedsReconnect,
	}, nil
}
func (a *PGAdapter) SaveTwitchPlaybackSession(ctx context.Context, s *repository.TwitchPlaybackSession) error {
	return mapErr(a.queries.SaveTwitchPlaybackSession(ctx, pggen.SaveTwitchPlaybackSessionParams{
		TwitchUserID: s.TwitchUserID, TwitchLogin: s.TwitchLogin,
		EncryptedToken: s.EncryptedToken, ExpiresAt: s.ExpiresAt, CheckedAt: s.CheckedAt,
	}))
}
func (a *PGAdapter) UpdateTwitchPlaybackSessionValidation(ctx context.Context, s *repository.TwitchPlaybackSession) error {

	return mapErr(a.queries.UpdateTwitchPlaybackSessionValidation(ctx, pggen.UpdateTwitchPlaybackSessionValidationParams{
		EncryptedToken: s.EncryptedToken, ExpiresAt: s.ExpiresAt, CheckedAt: s.CheckedAt,
		NeedsReconnect: s.NeedsReconnect,
	}))
}
func (a *PGAdapter) DeleteTwitchPlaybackSession(ctx context.Context) error {
	return mapErr(a.queries.DeleteTwitchPlaybackSession(ctx))
}
