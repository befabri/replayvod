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

func (a *SQLiteAdapter) UpsertRecordingWebhookConfig(ctx context.Context, enabled bool, url, events string) (*repository.ServerSettings, error) {
	var enabledInt int64
	if enabled {
		enabledInt = 1
	}
	row, err := a.queries.UpsertRecordingWebhookConfig(ctx, sqlitegen.UpsertRecordingWebhookConfigParams{
		RecordingWebhookEnabled: enabledInt,
		RecordingWebhookUrl:     url,
		RecordingWebhookEvents:  events,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert recording webhook config: %w", err)
	}
	return sqliteServerSettingsToDomain(row), nil
}

func (a *SQLiteAdapter) UpsertPlaybackCacheConfig(ctx context.Context, enabled bool, maxPercent int, autoGenerate bool) (*repository.ServerSettings, error) {
	var enabledInt, autoGenerateInt int64
	if enabled {
		enabledInt = 1
	}
	if autoGenerate {
		autoGenerateInt = 1
	}
	row, err := a.queries.UpsertPlaybackCacheConfig(ctx, sqlitegen.UpsertPlaybackCacheConfigParams{
		PlaybackCacheEnabled:      enabledInt,
		PlaybackCacheMaxPercent:   int64(maxPercent),
		PlaybackCacheAutoGenerate: autoGenerateInt,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert playback cache config: %w", err)
	}
	return sqliteServerSettingsToDomain(row), nil
}

func (a *SQLiteAdapter) SetSchedulesPaused(ctx context.Context, paused bool) (*repository.ServerSettings, error) {
	var pausedInt int64
	if paused {
		pausedInt = 1
	}
	row, err := a.queries.SetSchedulesPaused(ctx, pausedInt)
	if err != nil {
		return nil, fmt.Errorf("sqlite set schedules paused: %w", err)
	}
	return sqliteServerSettingsToDomain(row), nil
}

func (a *SQLiteAdapter) EnsureRecordingWebhookSecret(ctx context.Context, secret string) error {
	if err := a.queries.EnsureRecordingWebhookSecret(ctx, secret); err != nil {
		return fmt.Errorf("sqlite ensure recording webhook secret: %w", err)
	}
	return nil
}

func (a *SQLiteAdapter) SetRecordingWebhookSecret(ctx context.Context, secret string) error {
	if err := a.queries.SetRecordingWebhookSecret(ctx, secret); err != nil {
		return fmt.Errorf("sqlite set recording webhook secret: %w", err)
	}
	return nil
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
		RecordingWebhookEnabled:       s.RecordingWebhookEnabled != 0,
		RecordingWebhookURL:           s.RecordingWebhookUrl,
		RecordingWebhookSecret:        s.RecordingWebhookSecret,
		RecordingWebhookEvents:        s.RecordingWebhookEvents,
		PlaybackCacheEnabled:          s.PlaybackCacheEnabled != 0,
		PlaybackCacheMaxPercent:       int(s.PlaybackCacheMaxPercent),
		PlaybackCacheAutoGenerate:     s.PlaybackCacheAutoGenerate != 0,
		SchedulesPaused:               s.SchedulesPaused != 0,
		CreatedAt:                     s.CreatedAt.Time,
		UpdatedAt:                     s.UpdatedAt.Time,
	}
}
