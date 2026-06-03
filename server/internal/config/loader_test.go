package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestLoadTOML covers every branch: a missing file leaves the passed-in
// defaults untouched, a well-formed file decodes onto them, a malformed file is
// an error, and an unknown key is tolerated (warned, not fatal) so a typo never
// blocks boot.
func TestLoadTOML(t *testing.T) {
	t.Run("missing file keeps defaults", func(t *testing.T) {
		app := getDefaultAppConfig()
		if err := loadTOML(filepath.Join(t.TempDir(), "absent.toml"), &app); err != nil {
			t.Fatalf("loadTOML(missing) = %v, want nil", err)
		}
		if app.Download.MaxConcurrent != 2 {
			t.Fatalf("defaults disturbed: MaxConcurrent = %d, want 2", app.Download.MaxConcurrent)
		}
	})

	t.Run("decodes values", func(t *testing.T) {
		path := writeFile(t, t.TempDir(), "config.toml", "development = true\n[download]\nmax_concurrent = 7\n")
		app := getDefaultAppConfig()
		if err := loadTOML(path, &app); err != nil {
			t.Fatalf("loadTOML = %v, want nil", err)
		}
		if !app.Development || app.Download.MaxConcurrent != 7 {
			t.Fatalf("decoded Development=%v MaxConcurrent=%d, want true/7", app.Development, app.Download.MaxConcurrent)
		}
	})

	t.Run("malformed is an error", func(t *testing.T) {
		path := writeFile(t, t.TempDir(), "config.toml", "this = = not toml")
		app := getDefaultAppConfig()
		if err := loadTOML(path, &app); err == nil {
			t.Fatal("loadTOML(malformed) = nil, want error")
		}
	})

	t.Run("unknown key is tolerated", func(t *testing.T) {
		path := writeFile(t, t.TempDir(), "config.toml", "[download]\nmax_concurrent = 5\nbogus_typo = 1\n")
		app := getDefaultAppConfig()
		if err := loadTOML(path, &app); err != nil {
			t.Fatalf("loadTOML(unknown key) = %v, want nil (warn only)", err)
		}
		if app.Download.MaxConcurrent != 5 {
			t.Fatalf("MaxConcurrent = %d, want 5", app.Download.MaxConcurrent)
		}
	})

	t.Run("deprecated sample rate is tolerated", func(t *testing.T) {
		path := writeFile(t, t.TempDir(), "config.toml", "[logging]\nsample_rate = 0.5\nlog_level = \"warn\"\n")
		app := getDefaultAppConfig()
		if err := loadTOML(path, &app); err != nil {
			t.Fatalf("loadTOML(deprecated sample_rate) = %v, want nil", err)
		}
		if app.Logging.LogLevel != "warn" {
			t.Fatalf("LogLevel = %q, want warn", app.Logging.LogLevel)
		}
	})
}

func TestIsDeprecatedConfigKey(t *testing.T) {
	if !isDeprecatedConfigKey("logging.sample_rate") {
		t.Fatal("logging.sample_rate should be recognized as deprecated")
	}
	if isDeprecatedConfigKey("download.max_concurrent") {
		t.Fatal("active config key reported as deprecated")
	}
}

// clearServerModeEnv forces the EventSub-related env vars present-but-empty so
// loadConfig sees app-managed (unset) server mode regardless of the runner's
// environment.
func clearServerModeEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SESSION_SECRET", "0123456789abcdef0123456789abcdef")
	for _, k := range []string{"SERVER_MODE", "WEBHOOK_CALLBACK_URL", "RELAY_INGEST_URL", "RELAY_SUBSCRIBE_URL", "RELAY_LOCAL_CALLBACK_URL"} {
		t.Setenv(k, "")
	}
}

