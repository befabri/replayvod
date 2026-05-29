package sqliteadapter

import (
	"context"
	"database/sql"
	"errors"
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

func (a *SQLiteAdapter) GetServerHMACSecret(ctx context.Context) (string, error) {
	secret, err := a.queries.GetServerHMACSecret(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", mapErr(err)
	}
	return secret, nil
}

func (a *SQLiteAdapter) EnsureServerHMACSecret(ctx context.Context, secret string) error {
	if err := a.queries.EnsureServerHMACSecret(ctx, secret); err != nil {
		return fmt.Errorf("sqlite ensure server hmac secret: %w", err)
	}
	return nil
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
