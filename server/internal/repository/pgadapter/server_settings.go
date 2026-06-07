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

func (a *PGAdapter) UpsertRecordingWebhookConfig(ctx context.Context, enabled bool, url, events string) (*repository.ServerSettings, error) {
	row, err := a.queries.UpsertRecordingWebhookConfig(ctx, pggen.UpsertRecordingWebhookConfigParams{
		RecordingWebhookEnabled: enabled,
		RecordingWebhookUrl:     url,
		RecordingWebhookEvents:  events,
	})
	if err != nil {
		return nil, fmt.Errorf("pg upsert recording webhook config: %w", err)
	}
	return pgServerSettingsToDomain(row), nil
}

func (a *PGAdapter) UpsertPlaybackCacheConfig(ctx context.Context, enabled bool, maxPercent int, autoGenerate bool) (*repository.ServerSettings, error) {
	row, err := a.queries.UpsertPlaybackCacheConfig(ctx, pggen.UpsertPlaybackCacheConfigParams{
		PlaybackCacheEnabled:      enabled,
		PlaybackCacheMaxPercent:   int32(maxPercent),
		PlaybackCacheAutoGenerate: autoGenerate,
	})
	if err != nil {
		return nil, fmt.Errorf("pg upsert playback cache config: %w", err)
	}
	return pgServerSettingsToDomain(row), nil
}

func (a *PGAdapter) SetSchedulesPaused(ctx context.Context, paused bool) (*repository.ServerSettings, error) {
	row, err := a.queries.SetSchedulesPaused(ctx, paused)
	if err != nil {
		return nil, fmt.Errorf("pg set schedules paused: %w", err)
	}
	return pgServerSettingsToDomain(row), nil
}

func (a *PGAdapter) EnsureRecordingWebhookSecret(ctx context.Context, secret string) error {
	if err := a.queries.EnsureRecordingWebhookSecret(ctx, secret); err != nil {
		return fmt.Errorf("pg ensure recording webhook secret: %w", err)
	}
	return nil
}

func (a *PGAdapter) SetRecordingWebhookSecret(ctx context.Context, secret string) error {
	if err := a.queries.SetRecordingWebhookSecret(ctx, secret); err != nil {
		return fmt.Errorf("pg set recording webhook secret: %w", err)
	}
	return nil
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
