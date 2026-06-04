package config

import "cmp"

func getDefaultAppConfig() AppConfig {
	return AppConfig{
		Server: ServerConfig{
			PollIntervalMinutes: 1,
		},
		Download: DownloadConfig{
			MaxConcurrent:        2,
			PreferredQuality:     "1080",
			SegmentConcurrency:   4,
			NetworkAttempts:      5,
			ServerErrorAttempts:  5,
			CDNLagAttempts:       3,
			AuthRefreshAttempts:  2,
			MaxGapRatio:          0.01,
			Strict:               false,
			EnableAV1:            false,
			DisableHEVC:          false,
			MaxRestartGapSeconds: 120,
			MaxPartCount:         1024,
			SignedURLTTLHours:    168,
		},
		Storage: StorageConfig{
			Type:      "local",
			LocalPath: "./data",
		},
		Scheduler: SchedulerConfig{
			Enabled:                               true,
			ThumbnailIntervalMinutes:              5,
			EventsubIntervalMinutes:               10,
			EventsubReconcileIntervalMinutes:      60,
			CategoryArtIntervalMinutes:            1440,
			TokenCleanupIntervalMinutes:           60,
			FetchLogsRetentionDays:                14,
			WebhookEventPayloadRetentionDays:      7,
			EventLogsRetentionDays:                14,
			RecordingWebhookDeliveryRetentionDays: 30,
			SessionCleanupIntervalMinutes:         120,
			RecordingsRetentionIntervalMinutes:    60,
		},
		Logging: LoggingConfig{
			LogToFile: false,
			LogDir:    "./logs",
			LogLevel:  "debug",
		},
		PostgresPool: PostgresPoolConfig{
			MaxConns:            25,
			MinConns:            5,
			MaxConnLifetimeMs:   1800000,
			MaxConnIdleTimeMs:   300000,
			HealthCheckPeriodMs: 30000,
		},
		Development: false,
	}
}

// orDefault returns def when v is at or below its type's zero value (<= 0 for
// numbers, "" for strings). It collapses the "reset an unset or non-positive
// field to its default" guard, which validateAppConfig applies to most fields,
// to a single expression.
func orDefault[T cmp.Ordered](v, def T) T {
	var zero T
	if v <= zero {
		return def
	}
	return v
}

func validateAppConfig(config *AppConfig) {
	config.Server.PollIntervalMinutes = orDefault(config.Server.PollIntervalMinutes, 1)
	config.Download.MaxConcurrent = orDefault(config.Download.MaxConcurrent, 2)
	config.Download.PreferredQuality = orDefault(config.Download.PreferredQuality, "1080")
	config.Download.SegmentConcurrency = orDefault(config.Download.SegmentConcurrency, 4)
	config.Download.NetworkAttempts = orDefault(config.Download.NetworkAttempts, 5)
	config.Download.ServerErrorAttempts = orDefault(config.Download.ServerErrorAttempts, 5)
	config.Download.CDNLagAttempts = orDefault(config.Download.CDNLagAttempts, 3)
	config.Download.AuthRefreshAttempts = orDefault(config.Download.AuthRefreshAttempts, 2)
	// MaxGapRatio must be in [0, 1). 0 = no tolerance (all gaps fail);
	// >=1 would accept any number of gaps and is nonsensical. Negative
	// or > 1 silently reset to the default rather than panicking at
	// startup.
	if config.Download.MaxGapRatio < 0 || config.Download.MaxGapRatio >= 1 {
		config.Download.MaxGapRatio = 0.01
	}
	config.Download.MaxRestartGapSeconds = orDefault(config.Download.MaxRestartGapSeconds, 120)
	if config.Download.MaxPartBytes < 0 {
		config.Download.MaxPartBytes = 0
	}
	if config.Download.MaxPartSeconds < 0 {
		config.Download.MaxPartSeconds = 0
	}
	config.Download.MaxPartCount = orDefault(config.Download.MaxPartCount, 1024)
	config.PostgresPool.MaxConns = orDefault(config.PostgresPool.MaxConns, 25)
	config.PostgresPool.MinConns = orDefault(config.PostgresPool.MinConns, 5)
	// MinConns can't exceed MaxConns or pgxpool rejects the config at open.
	// Clamp the floor down to the ceiling rather than failing boot, matching
	// the silent-reset style of the rest of this function.
	config.PostgresPool.MinConns = min(config.PostgresPool.MinConns, config.PostgresPool.MaxConns)
}
