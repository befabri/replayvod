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
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/logger"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
	"github.com/befabri/replayvod/server/internal/scheduler"
	"github.com/befabri/replayvod/server/internal/server"
	"github.com/befabri/replayvod/server/internal/service/eventsub"
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

		repo = pgadapter.New(pgPool)

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

	// Create Twitch client. Audit every Helix call into the fetch_logs
	// table — adapter lives at the wiring site so twitch stays unaware of
	// the repository, and repository stays unaware of twitch.
	twitchClient := twitch.NewClient(cfg.Env.TwitchClientID, cfg.Env.TwitchSecret, log)
	twitchClient.SetFetchLogRecorder(twitch.RecorderFunc(func(ctx context.Context, e twitch.FetchLogEntry) {
		var errStr *string
		if e.Error != "" {
			errStr = &e.Error
		}
		if err := repo.CreateFetchLog(ctx, &repository.FetchLogInput{
			UserID:        e.UserID,
			FetchType:     e.FetchType,
			BroadcasterID: e.BroadcasterID,
			Status:        e.Status,
			Error:         errStr,
			DurationMs:    e.DurationMs,
		}); err != nil {
			log.Warn("failed to record fetch log", "error", err)
		}
	}))

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

	// Storage backend selection. Fail fast on missing required fields
	// for the chosen type — a misconfigured S3 backend only surfaces
	// on the first download otherwise, which is a far worse diagnostic.
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

	case "s3":
		// Path-style default: on when a custom endpoint is set (MinIO
		// and most self-hosted S3 implementations require it), off for
		// AWS. Operators on providers that disagree with the heuristic
		// (some Wasabi/DO Spaces setups prefer virtual-hosted even with
		// a custom endpoint) override via use_path_style in TOML.
		usePathStyle := cfg.App.Storage.S3.Endpoint != ""
		if cfg.App.Storage.S3.UsePathStyle != nil {
			usePathStyle = *cfg.App.Storage.S3.UsePathStyle
		}
		s3opts := storage.S3Options{
			Endpoint:     cfg.App.Storage.S3.Endpoint,
			Bucket:       cfg.App.Storage.S3.Bucket,
			Region:       cfg.App.Storage.S3.Region,
			AccessKey:    cfg.App.Storage.S3.AccessKey,
			SecretKey:    cfg.App.Storage.S3.SecretKey,
			UsePathStyle: usePathStyle,
		}
		s3store, err := storage.NewS3(ctx, s3opts)
		if err != nil {
			log.Error("Failed to init S3 storage", "error", err)
			os.Exit(1)
		}
		store = s3store
		log.Info("Storage initialized", "type", "s3",
			"endpoint", cfg.App.Storage.S3.Endpoint,
			"bucket", cfg.App.Storage.S3.Bucket,
			"region", cfg.App.Storage.S3.Region,
		)

	default:
		log.Error("Unsupported storage type",
			"type", cfg.App.Storage.Type,
			"supported", []string{"local", "s3"},
		)
		os.Exit(1)
	}

	// Downloader service. When a service-account refresh token
	// is configured (TWITCH_SERVICE_ACCOUNT_REFRESH_TOKEN), wire
	// the Helix client's RefreshUserToken so the playback-token
	// GQL path can carry Authorization: OAuth <access>. Narrow
	// callback rather than the full client keeps
	// internal/downloader off internal/twitch's import graph.
	dl := downloader.NewService(cfg, repo, store, log)
	if cfg.Env.ServiceAccountOAuthToken != "" {
		dl.SetOAuthRefresher(func(ctx context.Context, refreshToken string) (string, time.Time, error) {
			resp, err := twitchClient.RefreshUserToken(ctx, refreshToken)
			if err != nil {
				return "", time.Time{}, err
			}
			expiresAt := time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
			return resp.AccessToken, expiresAt, nil
		})
	}

	// Resume in-flight downloads from the previous process
	// lifetime. Must run after SetOAuthRefresher (resumed jobs
	// may hit the service-account refresh path) and before the
	// HTTP server accepts requests (a concurrent Start would
	// race resume over the active map + concurrency cap).
	//
	// Sweeps orphaned scratch dirs as a side effect; on a clean
	// boot with no RUNNING jobs this is the only place the
	// sweep runs.
	if err := dl.Resume(ctx); err != nil {
		log.Error("Failed to resume in-flight downloads", "error", err)
		os.Exit(1)
	}

	// SSE bus: one set of topics shared between scheduler (publishes
	// task status), schedule processor (publishes stream.live), and
	// event-log writer (publishes system.events). Routing handlers
	// subscribe per-client.
	bus := eventbus.New()

	// Scheduler: wire the EventSub manager first so the snapshot task
	// has something to call. Skip entirely if cfg.App.Scheduler.Enabled
	// is false — useful for one-off CLI invocations or tests.
	var sched *scheduler.Service
	if cfg.App.Scheduler.Enabled {
		esvc := eventsub.New(repo, twitchClient, cfg.Env.WebhookCallbackURL, cfg.Env.HMACSecret, log)
		sched = scheduler.NewService(repo, log, 15*time.Second, bus)
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
	srv := server.NewServer(cfg, repo, sessionMgr, twitchClient, store, dl, bus, log)
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
