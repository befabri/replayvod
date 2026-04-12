package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/database"
	"github.com/befabri/replayvod/server/internal/logger"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
	"github.com/befabri/replayvod/server/internal/server"
)

func main() {
	// Load config
	configPath := "server/config.toml"
	if _, err := os.Stat("config.toml"); err == nil {
		configPath = "config.toml"
	}
	cfg := config.LoadConfig(configPath)
	if cfg == nil {
		slog.Error("Failed to load configuration")
		os.Exit(1)
	}

	// Setup logger
	level := logger.ParseLogLevel(cfg.App.Logging.LogLevel)
	log := logger.SetupLoggerWithLevel(
		os.Stderr, "replayvod",
		cfg.App.Logging.LogToFile, cfg.App.Logging.LogDir,
		cfg.App.Logging.SampleRate, level,
	)

	// Connect to database and create repository
	ctx := context.Background()
	var repo repository.Repository
	var pgPool *pgxpool.Pool
	var sqliteDB *sql.DB

	switch cfg.Env.DatabaseDriver {
	case "postgres":
		var err error
		pgPool, err = database.NewPostgresPool(ctx, cfg)
		if err != nil {
			log.Error("Failed to connect to PostgreSQL", "error", err)
			os.Exit(1)
		}
		defer pgPool.Close()
		log.Info("Connected to PostgreSQL", "host", cfg.Env.PostgresHost, "database", cfg.Env.PostgresDatabase)

		// Run migrations
		migrationsDir := "migrations/postgres"
		if _, err := os.Stat("server/migrations/postgres"); err == nil {
			migrationsDir = "server/migrations/postgres"
		}
		if err := database.MigratePostgres(ctx, pgPool, migrationsDir); err != nil {
			log.Error("Failed to run PostgreSQL migrations", "error", err)
			os.Exit(1)
		}

		repo = pgadapter.New(pggen.New(pgPool))

	case "sqlite":
		var err error
		sqliteDB, err = database.NewSQLiteDB(cfg.Env.SQLitePath)
		if err != nil {
			log.Error("Failed to open SQLite database", "error", err)
			os.Exit(1)
		}
		defer sqliteDB.Close()
		log.Info("Connected to SQLite", "path", cfg.Env.SQLitePath)

		// Run migrations
		migrationsDir := "migrations/sqlite"
		if _, err := os.Stat("server/migrations/sqlite"); err == nil {
			migrationsDir = "server/migrations/sqlite"
		}
		if err := database.MigrateSQLite(ctx, sqliteDB, migrationsDir); err != nil {
			log.Error("Failed to run SQLite migrations", "error", err)
			os.Exit(1)
		}

		repo = sqliteadapter.New(sqlitegen.New(sqliteDB))

	default:
		log.Error("Unknown database driver", "driver", cfg.Env.DatabaseDriver)
		os.Exit(1)
	}

	// Setup graceful shutdown
	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start server
	log.Info("Server starting", "address", cfg.GetAddress(), "database", cfg.Env.DatabaseDriver)
	srv := server.NewServer(cfg, repo, log)
	go srv.Start()

	// Wait for shutdown signal
	<-signalCtx.Done()
	log.Info("Shutting down server...")
	srv.Stop()
	logger.Close()

	os.Exit(0)
}
