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
	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/logger"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
	"github.com/befabri/replayvod/server/internal/scheduler"
	"github.com/befabri/replayvod/server/internal/server"
	"github.com/befabri/replayvod/server/internal/service/eventsubservice"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/befabri/replayvod/server/migrations"

	"time"
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

		if err := database.MigratePostgres(ctx, pgPool, migrations.Postgres()); err != nil {
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

		if err := database.MigrateSQLite(ctx, sqliteDB, migrations.SQLite()); err != nil {
			log.Error("Failed to run SQLite migrations", "error", err)
			os.Exit(1)
		}

		repo = sqliteadapter.New(sqlitegen.New(sqliteDB))

	default:
		log.Error("Unknown database driver", "driver", cfg.Env.DatabaseDriver)
		os.Exit(1)
	}

	// Create Twitch client with fetch log recorder.
	twitchClient := twitch.NewClient(cfg.Env.TwitchClientID, cfg.Env.TwitchSecret, log)
	twitchClient.SetFetchLogRecorder(&fetchLogRecorder{repo: repo, log: log})

	// Create session manager
	secureCookie := cfg.Env.Host != "localhost" && cfg.Env.Host != "0.0.0.0"
	sessionMgr, err := session.NewManager(repo, cfg.Env.SessionSecret, secureCookie, log)
	if err != nil {
		log.Error("Failed to create session manager", "error", err)
		os.Exit(1)
	}

	// Seed whitelist from config (idempotent). OWNER_TWITCH_ID is always seeded
	// so the owner can log in even with whitelist enabled and an empty DB.
	// Runtime auth only consults the DB — config is bootstrap-only.
	if err := session.SeedWhitelist(ctx, repo, cfg.Env.OwnerTwitchID, cfg.Env.WhitelistedUserIDs, log); err != nil {
		log.Error("Failed to seed whitelist", "error", err)
		os.Exit(1)
	}

	// Storage backend. Only local is wired in Phase 4; S3 + rclone land in Phase 8.
	var store storage.Storage
	switch cfg.App.Storage.Type {
	case "", "local":
		local, err := storage.NewLocal(cfg.App.Storage.LocalPath)
		if err != nil {
			log.Error("Failed to init local storage", "error", err)
			os.Exit(1)
		}
		store = local
		log.Info("Storage initialized", "type", "local", "path", local.Root)
	default:
		log.Error("Unsupported storage type", "type", cfg.App.Storage.Type)
		os.Exit(1)
	}

	// Downloader service
	dl := downloader.NewService(cfg, repo, store, log)

	// Scheduler: wire the EventSub manager first so the snapshot task
	// has something to call. Skip entirely if cfg.App.Scheduler.Enabled
	// is false — useful for one-off CLI invocations or tests.
	var sched *scheduler.Service
	if cfg.App.Scheduler.Enabled {
		esvc := eventsubservice.New(repo, twitchClient, cfg.Env.WebhookCallbackURL, cfg.Env.HMACSecret, log)
		sched = scheduler.NewService(repo, log, 15*time.Second)
		if err := scheduler.RegisterStandardTasks(sched, cfg, repo, esvc, log); err != nil {
			log.Error("Failed to register scheduler tasks", "error", err)
			os.Exit(1)
		}
		if err := sched.Start(ctx); err != nil {
			log.Error("Failed to start scheduler", "error", err)
			os.Exit(1)
		}
		log.Info("Scheduler started")
	} else {
		log.Info("Scheduler disabled by config")
	}

	// Setup graceful shutdown
	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start server
	log.Info("Server starting", "address", cfg.GetAddress(), "database", cfg.Env.DatabaseDriver)
	srv := server.NewServer(cfg, repo, sessionMgr, twitchClient, store, dl, log)
	go srv.Start()

	// Wait for shutdown signal
	<-signalCtx.Done()
	log.Info("Shutting down server...")
	if sched != nil {
		sched.Stop()
	}
	srv.Stop()
	logger.Close()

	os.Exit(0)
}
