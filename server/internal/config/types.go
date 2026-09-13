package config

// Environment contains infrastructure settings loaded from environment variables at startup.
type Environment struct {
	DatabaseDriver   string `env:"DATABASE_DRIVER" envDefault:"postgres"`
	PostgresHost     string `env:"POSTGRES_HOST" envDefault:"127.0.0.1"`
	PostgresPort     int    `env:"POSTGRES_PORT" envDefault:"5432"`
	PostgresDatabase string `env:"POSTGRES_DATABASE" envDefault:"replayvod"`
	PostgresUser     string `env:"POSTGRES_USER" envDefault:"postgres"`
	PostgresPassword string `env:"POSTGRES_PASSWORD"`
	PostgresSSLMode  string `env:"POSTGRES_SSL_MODE" envDefault:"disable"`
	SQLitePath       string `env:"SQLITE_PATH" envDefault:"./data/replayvod.db"`

	TwitchClientID string `env:"TWITCH_CLIENT_ID"`
	TwitchSecret   string `env:"TWITCH_SECRET"`

	// HMACSecret seeds the database secret only on first boot; startup replaces it with the saved
	// value.
	HMACSecret string `env:"HMAC_SECRET"`

	Host string `env:"HOST" envDefault:"0.0.0.0"`
	Port int    `env:"PORT" envDefault:"8080"`

	// DevelopmentOverride takes precedence over config.toml; nil leaves the file setting unchanged.
	DevelopmentOverride *bool `env:"DEVELOPMENT"`

	SessionSecret      string `env:"SESSION_SECRET"`
	WhitelistEnabled   bool   `env:"WHITELIST_ENABLED" envDefault:"false"`
	WhitelistedUserIDs string `env:"WHITELISTED_USER_IDS"`
	OwnerTwitchID      string `env:"OWNER_TWITCH_ID"`

	CallbackURL        string `env:"-"`
	WebhookCallbackURL string `env:"WEBHOOK_CALLBACK_URL"`
	FrontendURL        string `env:"-"`

	// PublicBaseURL is an absolute scheme://host used to derive callback, frontend, and
	// signed-download URLs.
	PublicBaseURL string `env:"PUBLIC_BASE_URL"`

	TrustedOrigins []string `env:"TRUSTED_ORIGINS"`

	// ServerMode accepts "off", "poll", "direct", or "relay"; empty enables app-managed configuration.
	ServerMode string `env:"SERVER_MODE"`

	ServerModeEnvConfigured bool `env:"-"`

	// RelayIngestURL is the public HTTPS endpoint Twitch posts to in relay mode.
	RelayIngestURL string `env:"RELAY_INGEST_URL"`

	// RelaySubscribeURL is the relay WebSocket endpoint and is required in relay mode.
	RelaySubscribeURL string `env:"RELAY_SUBSCRIBE_URL"`

	// RelayLocalCallbackURL defaults to http://127.0.0.1:<PORT>/api/v1/webhook/callback.
	RelayLocalCallbackURL string `env:"RELAY_LOCAL_CALLBACK_URL"`

	VideoDir     string `env:"VIDEO_DIR" envDefault:"./data/videos"`
	ThumbnailDir string `env:"THUMBNAIL_DIR" envDefault:"./data/thumbnails"`
	DashboardDir string `env:"DASHBOARD_DIR"`

	// ScratchDir needs capacity for captured segments and prepared output, independently of media
	// storage.
	ScratchDir string `env:"SCRATCH_DIR" envDefault:"./data/.scratch"`
}

// AppConfig contains config.toml settings loaded at startup.
type AppConfig struct {
	Server       ServerConfig       `toml:"server"`
	Download     DownloadConfig     `toml:"download"`
	Storage      StorageConfig      `toml:"storage"`
	Scheduler    SchedulerConfig    `toml:"scheduler"`
	Logging      LoggingConfig      `toml:"logging"`
	PostgresPool PostgresPoolConfig `toml:"postgres"`
	Health       HealthConfig       `toml:"health"`
	Development  bool               `toml:"development"`
}

// HealthConfig enables the unauthenticated readiness endpoint used by container health checks.
type HealthConfig struct {
	Enabled bool `toml:"enabled"`
}

// ServerConfig sets the interval for polling live channels.
type ServerConfig struct {
	PollIntervalMinutes int `toml:"poll_interval_minutes"`
}