// TestLoadConfig covers the assembly path that LoadConfig wraps: defaults when
// no TOML exists, and the two failure modes that must surface as errors rather
// than a half-built config (malformed TOML, invalid SERVER_MODE).
func TestLoadConfig(t *testing.T) {
	t.Run("defaults with no toml", func(t *testing.T) {
		clearServerModeEnv(t)
		cfg, err := loadConfig(filepath.Join(t.TempDir(), "absent.toml"))
		if err != nil {
			t.Fatalf("loadConfig = %v, want nil", err)
		}
		if cfg.Env.ServerMode != "" {
			t.Fatalf("ServerMode = %q, want empty", cfg.Env.ServerMode)
		}
		if cfg.App.Download.MaxConcurrent != 2 {
			t.Fatalf("validated defaults not applied: MaxConcurrent = %d, want 2", cfg.App.Download.MaxConcurrent)
		}
		if cfg.ServerMode.SetupRequired() != true {
			t.Fatal("unset server mode should require setup")
		}
	})

	t.Run("malformed toml is an error", func(t *testing.T) {
		clearServerModeEnv(t)
		path := writeFile(t, t.TempDir(), "config.toml", "== not toml ==")
		if _, err := loadConfig(path); err == nil {
			t.Fatal("loadConfig(malformed toml) = nil, want error")
		}
	})

	t.Run("invalid server mode is an error", func(t *testing.T) {
		t.Setenv("SERVER_MODE", "magic")
		if _, err := loadConfig(filepath.Join(t.TempDir(), "absent.toml")); err == nil {
			t.Fatal("loadConfig(invalid SERVER_MODE) = nil, want error")
		}
	})
}

// TestReloadAppConfig covers the live-reload path: it errors before the first
// load, reloads the TOML while preserving Env and ServerMode, and surfaces a
// malformed TOML as an error instead of swapping in a broken config.
func TestReloadAppConfig(t *testing.T) {
	saved := configPtr.Load()
	savedPath := tomlPath
	t.Cleanup(func() {
		configPtr.Store(saved)
		tomlPath = savedPath
	})

	t.Run("errors when not loaded", func(t *testing.T) {
		configPtr.Store(nil)
		if err := ReloadAppConfig(); err == nil {
			t.Fatal("ReloadAppConfig() = nil, want error before first load")
		}
	})

	t.Run("reloads toml and preserves env and server mode", func(t *testing.T) {
		tomlPath = writeFile(t, t.TempDir(), "config.toml", "[download]\nmax_concurrent = 9\n")
		configPtr.Store(&Config{
			App:        getDefaultAppConfig(),
			Env:        Environment{Host: "preserved-host"},
			ServerMode: ServerModeConfig{Mode: ServerModeOff},
		})
		if err := ReloadAppConfig(); err != nil {
			t.Fatalf("ReloadAppConfig() = %v, want nil", err)
		}
		got := configPtr.Load()
		if got.App.Download.MaxConcurrent != 9 {
			t.Fatalf("reloaded MaxConcurrent = %d, want 9", got.App.Download.MaxConcurrent)
		}
		if got.Env.Host != "preserved-host" || got.ServerMode.Mode != ServerModeOff {
			t.Fatalf("reload did not preserve Env/ServerMode: %+v / %+v", got.Env, got.ServerMode)
		}
	})

	t.Run("malformed toml leaves config untouched", func(t *testing.T) {
		tomlPath = writeFile(t, t.TempDir(), "config.toml", "== not toml ==")
		original := &Config{App: getDefaultAppConfig()}
		configPtr.Store(original)
		if err := ReloadAppConfig(); err == nil {
			t.Fatal("ReloadAppConfig(malformed) = nil, want error")
		}
		if configPtr.Load() != original {
			t.Fatal("a failed reload must not swap in a new config")
		}
	})
}

// TestGetConfigReturnsStored pins the happy path of the accessor. The not-loaded
// path calls os.Exit and is intentionally left to the bootstrap.
func TestGetConfigReturnsStored(t *testing.T) {
	saved := configPtr.Load()
	t.Cleanup(func() { configPtr.Store(saved) })

	want := &Config{Env: Environment{Host: "stored"}}
	configPtr.Store(want)
	if got := GetConfig(); got != want {
		t.Fatal("GetConfig() did not return the stored config")
	}
}
