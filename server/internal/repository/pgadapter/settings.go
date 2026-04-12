package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) GetSettings(ctx context.Context, userID string) (*repository.Settings, error) {
	row, err := a.queries.GetSettings(ctx, userID)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgSettingsToDomain(row), nil
}

func (a *PGAdapter) UpsertSettings(ctx context.Context, s *repository.Settings) (*repository.Settings, error) {
	row, err := a.queries.UpsertSettings(ctx, pggen.UpsertSettingsParams{
		UserID:         s.UserID,
		Timezone:       s.Timezone,
		DatetimeFormat: s.DatetimeFormat,
		Language:       s.Language,
	})
	if err != nil {
		return nil, fmt.Errorf("pg upsert settings: %w", err)
	}
	return pgSettingsToDomain(row), nil
}

func pgSettingsToDomain(s pggen.Setting) *repository.Settings {
	return &repository.Settings{
		UserID:         s.UserID,
		Timezone:       s.Timezone,
		DatetimeFormat: s.DatetimeFormat,
		Language:       s.Language,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
	}
}
