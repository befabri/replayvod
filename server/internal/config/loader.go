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

// LoadConfig loads configuration from TOML file and environment variables.
func LoadConfig(path string) *Config {
	once.Do(func() {
		tomlPath = path
		config := &Config{}

		if err := godotenv.Load(); err == nil {
			slog.Info("Loaded environment from .env file")
		}

		config.App = getDefaultAppConfig()
		if err := loadTOML(tomlPath, &config.App); err != nil {
			slog.Error("Failed to load config.toml", "error", err)
			os.Exit(1)
		}

		if err := env.Parse(&config.Env); err != nil {
			slog.Error("Failed to parse environment config", "error", err)
			os.Exit(1)
		}

		validateAppConfig(&config.App)
		configPtr.Store(config)
	})
	return configPtr.Load()
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

	newConfig := &Config{
		App: newApp,
		Env: current.Env,
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

	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		for _, key := range undecoded {
			slog.Warn("Unknown config key (typo?)", "key", key.String())
		}
	}

	return nil
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
