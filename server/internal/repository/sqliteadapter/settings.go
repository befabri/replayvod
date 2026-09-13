package sqliteadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) EnsureSettings(ctx context.Context, userID string) (*repository.Settings, error) {
	if err := a.queries.EnsureSettings(ctx, userID); err != nil {
		return nil, fmt.Errorf("sqlite ensure settings: %w", mapErr(err))
	}
	return a.GetSettings(ctx, userID)
}

func (a *SQLiteAdapter) UpsertSettings(ctx context.Context, s *repository.Settings) (*repository.Settings, error) {
	row, err := a.queries.UpsertSettings(ctx, sqlitegen.UpsertSettingsParams{
		UserID:         s.UserID,
		Timezone:       s.Timezone,
		DatetimeFormat: s.DatetimeFormat,
		Language:       s.Language,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert settings: %w", err)
	}
	return sqliteSettingsToDomain(row), nil
}

func (a *SQLiteAdapter) UpdatePlaybackSettings(ctx context.Context, s *repository.Settings) (*repository.Settings, error) {
	row, err := a.queries.UpdatePlaybackSettings(ctx, sqlitegen.UpdatePlaybackSettingsParams{
		UserID:                 s.UserID,
		ResumeMinSeconds:       s.ResumeMinSeconds,
		ResumeEndMarginSeconds: s.ResumeEndMarginSeconds,
		ResumeEndMarginPercent: s.ResumeEndMarginPercent,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite update playback settings: %w", err)
	}
	return sqliteSettingsToDomain(row), nil
}
