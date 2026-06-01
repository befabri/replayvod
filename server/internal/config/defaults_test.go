package config

import (
	"reflect"
	"testing"
)

// TestValidateAppConfigClampsPollInterval pins the poll-interval floor: a zero
// or negative interval is clamped to 1 minute. The clamped value feeds
// time.Duration(...) * time.Minute tickers in main (live poll + metadata
// watcher), where a 0 would make a ticker panic / busy-loop.
func TestValidateAppConfigClampsPollInterval(t *testing.T) {
	for _, in := range []int{0, -5} {
		cfg := getDefaultAppConfig()
		cfg.Server.PollIntervalMinutes = in
		validateAppConfig(&cfg)
		if cfg.Server.PollIntervalMinutes != 1 {
			t.Fatalf("PollIntervalMinutes(%d) clamped to %d, want 1", in, cfg.Server.PollIntervalMinutes)
		}
	}

	// A positive value is left untouched.
	cfg := getDefaultAppConfig()
	cfg.Server.PollIntervalMinutes = 15
	validateAppConfig(&cfg)
	if cfg.Server.PollIntervalMinutes != 15 {
		t.Fatalf("PollIntervalMinutes(15) = %d, want it left untouched", cfg.Server.PollIntervalMinutes)
	}
}

// TestGetDefaultAppConfig pins the shipped defaults. validateAppConfig falls
// back to these exact values, so an accidental edit to a default would silently
// change clamp behaviour and the recorder's out-of-the-box settings. Asserting
// the whole struct also guards the booleans (Strict, EnableAV1, DisableHEVC,
// Scheduler.Enabled, LogToFile, Development) that have no other observable test.
func TestGetDefaultAppConfig(t *testing.T) {
	want := AppConfig{
		Server: ServerConfig{
			AllowedOrigins:      []string{"http://localhost:3000"},
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

	if got := getDefaultAppConfig(); !reflect.DeepEqual(got, want) {
		t.Fatalf("getDefaultAppConfig() = %+v, want %+v", got, want)
	}

	// The defaults must themselves survive validation unchanged — otherwise a
	// default and its own clamp rule disagree.
	cfg := getDefaultAppConfig()
	validateAppConfig(&cfg)
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("validateAppConfig rewrote a default value:\n got  %+v\n want %+v", cfg, want)
	}
}

// validBaseline returns a config whose every validated field sits at a valid
// value that is deliberately *different* from the default validateAppConfig
// would write on rejection. Using distinct values means a "left untouched"
// assertion observes the real input rather than coincidentally matching the
// default a wrongly-firing check would have stamped in.
func validBaseline() AppConfig {
	cfg := getDefaultAppConfig()
	cfg.Server.PollIntervalMinutes = 15
	cfg.Download.MaxConcurrent = 8
	cfg.Download.PreferredQuality = "720"
	cfg.Download.SegmentConcurrency = 6
	cfg.Download.NetworkAttempts = 9
	cfg.Download.ServerErrorAttempts = 7
	cfg.Download.CDNLagAttempts = 6
	cfg.Download.AuthRefreshAttempts = 4
	cfg.Download.MaxGapRatio = 0.5
	cfg.Download.MaxRestartGapSeconds = 200
	cfg.Download.MaxPartBytes = 4_000_000_000
	cfg.Download.MaxPartSeconds = 3600
	cfg.Download.MaxPartCount = 2048
	cfg.Logging.SampleRate = 0.5
	cfg.PostgresPool.MaxConns = 40
	cfg.PostgresPool.MinConns = 8
	return cfg
}

// TestValidateAppConfigLeavesValidConfigUntouched pins every happy-path side of
// validateAppConfig in one shot: a fully valid config passes through with no
// field rewritten. Because each baseline value differs from its clamp default,
// any check that wrongly fired would overwrite the field and fail DeepEqual.
func TestValidateAppConfigLeavesValidConfigUntouched(t *testing.T) {
	cfg := validBaseline()
	want := cfg
	validateAppConfig(&cfg)
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("validateAppConfig mutated a valid config:\n got  %+v\n want %+v", cfg, want)
	}
}

