package config

func getDefaultAppConfig() AppConfig {
	return AppConfig{
		Server: ServerConfig{
			AllowedOrigins: []string{"http://localhost:3000"},
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
			AudioRate:            48000,
		},
		Storage: StorageConfig{
			Type:      "local",
			LocalPath: "./data",
		},
		Scheduler: SchedulerConfig{
			Enabled:                          true,
			ThumbnailIntervalMinutes:         5,
			EventsubIntervalMinutes:          10,
			CategoryArtIntervalMinutes:       1440,
			TokenCleanupIntervalMinutes:      60,
			FetchLogsRetentionDays:           14,
			WebhookEventPayloadRetentionDays: 7,
			EventLogsRetentionDays:           14,
			SessionCleanupIntervalMinutes:    120,
		},
		Logging: LoggingConfig{
			LogToFile:  false,
			LogDir:     "./logs",
			LogLevel:   "debug",
			SampleRate: 1.0,
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

func validateAppConfig(config *AppConfig) {
	if config.Download.MaxConcurrent <= 0 {
		config.Download.MaxConcurrent = 2
	}
	if config.Download.PreferredQuality == "" {
		config.Download.PreferredQuality = "1080"
	}
	if config.Download.SegmentConcurrency <= 0 {
		config.Download.SegmentConcurrency = 4
	}
	if config.Download.NetworkAttempts <= 0 {
		config.Download.NetworkAttempts = 5
	}
	if config.Download.ServerErrorAttempts <= 0 {
		config.Download.ServerErrorAttempts = 5
	}
	if config.Download.CDNLagAttempts <= 0 {
		config.Download.CDNLagAttempts = 3
	}
	if config.Download.AuthRefreshAttempts <= 0 {
		config.Download.AuthRefreshAttempts = 2
	}
	// MaxGapRatio must be in [0, 1). 0 = no tolerance (all gaps fail);
	// >=1 would accept any number of gaps and is nonsensical. Negative
	// or > 1 silently reset to the default rather than panicking at
	// startup.
	if config.Download.MaxGapRatio < 0 || config.Download.MaxGapRatio >= 1 {
		config.Download.MaxGapRatio = 0.01
	}
	if config.Download.MaxRestartGapSeconds <= 0 {
		config.Download.MaxRestartGapSeconds = 120
	}
	if config.Download.AudioRate <= 0 {
		config.Download.AudioRate = 48000
	}
	if config.Logging.SampleRate <= 0 || config.Logging.SampleRate > 1 {
		config.Logging.SampleRate = 1.0
	}
	if config.PostgresPool.MaxConns <= 0 {
		config.PostgresPool.MaxConns = 25
	}
	if config.PostgresPool.MinConns <= 0 {
		config.PostgresPool.MinConns = 5
	}
}
