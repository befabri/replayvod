package sqliteadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) GetSettings(ctx context.Context, userID string) (*repository.Settings, error) {
	row, err := a.queries.GetSettings(ctx, userID)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteSettingsToDomain(row), nil
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

func sqliteSettingsToDomain(s sqlitegen.Setting) *repository.Settings {
	return &repository.Settings{
		UserID:         s.UserID,
		Timezone:       s.Timezone,
		DatetimeFormat: s.DatetimeFormat,
		Language:       s.Language,
		CreatedAt:      parseTime(s.CreatedAt),
		UpdatedAt:      parseTime(s.UpdatedAt),
	}
}
