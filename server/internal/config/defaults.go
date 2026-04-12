package config

func getDefaultAppConfig() AppConfig {
	return AppConfig{
		Server: ServerConfig{
			AllowedOrigins: []string{"http://localhost:3000"},
		},
		Download: DownloadConfig{
			MaxConcurrent:  2,
			DefaultQuality: "1080",
			AudioRate:      48000,
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
