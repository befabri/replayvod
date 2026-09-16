package sqliteadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

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
	row, err := a.queries.UpsertRecordingWebhookConfig(ctx, sqlitegen.UpsertRecordingWebhookConfigParams{
		Enabled: boolToInt64(enabled),
		Url:     url,
		Events:  events,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert recording webhook config: %w", err)
	}
	return sqliteServerSettingsToDomain(row), nil
}

func (a *SQLiteAdapter) UpsertPlaybackCacheConfig(ctx context.Context, enabled bool, maxPercent int, autoGenerate bool) (*repository.ServerSettings, error) {
	row, err := a.queries.UpsertPlaybackCacheConfig(ctx, sqlitegen.UpsertPlaybackCacheConfigParams{
		Enabled:      boolToInt64(enabled),
		MaxPercent:   int64(maxPercent),
		AutoGenerate: boolToInt64(autoGenerate),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite upsert playback cache config: %w", err)
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

func (a *SQLiteAdapter) SetStorageRestoreCursor(ctx context.Context, cursor *int64) error {
	var value sql.NullInt64
	if cursor != nil {
		value.Int64, value.Valid = *cursor, true
	}
	if err := a.queries.SetStorageRestoreCursor(ctx, value); err != nil {
		return fmt.Errorf("sqlite set storage restore cursor: %w", err)
	}
	return nil
}
