package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/database"
	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/logger"
	"github.com/befabri/replayvod/server/internal/relayclient"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/scheduler"
	"github.com/befabri/replayvod/server/internal/server"
	"github.com/befabri/replayvod/server/internal/service/categoryart"
	"github.com/befabri/replayvod/server/internal/service/eventsub"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/befabri/replayvod/server/migrations"
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

		repo = sqliteadapter.New(sqliteDB)

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
	// streammeta.Hydrator + MetadataWatcher are the shared enrichment
	// surface: the hydrator runs at trigger/stream-online time; the
	// watcher polls for title changes during the recording. Both
	// read + persist the same titles / video_titles M2M rows so the
	// UI's title-history button surfaces the full broadcast history.
	//
	// The title-tracking mode selects how we capture mid-stream
	// changes:
	//   poll    → MetadataWatcher polls Helix
	//   webhook → channel.update EventSub per-recording
	//   off     → only the at-start snapshot
	// channelSubs is the narrow ChannelUpdateSubscriber interface
	// the downloader uses; it's satisfied by eventsubSvc below, but
	// we only pass it through when webhook mode is configured so
	// unused deps stay nil.
	// categoryart.Service fetches box_art_url from /helix/games. Used
	// by the Hydrator eagerly (on first category observation) and by
	// the scheduler's category_art_sync task as a backfill path.
	artSvc := categoryart.New(repo, twitchClient, log)
	hydrator := streammeta.NewHydrator(repo, twitchClient, streammeta.Config{
		CategoryArt: artSvc,
	}, log)
	titleMode := cfg.App.TitleTracking.EffectiveMode()

	// Webhook mode requires a publicly-reachable HTTPS callback on
	// a standard port (443). Twitch rejects anything else with
	// Helix 400 — if we let the server start and silently fall
	// back to poll, the operator wouldn't notice the misconfig
	// until they wondered why title history was empty. Hard fail
	// instead. Covers: empty URL, http://, non-443 ports, malformed
	// URL, missing host.
	if titleMode == config.TitleTrackingModeWebhook && !isUsableWebhookURL(cfg.Env.WebhookCallbackURL) {
		log.Error("title_tracking.mode=webhook requires WEBHOOK_CALLBACK_URL to be a valid HTTPS URL on port 443",
			"callback_host", urlHost(cfg.Env.WebhookCallbackURL))
		os.Exit(1)
	}
	if err := validateRelayURLs(cfg.Env.WebhookCallbackURL, cfg.Env.RelaySubscribeURL); err != nil {
		log.Error("relay URL validation failed",
			"error", err,
			"callback_host", urlHost(cfg.Env.WebhookCallbackURL),
			"subscribe_host", urlHost(cfg.Env.RelaySubscribeURL))
		os.Exit(1)
	}
	// Soft warning for the non-webhook-mode case: WEBHOOK_CALLBACK_URL
	// is set but unusable. Scheduled recording + live-dot subs will
	// fail silently (one info log per reconcile, handled in the
	// service). Surfacing it once at startup gives the operator a
	// clear signal before the feature silently breaks.
	if cfg.Env.WebhookCallbackURL != "" && !isUsableWebhookURL(cfg.Env.WebhookCallbackURL) {
		log.Warn("WEBHOOK_CALLBACK_URL is set but not a valid HTTPS endpoint — webhook-dependent features (scheduled recording, live-dot SSE) will be skipped",
			"callback_host", urlHost(cfg.Env.WebhookCallbackURL))
	}

	// eventsubSvc is constructed once and shared: used by the
	// existing stream.online subscription flow AND by the new
	// channel.update per-recording flow. The HMAC secret + callback
	// URL are the same across both sub types.
	eventsubSvc := eventsub.New(repo, twitchClient, cfg.Env.WebhookCallbackURL, cfg.Env.HMACSecret, log)

	var metaWatcher *streammeta.MetadataWatcher
	if titleMode == config.TitleTrackingModePoll {
		metaWatcher = streammeta.NewMetadataWatcher(hydrator, streammeta.MetadataWatchConfig{
			Interval: time.Duration(cfg.App.TitleTracking.IntervalMinutes) * time.Minute,
		}, log)
	}
	var channelSubs downloader.ChannelUpdateSubscriber
	if titleMode == config.TitleTrackingModeWebhook {
		channelSubs = &channelSubsAdapter{es: eventsubSvc}
	}
	dl := downloader.NewService(cfg, repo, store, hydrator, metaWatcher, channelSubs, log)
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

	// Setup graceful shutdown before starting background goroutines.
	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// SSE bus: one set of topics shared between scheduler (publishes
	// task status), schedule processor (publishes stream.live), and
	// event-log writer (publishes system.events). Routing handlers
	// subscribe per-client.
	bus := eventbus.New()

	// Start the local HTTP server before creating or reconciling EventSub
	// subscriptions. Twitch verification challenges must be able to reach the
	// local callback immediately, especially when routed through Connect relay.
	log.Info("Server starting", "address", cfg.GetAddress(), "database", cfg.Env.DatabaseDriver)
	srv := server.NewServer(cfg, repo, sessionMgr, twitchClient, store, dl, hydrator, bus, log)
	serverReady := make(chan error, 1)
	go srv.Start(serverReady)
	if err := <-serverReady; err != nil {
		log.Error("Failed to start server", "error", err)
		logger.Close()
		os.Exit(1)
	}
	log.Info("Server started", "address", cfg.GetAddress(), "database", cfg.Env.DatabaseDriver)

	// Optional Connect relay agent. WEBHOOK_CALLBACK_URL remains the
	// public HTTPS URL registered with Twitch (the relay ingest URL), while
	// RELAY_LOCAL_CALLBACK_URL is where this agent replays frames locally.
	// The HMAC is still verified by webhook.Handler — the relay never holds
	// the secret.
	if cfg.Env.RelaySubscribeURL != "" {
		localCallbackURL := relayLocalCallbackURL(cfg)
		if sameURL(localCallbackURL, cfg.Env.WebhookCallbackURL) {
			log.Error("RELAY_LOCAL_CALLBACK_URL must not equal WEBHOOK_CALLBACK_URL; local replay would loop back into the public relay",
				"callback_host", urlHost(localCallbackURL))
			srv.Stop()
			logger.Close()
			os.Exit(1)
		}
		rc, err := relayclient.New(relayclient.Config{
			SubscribeURL: cfg.Env.RelaySubscribeURL,
			CallbackURL:  localCallbackURL,
			Logger:       log,
		})
		if err != nil {
			log.Error("Failed to start relay client", "error", err)
			srv.Stop()
			logger.Close()
			os.Exit(1)
		}
		go rc.Run(signalCtx)
		select {
		case <-rc.Ready():
			log.Info("Relay client started",
				"subscribe_host", urlHost(cfg.Env.RelaySubscribeURL),
				"local_callback", localCallbackURL,
			)
		case <-time.After(15 * time.Second):
			log.Error("Relay client did not connect before startup timeout",
				"subscribe_host", urlHost(cfg.Env.RelaySubscribeURL))
			srv.Stop()
			logger.Close()
			os.Exit(1)
		case <-signalCtx.Done():
			srv.Stop()
			logger.Close()
			os.Exit(0)
		}
	}

	// Boot-time reconcile of EventSub subscriptions. Two separate
	// sweeps, different semantics:
	//
	//   1. channel.update subs (title tracking, webhook mode only) —
	//      delete orphans left by a prior process that crashed before
	//      unsubscribe. Matches against the active-recording set.
	//
	//   2. stream.online/stream.offline subs (live-dot feed) — ensure
	//      every local channel has both, delete orphans. Makes the
	//      SSE delta feed authoritative so the frontend can keep
	//      staleTime: Infinity without risk of drift. Runs regardless
	//      of title-tracking mode since the live-dot is a separate
	//      feature.
	//
	// Both are best-effort; failures log but don't fail startup.
	if titleMode == config.TitleTrackingModeWebhook {
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

	if cfg.Env.WebhookCallbackURL != "" {
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

	// Scheduler: wire the EventSub manager first so the snapshot task
	// has something to call. Skip entirely if cfg.App.Scheduler.Enabled
	// is false — useful for one-off CLI invocations or tests.
	var sched *scheduler.Service
	if cfg.App.Scheduler.Enabled {
		esvc := eventsub.New(repo, twitchClient, cfg.Env.WebhookCallbackURL, cfg.Env.HMACSecret, log)
		sched = scheduler.NewService(repo, log, 15*time.Second, bus)
		if err := scheduler.RegisterStandardTasks(sched, cfg, repo, esvc, artSvc, log); err != nil {
			log.Error("Failed to register scheduler tasks", "error", err)
			srv.Stop()
			logger.Close()
			os.Exit(1)
		}
		if err := sched.Start(ctx); err != nil {
			log.Error("Failed to start scheduler", "error", err)
			srv.Stop()
			logger.Close()
			os.Exit(1)
		}
		log.Info("Scheduler started")
	} else {
		log.Info("Scheduler disabled by config")
	}

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

func relayLocalCallbackURL(cfg *config.Config) string {
	if cfg.Env.RelayLocalCallbackURL != "" {
		return cfg.Env.RelayLocalCallbackURL
	}
	return fmt.Sprintf("http://127.0.0.1:%d/api/v1/webhook/callback", cfg.Env.Port)
}

// validateRelayURLs enforces the startup-time invariants that tie the optional
// Connect relay to the local server. Twitch posts to WEBHOOK_CALLBACK_URL, while
// the local agent dials RELAY_SUBSCRIBE_URL; both must address the same relay
// host and /u/<token> Durable Object or verification challenges will miss the
// subscriber.
func validateRelayURLs(webhookCallbackURL, subscribeURL string) error {
	if subscribeURL == "" {
		return nil
	}
	if !isUsableWebhookURL(webhookCallbackURL) {
		return fmt.Errorf("RELAY_SUBSCRIBE_URL requires WEBHOOK_CALLBACK_URL to be the public HTTPS relay ingest URL")
	}
	ingest, err := url.Parse(webhookCallbackURL)
	if err != nil {
		return fmt.Errorf("parse WEBHOOK_CALLBACK_URL: %w", err)
	}
	subscribe, err := url.Parse(subscribeURL)
	if err != nil {
		return fmt.Errorf("parse RELAY_SUBSCRIBE_URL: %w", err)
	}
	if subscribe.Scheme != "wss" {
		return fmt.Errorf("RELAY_SUBSCRIBE_URL must use wss://")
	}
	if !strings.EqualFold(ingest.Host, subscribe.Host) {
		return fmt.Errorf("WEBHOOK_CALLBACK_URL and RELAY_SUBSCRIBE_URL must use the same relay host")
	}
	ingestToken, ok := relayIngestToken(ingest.Path)
	if !ok {
		return fmt.Errorf("WEBHOOK_CALLBACK_URL must use /u/<token>")
	}
	subscribeToken, ok := relaySubscribeToken(subscribe.Path)
	if !ok {
		return fmt.Errorf("RELAY_SUBSCRIBE_URL must use /u/<token>/subscribe")
	}
	if ingestToken != subscribeToken {
		return fmt.Errorf("WEBHOOK_CALLBACK_URL and RELAY_SUBSCRIBE_URL must use the same relay token")
	}
	return nil
}

func relayIngestToken(path string) (string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 || parts[0] != "u" || !isRelayToken(parts[1]) {
		return "", false
	}
	return parts[1], true
}

func relaySubscribeToken(path string) (string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 || parts[0] != "u" || parts[2] != "subscribe" || !isRelayToken(parts[1]) {
		return "", false
	}
	return parts[1], true
}

func isRelayToken(value string) bool {
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

func sameURL(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	if errA != nil || errB != nil {
		return false
	}
	return strings.EqualFold(ua.Scheme, ub.Scheme) &&
		strings.EqualFold(ua.Host, ub.Host) &&
		ua.Path == ub.Path
}

// isUsableWebhookURL mirrors eventsub.isCallbackURLUsable so the
// startup validation matches what the service will actually accept.
// Kept here in main so we can fail loudly before the service is
// ever called.
func isUsableWebhookURL(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return false
	}
	if u.Port() != "" && u.Port() != "443" {
		return false
	}
	return true
}

// channelSubsAdapter bridges eventsub.Service's return-rich
// SubscribeChannelUpdate (returns *repository.Subscription) to
// the downloader's error-only ChannelUpdateSubscriber. Keeps
// internal/downloader off the repository import graph.
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