// TestValidateAppConfigFieldRules table-mutates exactly one field of an
// otherwise-valid config per case, then asserts the post-validation value.
//
// The cases come in two flavours, both needed to individually pin the boolean
// operators and comparisons in each check:
//
//   - rejection cases drive a field to an out-of-range value (zero, negative,
//     or, for the ratio/rate ranges, the upper bound) and assert the documented
//     default is stamped in. The zero cases sit exactly on each `<= 0` /
//     `>= 1` boundary, so they distinguish `<=` from `<` (and `>=` from `>`).
//   - "stays" cases set a valid value of 1 (or a mid-range float, or a custom
//     string) and assert it survives untouched. Value 1 sits one past the `<= 0`
//     boundary, so it distinguishes the literal `0` from `1`; the string and
//     mid-range float cases distinguish `==` from `!=` and pin the range bounds.
func TestValidateAppConfigFieldRules(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*AppConfig)
		got    func(*AppConfig) any
		want   any
	}{
		// PollIntervalMinutes: clamp <= 0 to 1.
		{"poll interval zero clamps", func(c *AppConfig) { c.Server.PollIntervalMinutes = 0 }, func(c *AppConfig) any { return c.Server.PollIntervalMinutes }, 1},
		{"poll interval negative clamps", func(c *AppConfig) { c.Server.PollIntervalMinutes = -5 }, func(c *AppConfig) any { return c.Server.PollIntervalMinutes }, 1},

		// MaxConcurrent: clamp <= 0 to 2.
		{"max concurrent zero clamps", func(c *AppConfig) { c.Download.MaxConcurrent = 0 }, func(c *AppConfig) any { return c.Download.MaxConcurrent }, 2},
		{"max concurrent negative clamps", func(c *AppConfig) { c.Download.MaxConcurrent = -3 }, func(c *AppConfig) any { return c.Download.MaxConcurrent }, 2},
		{"max concurrent one stays", func(c *AppConfig) { c.Download.MaxConcurrent = 1 }, func(c *AppConfig) any { return c.Download.MaxConcurrent }, 1},

		// PreferredQuality: clamp "" to "1080".
		{"preferred quality empty clamps", func(c *AppConfig) { c.Download.PreferredQuality = "" }, func(c *AppConfig) any { return c.Download.PreferredQuality }, "1080"},
		{"preferred quality custom stays", func(c *AppConfig) { c.Download.PreferredQuality = "480" }, func(c *AppConfig) any { return c.Download.PreferredQuality }, "480"},

		// SegmentConcurrency: clamp <= 0 to 4.
		{"segment concurrency zero clamps", func(c *AppConfig) { c.Download.SegmentConcurrency = 0 }, func(c *AppConfig) any { return c.Download.SegmentConcurrency }, 4},
		{"segment concurrency negative clamps", func(c *AppConfig) { c.Download.SegmentConcurrency = -1 }, func(c *AppConfig) any { return c.Download.SegmentConcurrency }, 4},
		{"segment concurrency one stays", func(c *AppConfig) { c.Download.SegmentConcurrency = 1 }, func(c *AppConfig) any { return c.Download.SegmentConcurrency }, 1},

		// NetworkAttempts: clamp <= 0 to 5.
		{"network attempts zero clamps", func(c *AppConfig) { c.Download.NetworkAttempts = 0 }, func(c *AppConfig) any { return c.Download.NetworkAttempts }, 5},
		{"network attempts one stays", func(c *AppConfig) { c.Download.NetworkAttempts = 1 }, func(c *AppConfig) any { return c.Download.NetworkAttempts }, 1},

		// ServerErrorAttempts: clamp <= 0 to 5.
		{"server error attempts zero clamps", func(c *AppConfig) { c.Download.ServerErrorAttempts = 0 }, func(c *AppConfig) any { return c.Download.ServerErrorAttempts }, 5},
		{"server error attempts one stays", func(c *AppConfig) { c.Download.ServerErrorAttempts = 1 }, func(c *AppConfig) any { return c.Download.ServerErrorAttempts }, 1},

		// CDNLagAttempts: clamp <= 0 to 3.
		{"cdn lag attempts zero clamps", func(c *AppConfig) { c.Download.CDNLagAttempts = 0 }, func(c *AppConfig) any { return c.Download.CDNLagAttempts }, 3},
		{"cdn lag attempts one stays", func(c *AppConfig) { c.Download.CDNLagAttempts = 1 }, func(c *AppConfig) any { return c.Download.CDNLagAttempts }, 1},

		// AuthRefreshAttempts: clamp <= 0 to 2.
		{"auth refresh attempts zero clamps", func(c *AppConfig) { c.Download.AuthRefreshAttempts = 0 }, func(c *AppConfig) any { return c.Download.AuthRefreshAttempts }, 2},
		{"auth refresh attempts one stays", func(c *AppConfig) { c.Download.AuthRefreshAttempts = 1 }, func(c *AppConfig) any { return c.Download.AuthRefreshAttempts }, 1},

		// MaxGapRatio: valid range is [0, 1); clamp outside to 0.01.
		{"max gap ratio negative clamps", func(c *AppConfig) { c.Download.MaxGapRatio = -0.5 }, func(c *AppConfig) any { return c.Download.MaxGapRatio }, 0.01},
		{"max gap ratio one clamps", func(c *AppConfig) { c.Download.MaxGapRatio = 1.0 }, func(c *AppConfig) any { return c.Download.MaxGapRatio }, 0.01},
		{"max gap ratio above one clamps", func(c *AppConfig) { c.Download.MaxGapRatio = 1.5 }, func(c *AppConfig) any { return c.Download.MaxGapRatio }, 0.01},
		{"max gap ratio zero stays", func(c *AppConfig) { c.Download.MaxGapRatio = 0 }, func(c *AppConfig) any { return c.Download.MaxGapRatio }, 0.0},
		{"max gap ratio mid stays", func(c *AppConfig) { c.Download.MaxGapRatio = 0.25 }, func(c *AppConfig) any { return c.Download.MaxGapRatio }, 0.25},

		// MaxRestartGapSeconds: clamp <= 0 to 120.
		{"max restart gap zero clamps", func(c *AppConfig) { c.Download.MaxRestartGapSeconds = 0 }, func(c *AppConfig) any { return c.Download.MaxRestartGapSeconds }, 120},
		{"max restart gap negative clamps", func(c *AppConfig) { c.Download.MaxRestartGapSeconds = -10 }, func(c *AppConfig) any { return c.Download.MaxRestartGapSeconds }, 120},
		{"max restart gap one stays", func(c *AppConfig) { c.Download.MaxRestartGapSeconds = 1 }, func(c *AppConfig) any { return c.Download.MaxRestartGapSeconds }, 1},

		// MaxPartBytes / MaxPartSeconds: 0 disables splitting; negative
		// values are typos and clamp to disabled rather than silently
		// relying on downloader guards.
		{"max part bytes negative clamps to disabled", func(c *AppConfig) { c.Download.MaxPartBytes = -1 }, func(c *AppConfig) any { return c.Download.MaxPartBytes }, int64(0)},
		{"max part bytes zero stays disabled", func(c *AppConfig) { c.Download.MaxPartBytes = 0 }, func(c *AppConfig) any { return c.Download.MaxPartBytes }, int64(0)},
		{"max part bytes one stays", func(c *AppConfig) { c.Download.MaxPartBytes = 1 }, func(c *AppConfig) any { return c.Download.MaxPartBytes }, int64(1)},
		{"max part seconds negative clamps to disabled", func(c *AppConfig) { c.Download.MaxPartSeconds = -1 }, func(c *AppConfig) any { return c.Download.MaxPartSeconds }, 0},
		{"max part seconds zero stays disabled", func(c *AppConfig) { c.Download.MaxPartSeconds = 0 }, func(c *AppConfig) any { return c.Download.MaxPartSeconds }, 0},
		{"max part seconds one stays", func(c *AppConfig) { c.Download.MaxPartSeconds = 1 }, func(c *AppConfig) any { return c.Download.MaxPartSeconds }, 1},

		// MaxPartCount: threshold split cap, separate from the internal
		// discontinuity cap.
		{"max part count zero clamps", func(c *AppConfig) { c.Download.MaxPartCount = 0 }, func(c *AppConfig) any { return c.Download.MaxPartCount }, int32(1024)},
		{"max part count negative clamps", func(c *AppConfig) { c.Download.MaxPartCount = -1 }, func(c *AppConfig) any { return c.Download.MaxPartCount }, int32(1024)},
		{"max part count one stays", func(c *AppConfig) { c.Download.MaxPartCount = 1 }, func(c *AppConfig) any { return c.Download.MaxPartCount }, int32(1)},

		// SampleRate: valid range is (0, 1]; clamp outside to 1.0.
		{"sample rate zero clamps", func(c *AppConfig) { c.Logging.SampleRate = 0 }, func(c *AppConfig) any { return c.Logging.SampleRate }, 1.0},
		{"sample rate negative clamps", func(c *AppConfig) { c.Logging.SampleRate = -0.2 }, func(c *AppConfig) any { return c.Logging.SampleRate }, 1.0},
		{"sample rate above one clamps", func(c *AppConfig) { c.Logging.SampleRate = 1.5 }, func(c *AppConfig) any { return c.Logging.SampleRate }, 1.0},
		{"sample rate mid stays", func(c *AppConfig) { c.Logging.SampleRate = 0.3 }, func(c *AppConfig) any { return c.Logging.SampleRate }, 0.3},
		{"sample rate one stays", func(c *AppConfig) { c.Logging.SampleRate = 1.0 }, func(c *AppConfig) any { return c.Logging.SampleRate }, 1.0},

		// PostgresPool.MaxConns: clamp <= 0 to 25 (int32).
		{"max conns zero clamps", func(c *AppConfig) { c.PostgresPool.MaxConns = 0 }, func(c *AppConfig) any { return c.PostgresPool.MaxConns }, int32(25)},
		{"max conns negative clamps", func(c *AppConfig) { c.PostgresPool.MaxConns = -1 }, func(c *AppConfig) any { return c.PostgresPool.MaxConns }, int32(25)},
		{"max conns one stays", func(c *AppConfig) { c.PostgresPool.MaxConns = 1 }, func(c *AppConfig) any { return c.PostgresPool.MaxConns }, int32(1)},

		// PostgresPool.MinConns: clamp <= 0 to 5 (int32).
		{"min conns zero clamps", func(c *AppConfig) { c.PostgresPool.MinConns = 0 }, func(c *AppConfig) any { return c.PostgresPool.MinConns }, int32(5)},
		{"min conns negative clamps", func(c *AppConfig) { c.PostgresPool.MinConns = -1 }, func(c *AppConfig) any { return c.PostgresPool.MinConns }, int32(5)},
		{"min conns one stays", func(c *AppConfig) { c.PostgresPool.MinConns = 1 }, func(c *AppConfig) any { return c.PostgresPool.MinConns }, int32(1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validBaseline()
			tc.mutate(&cfg)
			validateAppConfig(&cfg)
			if got := tc.got(&cfg); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("after validateAppConfig: got %#v, want %#v", got, tc.want)
			}
		})
	}
}
