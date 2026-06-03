package config

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"

	"github.com/BurntSushi/toml"
	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

var (
	configPtr atomic.Pointer[Config]
	configMu  sync.Mutex
	tomlPath  string
	once      sync.Once
)

// LoadConfig loads configuration from the TOML file and environment variables.
// It runs once; a failure is fatal because the process cannot start without it.
func LoadConfig(path string) *Config {
	once.Do(func() {
		config, err := loadConfig(path)
		if err != nil {
			slog.Error("Failed to load configuration", "error", err)
			os.Exit(1)
		}
		tomlPath = path
		configPtr.Store(config)
	})
	return configPtr.Load()
}

// loadConfig assembles the configuration from .env, the TOML file, and the
// environment. It returns an error rather than exiting so it can be tested and
// reused by the once-guarded LoadConfig.
func loadConfig(path string) (*Config, error) {
	if err := loadDotenv(); err != nil {
		return nil, err
	}
	config := &Config{App: getDefaultAppConfig()}
	if err := loadTOML(path, &config.App); err != nil {
		return nil, fmt.Errorf("load config.toml: %w", err)
	}
	if err := env.Parse(&config.Env); err != nil {
		return nil, fmt.Errorf("parse environment config: %w", err)
	}
	if err := validateEnvironment(&config.Env); err != nil {
		return nil, fmt.Errorf("validate environment config: %w", err)
	}
	config.ServerMode = ServerModeConfigFromEnv(config.Env)
	validateAppConfig(&config.App)
	applyEnvOverrides(&config.App, config.Env)
	return config, nil
}

// applyEnvOverrides lets specific environment variables win over config.toml.
// Today only DEVELOPMENT: the Docker image sets it false so the published image
// runs in production mode regardless of the baked config.toml, while local runs
// (no env var) keep the config.toml value.
func applyEnvOverrides(app *AppConfig, env Environment) {
	if env.DevelopmentOverride != nil {
		app.Development = *env.DevelopmentOverride
	}
}

// loadDotenv loads .env into the process environment when present, first
// rejecting duplicate keys (godotenv silently keeps the last value, which would
// hide a misconfiguration). A missing .env is not an error.
func loadDotenv() error {
	if err := validateDotenvNoDuplicateKeys(".env"); err != nil {
		return fmt.Errorf("validate .env file: %w", err)
	}
	switch err := godotenv.Load(); {
	case err == nil:
		slog.Info("Loaded environment from .env file")
	case !os.IsNotExist(err):
		return fmt.Errorf("load .env file: %w", err)
	}
	return nil
}

// ReloadAppConfig reloads config.toml without restarting.
func ReloadAppConfig() error {
	configMu.Lock()
	defer configMu.Unlock()

	current := configPtr.Load()
	if current == nil {
		return fmt.Errorf("config not loaded yet")
	}

	newApp := getDefaultAppConfig()
	if err := loadTOML(tomlPath, &newApp); err != nil {
		return fmt.Errorf("failed to parse config.toml: %w", err)
	}

	validateAppConfig(&newApp)
	applyEnvOverrides(&newApp, current.Env)

	newConfig := &Config{
		App:        newApp,
		Env:        current.Env,
		ServerMode: current.ServerMode,
	}

	configPtr.Store(newConfig)
	slog.Info("Config reloaded successfully", "path", tomlPath)
	return nil
}

func loadTOML(path string, config *AppConfig) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		slog.Info("No config.toml found, using defaults", "path", path)
		return nil
	}

	meta, err := toml.DecodeFile(path, config)
	if err != nil {
		return fmt.Errorf("parse error in %s: %w", path, err)
	}

	slog.Info("Loaded config.toml", "path", path)

	for _, key := range meta.Undecoded() {
		if isDeprecatedConfigKey(key.String()) {
			slog.Warn("Deprecated config key ignored", "key", key.String())
			continue
		}
		slog.Warn("Unknown config key (typo?)", "key", key.String())
	}

	return nil
}

func isDeprecatedConfigKey(key string) bool {
	return key == "logging.sample_rate"
}

// GetConfig returns the loaded configuration (thread-safe).
func GetConfig() *Config {
	cfg := configPtr.Load()
	if cfg == nil {
		slog.Error("Config not loaded. Call LoadConfig() first.")
		os.Exit(1)
	}
	return cfg
}
