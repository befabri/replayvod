package config

// Environment contains all settings from .env — infrastructure, secrets, paths.
// These are static and require a restart to change.
type Environment struct {
	// Database
	DatabaseDriver   string `env:"DATABASE_DRIVER" envDefault:"postgres"`
	PostgresHost     string `env:"POSTGRES_HOST" envDefault:"127.0.0.1"`
	PostgresPort     int    `env:"POSTGRES_PORT" envDefault:"5432"`
	PostgresDatabase string `env:"POSTGRES_DATABASE" envDefault:"replayvod"`
	PostgresUser     string `env:"POSTGRES_USER" envDefault:"postgres"`
	PostgresPassword string `env:"POSTGRES_PASSWORD"`
	PostgresSSLMode  string `env:"POSTGRES_SSL_MODE" envDefault:"disable"`
	SQLitePath       string `env:"SQLITE_PATH" envDefault:"./data/replayvod.db"`

	// Twitch
	TwitchClientID string `env:"TWITCH_CLIENT_ID"`
	TwitchSecret   string `env:"TWITCH_SECRET"`
	HMACSecret     string `env:"HMAC_SECRET"`

	// Server
	Host string `env:"HOST" envDefault:"0.0.0.0"`
	Port int    `env:"PORT" envDefault:"8080"`

	// Security
	SessionSecret      string `env:"SESSION_SECRET"`
	WhitelistEnabled   bool   `env:"WHITELIST_ENABLED" envDefault:"false"`
	WhitelistedUserIDs string `env:"WHITELISTED_USER_IDS"`
	OwnerTwitchID      string `env:"OWNER_TWITCH_ID"`

	// URLs
	CallbackURL        string `env:"CALLBACK_URL" envDefault:"http://localhost:8080/api/v1/auth/twitch/callback"`
	WebhookCallbackURL string `env:"WEBHOOK_CALLBACK_URL" envDefault:"http://localhost:8080/api/v1/webhook/callback"`
	FrontendURL        string `env:"FRONTEND_URL" envDefault:"http://localhost:3000"`

	// Paths
	VideoDir     string `env:"VIDEO_DIR" envDefault:"./data/videos"`
	ThumbnailDir string `env:"THUMBNAIL_DIR" envDefault:"./data/thumbnails"`
	YtdlpPath    string `env:"YTDLP_PATH" envDefault:"yt-dlp"`
	DashboardDir string `env:"DASHBOARD_DIR"`
}

// AppConfig contains behavior settings from config.toml — hot-reloadable.
type AppConfig struct {
	Server       ServerConfig       `toml:"server"`
	Download     DownloadConfig     `toml:"download"`
	Storage      StorageConfig      `toml:"storage"`
	Scheduler    SchedulerConfig    `toml:"scheduler"`
	Logging      LoggingConfig      `toml:"logging"`
	PostgresPool PostgresPoolConfig `toml:"postgres"`
	Development  bool               `toml:"development"`
}

type ServerConfig struct {
	AllowedOrigins []string `toml:"allowed_origins"`
}

type DownloadConfig struct {
	MaxConcurrent  int    `toml:"max_concurrent"`
	DefaultQuality string `toml:"default_quality"`
	AudioRate      int    `toml:"audio_rate"`
}

type StorageConfig struct {
	Type      string       `toml:"type"`
	LocalPath string       `toml:"local_path"`
	S3        S3Config     `toml:"s3"`
	Rclone    RcloneConfig `toml:"rclone"`
}

type S3Config struct {
	Endpoint  string `toml:"endpoint"`
	Bucket    string `toml:"bucket"`
	Region    string `toml:"region"`
	AccessKey string `toml:"access_key"`
	SecretKey string `toml:"secret_key"`
}

type RcloneConfig struct {
	Remote string `toml:"remote"`
}

type SchedulerConfig struct {
	Enabled                     bool `toml:"enabled"`
	ThumbnailIntervalMinutes    int  `toml:"thumbnail_interval_minutes"`
	EventsubIntervalMinutes     int  `toml:"eventsub_interval_minutes"`
	CategoryArtIntervalMinutes  int  `toml:"category_art_interval_minutes"`
	TokenCleanupIntervalMinutes int  `toml:"token_cleanup_interval_minutes"`
	// FetchLogsRetentionDays prunes fetch_logs older than this on a
	// daily interval. 0 disables the task (keep forever).
	FetchLogsRetentionDays int `toml:"fetch_logs_retention_days"`
	// WebhookEventPayloadRetentionDays trims the payload column (not
	// the row) on webhook_events older than this.
	WebhookEventPayloadRetentionDays int `toml:"webhook_event_payload_retention_days"`
	// EventLogsRetentionDays prunes debug+info event_logs older than
	// this. warn+error rows have a longer retention managed below.
	EventLogsRetentionDays int `toml:"event_logs_retention_days"`
	// SessionCleanupIntervalMinutes sweeps expired sessions.
	SessionCleanupIntervalMinutes int `toml:"session_cleanup_interval_minutes"`
}

type LoggingConfig struct {
	LogToFile  bool    `toml:"log_to_file"`
	LogDir     string  `toml:"log_dir"`
	LogLevel   string  `toml:"log_level"`
	SampleRate float64 `toml:"sample_rate"`
}

type PostgresPoolConfig struct {
	MaxConns            int32 `toml:"max_conns"`
	MinConns            int32 `toml:"min_conns"`
	MaxConnLifetimeMs   int   `toml:"max_conn_lifetime_ms"`
	MaxConnIdleTimeMs   int   `toml:"max_conn_idle_time_ms"`
	HealthCheckPeriodMs int   `toml:"health_check_period_ms"`
}

// Config is the combined configuration.
type Config struct {
	App AppConfig
	Env Environment
}
