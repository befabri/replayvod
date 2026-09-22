package sqliteadapter

import (
	"context"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) SaveTwitchPlaybackSession(ctx context.Context, s *repository.TwitchPlaybackSession) error {
	return mapErr(a.queries.SaveTwitchPlaybackSession(ctx, sqlitegen.SaveTwitchPlaybackSessionParams{
		TwitchUserID: s.TwitchUserID, TwitchLogin: s.TwitchLogin,
		EncryptedToken: s.EncryptedToken, ExpiresAt: s.ExpiresAt, CheckedAt: s.CheckedAt,
	}))
}
func (a *SQLiteAdapter) UpdateTwitchPlaybackSessionValidation(ctx context.Context, s *repository.TwitchPlaybackSession) error {
	var reconnect int64
	if s.NeedsReconnect {
		reconnect = 1
	}
	return mapErr(a.queries.UpdateTwitchPlaybackSessionValidation(ctx, sqlitegen.UpdateTwitchPlaybackSessionValidationParams{
		EncryptedToken: s.EncryptedToken, ExpiresAt: s.ExpiresAt, CheckedAt: s.CheckedAt,
		NeedsReconnect: reconnect,
	}))
}
