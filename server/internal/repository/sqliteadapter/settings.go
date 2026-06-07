package sqliteadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

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
