package pgadapter

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) GetServerSettings(ctx context.Context) (*repository.ServerSettings, error) {
	row, err := a.queries.GetServerSettings(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgServerSettingsToDomain(row), nil
}

func (a *PGAdapter) UpsertServerSettings(ctx context.Context, s *repository.ServerSettings) (*repository.ServerSettings, error) {
	row, err := a.queries.UpsertServerSettings(ctx, pggen.UpsertServerSettingsParams{
		ServerMode:                    s.ServerMode,
		EventsubWebhookCallbackUrl:    s.EventSubWebhookCallbackURL,
		EventsubRelayIngestUrl:        s.EventSubRelayIngestURL,
		EventsubRelaySubscribeUrl:     s.EventSubRelaySubscribeURL,
		EventsubRelayLocalCallbackUrl: s.EventSubRelayLocalCallbackURL,
	})
	if err != nil {
		return nil, fmt.Errorf("pg upsert server settings: %w", err)
	}
	return pgServerSettingsToDomain(row), nil
}

func (a *PGAdapter) GetServerHMACSecret(ctx context.Context) (string, error) {
	secret, err := a.queries.GetServerHMACSecret(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", mapErr(err)
	}
	return secret, nil
}

func (a *PGAdapter) EnsureServerHMACSecret(ctx context.Context, secret string) error {
	if err := a.queries.EnsureServerHMACSecret(ctx, secret); err != nil {
		return fmt.Errorf("pg ensure server hmac secret: %w", err)
	}
	return nil
}

func pgServerSettingsToDomain(s pggen.ServerSetting) *repository.ServerSettings {
	return &repository.ServerSettings{
		ServerMode:                    s.ServerMode,
		EventSubWebhookCallbackURL:    s.EventsubWebhookCallbackUrl,
		EventSubRelayIngestURL:        s.EventsubRelayIngestUrl,
		EventSubRelaySubscribeURL:     s.EventsubRelaySubscribeUrl,
		EventSubRelayLocalCallbackURL: s.EventsubRelayLocalCallbackUrl,
		CreatedAt:                     s.CreatedAt,
		UpdatedAt:                     s.UpdatedAt,
	}
}
