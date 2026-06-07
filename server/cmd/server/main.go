package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/database"
	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/igdb"
	"github.com/befabri/replayvod/server/internal/logger"
	"github.com/befabri/replayvod/server/internal/recordingwebhook"
	"github.com/befabri/replayvod/server/internal/relayclient"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/scheduler"
	"github.com/befabri/replayvod/server/internal/secrets"
	"github.com/befabri/replayvod/server/internal/server"
	"github.com/befabri/replayvod/server/internal/service/categoryart"
	"github.com/befabri/replayvod/server/internal/service/categorymeta"
	"github.com/befabri/replayvod/server/internal/service/eventsub"
	"github.com/befabri/replayvod/server/internal/service/eventsubconfig"
	"github.com/befabri/replayvod/server/internal/service/livepoll"
	"github.com/befabri/replayvod/server/internal/service/playbackcache"
	"github.com/befabri/replayvod/server/internal/service/retention"
	schedulesvc "github.com/befabri/replayvod/server/internal/service/schedule"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/befabri/replayvod/server/internal/videodownload"
	"github.com/befabri/replayvod/server/migrations"
)

func main() {
	configPath := "server/config.toml"
	if _, err := os.Stat("config.toml"); err == nil {
		configPath = "config.toml"
	}
	cfg := config.LoadConfig(configPath)
	if cfg == nil {
		slog.Error("Failed to load configuration")
		os.Exit(1)
	}

	level := logger.ParseLogLevel(cfg.App.Logging.LogLevel)
	log := logger.SetupLoggerWithLevel(
		os.Stderr, "replayvod",
		cfg.App.Logging.LogToFile, cfg.App.Logging.LogDir,
		level,
	)

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

		repo = sqliteadapter.New(sqliteDB)

	default:
		log.Error("Unknown database driver", "driver", cfg.Env.DatabaseDriver)
		os.Exit(1)
	}

	hmacSecret, hmacSource, hmacErr := secrets.ResolveHMAC(ctx, repo, cfg.Env.HMACSecret)
	if hmacErr != nil {
		log.Error("Failed to resolve EventSub HMAC secret", "error", hmacErr)
		os.Exit(1)
	}
	cfg.Env.HMACSecret = hmacSecret
	switch hmacSource {
	case secrets.FromEnv:
		log.Info("Seeded EventSub HMAC secret from HMAC_SECRET into the database")
	case secrets.Generated:
		log.Info("Generated EventSub HMAC secret and stored it in the database")
	}

	resolvedMode, resolveErr := eventsubconfig.Resolve(ctx, repo, cfg)
	finalMode, fatalResolve := resolveOrDegrade(resolvedMode, resolveErr)
	cfg.ServerMode = finalMode
	if resolveErr != nil {
		if fatalResolve {
			log.Error("Server mode config invalid",
				"error", resolveErr,
				"mode", resolvedMode.Mode,
				"source", resolvedMode.Source,
				"callback_host", config.URLHost(resolvedMode.CallbackURL()),
				"subscribe_host", config.URLHost(resolvedMode.RelaySubscribeURL))
			os.Exit(1)
		}
		log.Warn("Saved server mode config invalid; continuing with setup required",
			"error", resolveErr,
			"mode", resolvedMode.Mode,
			"source", resolvedMode.Source,
			"callback_host", config.URLHost(resolvedMode.CallbackURL()),
			"subscribe_host", config.URLHost(resolvedMode.RelaySubscribeURL))
	}
	if cfg.ServerMode.SetupRequired() {
		log.Warn("Server mode is not configured; owner onboarding is required before live automation can run")
	} else {
		log.Info("Server mode configured",
			"source", cfg.ServerMode.Source,
			"mode", cfg.ServerMode.Mode,
			"callback_host", config.URLHost(cfg.ServerMode.CallbackURL()),
			"subscribe_host", config.URLHost(cfg.ServerMode.RelaySubscribeURL))
	}

	twitchClient := twitch.NewClient(cfg.Env.TwitchClientID, cfg.Env.TwitchSecret, log)
	twitchClient.SetFetchLogRecorder(twitch.RecorderFunc(func(ctx context.Context, e twitch.FetchLogEntry) {
		logCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		var errStr *string
		if e.Error != "" {
			errStr = &e.Error
		}
		if err := repo.CreateFetchLog(logCtx, &repository.FetchLogInput{
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

	secureCookie := cfg.Env.Host != "localhost" && cfg.Env.Host != "0.0.0.0"
	sessionMgr, err := session.NewManager(repo, cfg.Env.SessionSecret, secureCookie, log)
	if err != nil {
		log.Error("Failed to create session manager", "error", err)
		os.Exit(1)
	}

	if err := session.SeedWhitelist(ctx, repo, cfg.Env.OwnerTwitchID, cfg.Env.WhitelistedUserIDs, log); err != nil {
		log.Error("Failed to seed whitelist", "error", err)
		os.Exit(1)
	}

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

	artSvc := categoryart.New(repo, twitchClient, log)
	igdbClient := igdb.NewClient(cfg.Env.TwitchClientID, twitchClient, log)
	categoryMetaSvc := categorymeta.New(repo, igdbClient, log)
	hydrator := streammeta.NewHydrator(repo, twitchClient, streammeta.Config{
		CategoryArt: artSvc,
	}, log)
	eventSubCallbackURL := cfg.ServerModeCallbackURL()

	eventsubSvc := eventsub.New(repo, twitchClient, eventSubCallbackURL, cfg.Env.HMACSecret, log)
	if err := eventsubconfig.CleanupNonSubscriptionRuntime(ctx, cfg.ServerMode, eventsubSvc, log); err != nil {
		log.Error("Failed to clean up stale EventSub subscriptions; continuing startup", "error", err)
	}

	var metaWatcher *streammeta.MetadataWatcher
	if cfg.ServerMode.TracksTitlesViaPoll() {
		metaWatcher = streammeta.NewMetadataWatcher(hydrator, streammeta.MetadataWatchConfig{
			Interval: time.Duration(cfg.App.Server.PollIntervalMinutes) * time.Minute,
		}, log)
	}
	var channelSubs downloader.ChannelUpdateSubscriber
	if cfg.ServerMode.TracksTitlesViaWebhook() {
		channelSubs = &channelSubsAdapter{es: eventsubSvc}
	}
	dl := downloader.NewService(cfg, repo, store, hydrator, metaWatcher, channelSubs, log)
	playbackCache := playbackcache.New(repo, store, filepath.Join(cfg.Env.ScratchDir, "playback-cache"), "", log)
	hydrator.SetMediaOffsetResolver(dl)
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

	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	bus := eventbus.New()
	dl.SetEventBus(bus)

	publicAPIBaseURL := cfg.PublicAPIBaseURL()
	webhookSigner := videodownload.NewSigner(cfg.Env.HMACSecret, publicAPIBaseURL, cfg.SignedDownloadURLTTL())
	if webhookSigner.Enabled() && config.OriginIsLoopback(publicAPIBaseURL) {
		log.Warn("signed recording-webhook download URLs derive from a loopback origin; set PUBLIC_BASE_URL to a publicly reachable host or external consumers will receive unreachable links",
			"origin", publicAPIBaseURL)
	}
	webhookDispatcher := recordingwebhook.NewDispatcher(repo, webhookSigner, log)
	webhookDispatcher.SetRetentionDownloadURLCapEnabled(cfg.App.Scheduler.RecordingsRetentionIntervalMinutes > 0)

	if err := dl.Resume(ctx); err != nil {
		log.Error("Failed to resume in-flight downloads", "error", err)
		os.Exit(1)
	}

	eventProcessor := schedulesvc.NewEventProcessor(repo, dl, twitchClient, hydrator, bus, log)

	log.Info("Server starting", "address", cfg.GetAddress(), "database", cfg.Env.DatabaseDriver)
	srv := server.NewServer(cfg, repo, sessionMgr, twitchClient, store, dl, hydrator, bus, eventProcessor, webhookDispatcher, playbackCache, log)
	serverReady := make(chan error, 1)
	go srv.Start(serverReady)
	if err := <-serverReady; err != nil {
		log.Error("Failed to start server", "error", err)
		logger.Close()
		os.Exit(1)
	}
	log.Info("Server started", "address", cfg.GetAddress(), "database", cfg.Env.DatabaseDriver)

	webhookDispatcher.Start(signalCtx, bus)

	shutdown := func(code int) {
		stop()
		webhookDispatcher.Wait()
		srv.Stop()
		playbackCache.Close()
		logger.Close()
		os.Exit(code)
	}

	var livePollDone chan struct{}
	if cfg.ServerMode.PollsHelix() {
		lp := livepoll.New(repo, twitchClient, eventProcessor, time.Duration(cfg.App.Server.PollIntervalMinutes)*time.Minute, log)
		livePollDone = make(chan struct{})
		go func() {
			defer close(livePollDone)
			lp.Run(signalCtx)
		}()
	}

	if cfg.ServerMode.UsesRelayAgent() {
		localCallbackURL := cfg.ServerMode.RelayLocalCallbackURLOrDefault(cfg.Env.Port)
		if config.SameURL(localCallbackURL, eventSubCallbackURL) {
			log.Error("RELAY_LOCAL_CALLBACK_URL must not equal the EventSub relay ingest URL; local replay would loop back into the public relay",
				"callback_host", config.URLHost(localCallbackURL))
			shutdown(1)
		}
		rc, err := relayclient.New(relayclient.Config{
			SubscribeURL: cfg.ServerMode.RelaySubscribeURL,
			CallbackURL:  localCallbackURL,
			Logger:       log,
		})
		if err != nil {
			log.Error("Failed to start relay client", "error", err)
			shutdown(1)
		}
		go rc.Run(signalCtx)
		select {
		case <-rc.Ready():
			log.Info("Relay client started",
				"subscribe_host", config.URLHost(cfg.ServerMode.RelaySubscribeURL),
				"local_callback", localCallbackURL,
			)
		case <-time.After(15 * time.Second):
			log.Error("Relay client did not connect before startup timeout",
				"subscribe_host", config.URLHost(cfg.ServerMode.RelaySubscribeURL))
			shutdown(1)
		case <-signalCtx.Done():
			shutdown(0)
		}
	}

	if cfg.ServerMode.TracksTitlesViaWebhook() {
		activeJobs, err := repo.ListRunningJobs(ctx)
		if err != nil {
			log.Warn("channel.update reconcile: list running jobs failed", "error", err)
		} else {
			active := make(map[string]bool, len(activeJobs))
			for _, j := range activeJobs {
				active[j.BroadcasterID] = true
			}
			if err := eventsubSvc.ReconcileChannelUpdateSubs(ctx, active); err != nil {
				log.Warn("channel.update reconcile failed", "error", err)
			}
		}
	}

	if cfg.ServerMode.CreatesTwitchSubscriptions() {
		channels, err := repo.ListChannels(ctx)
		if err != nil {
			log.Warn("followed-subs reconcile: list channels failed", "error", err)
		} else {
			followed := make(map[string]bool, len(channels))
			for _, ch := range channels {
				followed[ch.BroadcasterID] = true
			}
			if err := eventsubSvc.ReconcileChannelSubs(ctx, followed); err != nil {
				log.Warn("followed-subs reconcile failed", "error", err)
			}
		}
	}

	var sched *scheduler.Service
	if cfg.App.Scheduler.Enabled {
		var esvc *eventsub.Service
		if cfg.ServerMode.CreatesTwitchSubscriptions() {
			esvc = eventsubSvc
		}
		sched = scheduler.NewService(repo, log, 15*time.Second, bus)
		retentionSvc := retention.New(repo, store, log)
		if err := scheduler.RegisterStandardTasks(sched, cfg, repo, scheduler.StandardTaskDeps{
			EventSub:         esvc,
			CategoryArt:      artSvc,
			CategoryMetadata: categoryMetaSvc,
			Retention:        retentionSvc,
		}, log); err != nil {
			log.Error("Failed to register scheduler tasks", "error", err)
			shutdown(1)
		}
		if err := sched.Register(scheduler.Task{
			Name:            "playback_cache_reconcile",
			Description:     "Prune the playback-artifact cache to its size cap",
			IntervalSeconds: 5 * 60,
			Run:             playbackCache.Reconcile,
		}); err != nil {
			log.Error("Failed to register playback cache reconcile task", "error", err)
			shutdown(1)
		}
		if err := sched.Start(ctx); err != nil {
			log.Error("Failed to start scheduler", "error", err)
			shutdown(1)
		}
		log.Info("Scheduler started")
	} else {
		log.Info("Scheduler disabled by config")
	}

	<-signalCtx.Done()
	log.Info("Shutting down server...")
	if sched != nil {
		sched.Stop()
	}
	awaitLivePollShutdown(livePollDone, 5*time.Second, log)
	shutdown(0)
}

func awaitLivePollShutdown(done <-chan struct{}, grace time.Duration, log *slog.Logger) {
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(grace):
		log.Warn("live poller did not stop within shutdown grace period")
	}
}

func resolveOrDegrade(resolved config.ServerModeConfig, err error) (final config.ServerModeConfig, fatal bool) {
	if err == nil {
		return resolved, false
	}
	if errors.Is(err, eventsubconfig.ErrInvalid) && !resolved.EnvManaged() {
		return config.ServerModeConfig{Source: config.ServerModeConfigSourceUnset}, false
	}
	return resolved, true
}

type channelSubsAdapter struct {
	es *eventsub.Service
}

func (a *channelSubsAdapter) SubscribeChannelUpdate(ctx context.Context, broadcasterID string) error {
	_, err := a.es.SubscribeChannelUpdate(ctx, broadcasterID)
	return err
}

func (a *channelSubsAdapter) UnsubscribeChannelUpdate(ctx context.Context, broadcasterID, reason string) error {
	return a.es.UnsubscribeChannelUpdate(ctx, broadcasterID, reason)
}
