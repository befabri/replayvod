package sqliteadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) GetServerSettings(ctx context.Context) (*repository.ServerSettings, error) {
	row, err := a.queries.GetServerSettings(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteServerSettingsToDomain(row), nil
}

func (a *SQLiteAdapter) UpsertServerSettings(ctx context.Context, s *repository.ServerSettings) (*repository.ServerSettings, error) {
	row, err := a.queries.UpsertServerSettings(ctx, sqlitegen.UpsertServerSettingsParams{
		ServerMode:                    s.ServerMode,
		EventsubWebhookCallbackUrl:    s.EventSubWebhookCallbackURL,
		EventsubRelayIngestUrl:        s.EventSubRelayIngestURL,
		EventsubRelaySubscribeUrl:     s.EventSubRelaySubscribeURL,
		EventsubRelayLocalCallbackUrl: s.EventSubRelayLocalCallbackURL,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert server settings: %w", err)
	}
	return sqliteServerSettingsToDomain(row), nil
}

func sqliteServerSettingsToDomain(s sqlitegen.ServerSetting) *repository.ServerSettings {
	return &repository.ServerSettings{
		ServerMode:                    s.ServerMode,
		EventSubWebhookCallbackURL:    s.EventsubWebhookCallbackUrl,
		EventSubRelayIngestURL:        s.EventsubRelayIngestUrl,
		EventSubRelaySubscribeURL:     s.EventsubRelaySubscribeUrl,
		EventSubRelayLocalCallbackURL: s.EventsubRelayLocalCallbackUrl,
		CreatedAt:                     parseTime(s.CreatedAt),
		UpdatedAt:                     parseTime(s.UpdatedAt),
	}
}
