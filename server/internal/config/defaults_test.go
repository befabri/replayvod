package config

import "testing"

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
