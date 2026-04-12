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

	// ServiceAccountOAuthToken is an optional Twitch user refresh token
	// (not an access token) used for authenticated playback — unlocks
	// ad-free recording on Turbo accounts and HEVC variants on channels
	// whose transcode ladder serves HEVC only to authenticated viewers.
	// Empty disables authenticated playback (anonymous requests only,
	// works for public non-subscriber-only streams). Lives in the env
	// rather than config.toml because it's a long-lived secret;
	// config.toml is not expected to carry secrets.
	ServiceAccountOAuthToken string `env:"TWITCH_SERVICE_ACCOUNT_REFRESH_TOKEN"`

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

	// ScratchDir is where subprocess downloads (yt-dlp, ffmpeg) land
	// before being uploaded to the configured Storage backend.
	//
	// Default keeps scratch on the same filesystem as VideoDir. This
	// is deliberate for the local-storage case: LocalStorage.Save
	// uses os.Rename under the hood, which is metadata-only within a
	// filesystem but falls back to copy+unlink across devices.
	// Pointing ScratchDir at os.TempDir() (often tmpfs on Linux) when
	// VideoDir lives on spinning disk turns a 10 GB "rename" into
	// minutes of wasted I/O.
	//
	// The S3 backend doesn't care where scratch lives — Save streams
	// the file out over the network and the rename penalty doesn't
	// apply. Operators on S3 with spare RAM may prefer pointing
	// ScratchDir at a tmpfs mount so large writes don't churn the
	// data disk.
	ScratchDir string `env:"SCRATCH_DIR" envDefault:"./data/.scratch"`
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

// DownloadConfig controls the native Go HLS downloader. Field docs
// follow the retry-and-resume model documented in
// .docs/spec/download-pipeline.md. Retry budgets are *per segment*
// unless otherwise noted; exhausting any one of them without a
// successful completion escalates the segment to a permanent failure
// (which then goes through the tolerant/strict mode policy).
type DownloadConfig struct {
	// MaxConcurrent caps the number of in-flight jobs service-wide.
	// Default 2. Combined with SegmentConcurrency, sets the
	// aggregate host-connection cap (MaxConcurrent *
	// SegmentConcurrency) on the shared http.Transport — every
	// active job shares the same Twitch host budget.
	MaxConcurrent int `toml:"max_concurrent"`

	// PreferredQuality is the starting quality for new jobs, as a
	// numeric string ("1080", "720", "480", "360", "160"). The
	// Stage 3 fallback chain downgrades from here if the requested
	// quality isn't available on the master playlist. New jobs
	// always start at this value; prior jobs' downgrades don't
	// stick across jobs.
	PreferredQuality string `toml:"preferred_quality"`

	// SegmentConcurrency is the size of the per-job fetcher worker
	// pool. Default 4. Each worker owns one HTTP request at a time
	// over the shared transport; the queue feeding them is a
	// buffered channel of capacity 2*SegmentConcurrency (producer
	// blocks when full → natural backpressure).
	SegmentConcurrency int `toml:"segment_concurrency"`

	// NetworkAttempts is the per-segment transport-error retry
	// budget — timeouts, reset connections, DNS failures, truncated
	// body reads. Default 5.
	NetworkAttempts int `toml:"network_attempts"`

	// ServerErrorAttempts is the per-segment retry budget for
	// 429/5xx responses. Honors Retry-After when present. Default 5.
	ServerErrorAttempts int `toml:"server_error_attempts"`

	// CDNLagAttempts is the per-segment retry budget for 404/410 —
	// "segment not yet on edge, or just rolled off the window."
	// Tight by design (default 3, at half-targetDuration intervals):
	// live HLS segments propagate within a few seconds or they
	// never will.
	CDNLagAttempts int `toml:"cdn_lag_attempts"`

	// AuthRefreshAttempts is the per-segment budget for
	// token-refresh cycles triggered by non-permanent 401/403
	// responses. Default 2. Permanent entitlement codes
	// (unauthorized_entitlements, etc.) fail immediately and do not
	// consume this budget.
	AuthRefreshAttempts int `toml:"auth_refresh_attempts"`

	// MaxGapRatio is the tolerant-mode ceiling: permanent segment
	// failures exceeding this fraction of observed segments fail
	// the whole job. Default 0.01 (1%). Ignored when Strict=true.
	MaxGapRatio float64 `toml:"max_gap_ratio"`

	// Strict flips the orchestrator from tolerant mode (record a
	// gap, keep going) to strict mode (any permanent segment
	// failure fails the job). Default false. Opt-in for operators
	// who would rather fail fast than record a partial VOD.
	Strict bool `toml:"strict"`

	// EnableAV1 opts into AV1 codec support at Stage 3. Default
	// false. When true, av01.* variants are retained during codec
	// filtering and the master-playlist `supported_codecs` query
	// parameter includes av1. Code paths exist even when this is
	// false — only runtime selection is gated.
	EnableAV1 bool `toml:"enable_av1"`

	// DisableHEVC is a config escape hatch: drop hvc1/hev1 variants
	// at Stage 3 even when the master playlist offers them. Default
	// false. Used when ffmpeg's HEVC build on the operator's system
	// has a known bug, or the downstream player can't decode HEVC.
	DisableHEVC bool `toml:"disable_hevc"`

	// MaxRestartGapSeconds bounds the size of any single gap
	// recorded on restart before the resume path splits the
	// recording into a new part. Default 120. Prevents a server
	// outage from embedding a massive hole in one MP4.
	MaxRestartGapSeconds int `toml:"max_restart_gap_seconds"`

	// AudioRate is legacy. The native pipeline's `-c copy` remux
	// carries Twitch's native sample rate through untouched; this
	// field is only read by the yt-dlp shim path while the rewrite
	// is in progress. Dropped once the native downloader ships.
	AudioRate int `toml:"audio_rate"`
}

type StorageConfig struct {
	Type      string   `toml:"type"`
	LocalPath string   `toml:"local_path"`
	S3        S3Config `toml:"s3"`
}

// S3Config holds S3-compatible storage options. Leave AccessKey and
// SecretKey empty to delegate to the AWS SDK's default credential
// chain (env vars AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY, shared
// credentials file, EC2 instance metadata, etc.) — recommended for
// production IAM-role deployments.
//
// Endpoint is optional; empty uses AWS default resolution. Set for
// self-hosted (MinIO, Ceph) or alternate clouds (R2, DO Spaces,
// Wasabi).
//
// UsePathStyle is a 3-state toggle: nil pointer lets the backend pick
// (path-style when Endpoint is set — required for MinIO — otherwise
// virtual-hosted style matching AWS). Set explicitly when a provider
// disagrees with the heuristic (some DO Spaces / Wasabi setups
// prefer virtual-hosted even with a custom endpoint).
type S3Config struct {
	Endpoint     string `toml:"endpoint"`
	Bucket       string `toml:"bucket"`
	Region       string `toml:"region"`
	AccessKey    string `toml:"access_key"`
	SecretKey    string `toml:"secret_key"`
	UsePathStyle *bool  `toml:"use_path_style"`
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