// DownloadConfig controls capture, recovery, and retry budgets.
type DownloadConfig struct {
	// MaxConcurrent limits live recording reservations, including manual restart waits; default two.
	MaxConcurrent int `toml:"max_concurrent"`

	// ArchiveMaxConcurrent reserves separate archive slots; default one.
	ArchiveMaxConcurrent int `toml:"archive_max_concurrent"`

	// ArchiveMaxBytesPerSecond applies only to archive capture; nonpositive values mean unlimited.
	ArchiveMaxBytesPerSecond int64 `toml:"archive_max_bytes_per_second"`

	// SegmentConcurrency defaults to four workers per recording.
	SegmentConcurrency int `toml:"segment_concurrency"`

	// NetworkAttempts bounds transport and truncated-body retries per segment; default five.
	NetworkAttempts int `toml:"network_attempts"`

	// ServerErrorAttempts bounds 429/5xx retries per segment, honoring Retry-After; default five.
	ServerErrorAttempts int `toml:"server_error_attempts"`

	// CDNLagAttempts bounds 404/410 retries at half the target segment duration; default three.
	CDNLagAttempts int `toml:"cdn_lag_attempts"`

	// AuthRefreshAttempts bounds playback-token renewals per part; permanent refusals fail
	// immediately.
	AuthRefreshAttempts int `toml:"auth_refresh_attempts"`

	// MaxGapRatio bounds tolerated content loss; Strict overrides it.
	MaxGapRatio float64 `toml:"max_gap_ratio"`

	Strict bool `toml:"strict"`

	EnableAV1 bool `toml:"enable_av1"`

	DisableHEVC bool `toml:"disable_hevc"`

	// MaxRestartGapSeconds defaults to 120 and splits recovery gaps into separate parts.
	MaxRestartGapSeconds int `toml:"max_restart_gap_seconds"`

	// StreamerRestartWaitSeconds is the manual recording grace period; nonpositive values default
	// to 120.
	StreamerRestartWaitSeconds int `toml:"streamer_restart_wait_seconds"`

	// MaxPartBytes counts committed source bytes; nonpositive values disable size splitting.
	// Cuts occur after whole segments, and remux overhead can exceed the source ceiling,
	// so external output-size limits require extra margin.
	MaxPartBytes int64 `toml:"max_part_bytes"`

	// MaxPartSeconds measures segment duration; nonpositive values disable duration splitting.
	MaxPartSeconds int `toml:"max_part_seconds"`

	// MaxPartCount defaults to 1024 and applies to size/duration cuts; discontinuities use a
	// tighter cap.
	MaxPartCount int32 `toml:"max_part_count"`

	// SignedURLTTLHours defaults to 168; zero omits signed part-download URLs.
	// Recording retention deadlines can shorten that lifetime.
	SignedURLTTLHours int `toml:"signed_url_ttl_hours"`
}

// StorageConfig selects the local or S3 media backend.
type StorageConfig struct {
	Type      string   `toml:"type"`
	LocalPath string   `toml:"local_path"`
	S3        S3Config `toml:"s3"`
}

// S3Config selects an S3-compatible backend.
// Empty credentials use the AWS credential chain; an empty Endpoint uses AWS resolution.
// Nil UsePathStyle defaults to path style with a custom endpoint and virtual-hosted style
// otherwise.
type S3Config struct {
	Endpoint     string `toml:"endpoint"`
	Bucket       string `toml:"bucket"`
	Region       string `toml:"region"`
	AccessKey    string `toml:"access_key"`
	SecretKey    string `toml:"secret_key"`
	UsePathStyle *bool  `toml:"use_path_style"`
}

// SchedulerConfig declares tasks and intervals available after startup; edits require a restart.
type SchedulerConfig struct {
	Enabled                         bool `toml:"enabled"`
	ThumbnailIntervalMinutes        int  `toml:"thumbnail_interval_minutes"`
	EventsubIntervalMinutes         int  `toml:"eventsub_interval_minutes"`
	CategoryArtIntervalMinutes      int  `toml:"category_art_interval_minutes"`
	CategoryMetadataIntervalMinutes int  `toml:"category_metadata_interval_minutes"`
	TokenCleanupIntervalMinutes     int  `toml:"token_cleanup_interval_minutes"`
	// EventsubReconcileIntervalMinutes repairs missing stream subscriptions; zero disables
	// reconciliation.
	EventsubReconcileIntervalMinutes int `toml:"eventsub_reconcile_interval_minutes"`
	// FetchLogsRetentionDays is zero to retain fetch logs indefinitely.
	FetchLogsRetentionDays int `toml:"fetch_logs_retention_days"`
	// WebhookEventPayloadRetentionDays expires payloads while retaining webhook event rows.
	WebhookEventPayloadRetentionDays int `toml:"webhook_event_payload_retention_days"`
	// EventLogsRetentionDays applies to debug and info logs; warning and error logs have longer
	// retention.
	EventLogsRetentionDays int `toml:"event_logs_retention_days"`
	// RecordingWebhookDeliveryRetentionDays expires terminal outbox rows; zero disables pruning.
	// Pending and delivering rows are always retained.
	RecordingWebhookDeliveryRetentionDays int `toml:"recording_webhook_delivery_retention_days"`
	SessionCleanupIntervalMinutes         int `toml:"session_cleanup_interval_minutes"`
	// RecordingsRetentionIntervalMinutes sets the sweep cadence; schedules supply retention windows.
	// Zero disables automatic recording deletion.
	RecordingsRetentionIntervalMinutes int `toml:"recordings_retention_interval_minutes"`
	// StorageScanIntervalMinutes is zero to disable scans; playback still detects missing media.
	StorageScanIntervalMinutes int `toml:"storage_scan_interval_minutes"`
	// ArchivePosterIntervalMinutes retries posters that Twitch renders late; zero disables backfill.
	ArchivePosterIntervalMinutes int `toml:"archive_poster_interval_minutes"`
}

// LoggingConfig sets filtering and optional file output for server logs.
type LoggingConfig struct {
	LogToFile bool   `toml:"log_to_file"`
	LogDir    string `toml:"log_dir"`
	LogLevel  string `toml:"log_level"`
}

// PostgresPoolConfig sets connection limits and millisecond lifetime bounds.
type PostgresPoolConfig struct {
	MaxConns            int32 `toml:"max_conns"`
	MinConns            int32 `toml:"min_conns"`
	MaxConnLifetimeMs   int   `toml:"max_conn_lifetime_ms"`
	MaxConnIdleTimeMs   int   `toml:"max_conn_idle_time_ms"`
	HealthCheckPeriodMs int   `toml:"health_check_period_ms"`
}

// Config combines file settings, environment overrides, and the resolved server mode.
type Config struct {
	App        AppConfig
	Env        Environment
	ServerMode ServerModeConfig
}
