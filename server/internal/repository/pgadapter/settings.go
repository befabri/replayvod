package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

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
