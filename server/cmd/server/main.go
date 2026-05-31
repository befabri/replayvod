package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/database"
	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/eventbus"
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
	"github.com/befabri/replayvod/server/internal/service/eventsub"
	"github.com/befabri/replayvod/server/internal/service/eventsubconfig"
	"github.com/befabri/replayvod/server/internal/service/livepoll"
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

	// Resolve the EventSub HMAC secret before anything reads it (server-mode
	// resolution below validates it; the webhook handler and EventSub service
	// verify/sign with it). The database is the source of truth; an empty slot
	// is seeded once from HMAC_SECRET if set, otherwise generated. Write the
	// resolved value back so every reader of cfg.Env.HMACSecret sees it.
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
	// Server mode selects how we detect live channels and mid-stream title
	// changes: poll uses Helix polling; direct/relay use EventSub push; off
	// stores only the at-start title snapshot.
	// categoryart.Service fetches box_art_url from /helix/games. Used
	// by the Hydrator eagerly (on first category observation) and by
	// the scheduler's category_art_sync task as a backfill path.
	artSvc := categoryart.New(repo, twitchClient, log)
	hydrator := streammeta.NewHydrator(repo, twitchClient, streammeta.Config{
		CategoryArt: artSvc,
	}, log)
	eventSubCallbackURL := cfg.ServerModeCallbackURL()

	// eventsubSvc is constructed once and shared: used by the
	// existing stream.online subscription flow AND by the new
	// channel.update per-recording flow. The HMAC secret + callback
	// URL are the same across both sub types.
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

	// Setup graceful shutdown before starting background goroutines.
	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// SSE bus: one set of topics shared between scheduler (publishes
	// task status), schedule processor (publishes stream.live), the
	// event-log writer (publishes system.events), and the recording-webhook
	// dispatcher (subscribes to terminal-recording wake-ups). Routing handlers
	// subscribe per-client.
	//
	// Wire the bus and webhook dispatcher BEFORE Resume. Resume() spawns
	// recording goroutines immediately; if one finalizes during boot, the
	// terminal path commits a durable webhook row and publishes a wake-up. A
	// dropped wake-up no longer loses the webhook, but wiring first still avoids
	// delaying resumed completions until the next poll interval.
	bus := eventbus.New()
	dl.SetEventBus(bus)

	// Outbound recording webhook: POST a signed payload when a recording
	// reaches a terminal state. Terminal paths persist an outbox row in the same
	// transaction as the video state change; this dispatcher polls due rows with
	// persisted retry/backoff state. The signer mints the signed per-part
	// download URLs embedded in each payload.
	//
	// Construct it now (the API handlers need it for test-send / history /
	// retry), but Start it only AFTER the HTTP listener is bound (below): a
	// payload's signed download_url points at this server's /videos/.../download
	// route, so delivering before the listener is up could hand a receiver a URL
	// that 404s. Durability makes the deferral safe — terminal rows are already
	// persisted, so the first poll after Start drains anything enqueued meanwhile.
	publicAPIBaseURL := cfg.PublicAPIBaseURL()
	webhookSigner := videodownload.NewSigner(cfg.Env.HMACSecret, publicAPIBaseURL, cfg.SignedDownloadURLTTL())
	if webhookSigner.Enabled() && config.OriginIsLoopback(publicAPIBaseURL) {
		// Signed downloads are on but the public origin is loopback (usually the
		// localhost CallbackURL default). An external recording-webhook consumer
		// would receive part-download links it can't reach; surface it loudly so
		// the operator sets PUBLIC_BASE_URL or a real callback origin.
		log.Warn("signed recording-webhook download URLs derive from a loopback origin; set PUBLIC_BASE_URL to a publicly reachable host or external consumers will receive unreachable links",
			"origin", publicAPIBaseURL)
	}
	webhookDispatcher := recordingwebhook.NewDispatcher(repo, webhookSigner, log)
	webhookDispatcher.SetRetentionDownloadURLCapEnabled(cfg.App.Scheduler.RecordingsRetentionIntervalMinutes > 0)

	// Resume in-flight downloads from the previous process lifetime. Must run
	// after SetOAuthRefresher (resumed jobs may hit the service-account refresh
	// path), after the eventbus + webhook dispatcher are wired (above), and
	// before the HTTP server accepts requests (a concurrent Start would race
	// resume over the active map + concurrency cap).
	//
	// Sweeps orphaned scratch dirs as a side effect; on a clean boot with no
	// RUNNING jobs this is the only place the sweep runs.
	if err := dl.Resume(ctx); err != nil {
		log.Error("Failed to resume in-flight downloads", "error", err)
		os.Exit(1)
	}

	eventProcessor := schedulesvc.NewEventProcessor(repo, dl, twitchClient, hydrator, bus, log)

	// Start the local HTTP server before creating or reconciling EventSub
	// subscriptions. Twitch verification challenges must be able to reach the
	// local callback immediately, especially when routed through Connect relay.
	log.Info("Server starting", "address", cfg.GetAddress(), "database", cfg.Env.DatabaseDriver)
	srv := server.NewServer(cfg, repo, sessionMgr, twitchClient, store, dl, hydrator, bus, eventProcessor, webhookDispatcher, log)
	serverReady := make(chan error, 1)
	go srv.Start(serverReady)
	if err := <-serverReady; err != nil {
		log.Error("Failed to start server", "error", err)
		logger.Close()
		os.Exit(1)
	}
	log.Info("Server started", "address", cfg.GetAddress(), "database", cfg.Env.DatabaseDriver)

	// Now that the HTTP listener is bound, start draining the webhook outbox.
	// Any rows enqueued during Resume above are picked up on the first poll.
	webhookDispatcher.Start(signalCtx, bus)

	// shutdown tears down in the same order the normal exit path does, so a fatal
	// boot error after the dispatcher has started can't kill an in-flight
	// delivery mid-POST. os.Exit skips deferreds, so the drain must be explicit:
	// cancel the signal context (the dispatcher's poll/wake loops watch it, which
	// is what lets Wait return), drain in-flight deliveries (bounded by the
	// dispatcher's own drain timeout), then stop the listener and flush logs.
	// webhookDispatcher.Wait is a no-op if Start never ran, and stop is
	// idempotent, so this is safe to call from every branch below including the
	// normal shutdown.
	shutdown := func(code int) {
		stop()
		webhookDispatcher.Wait()
		srv.Stop()
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

	// Optional Connect relay agent. RelayIngestURL remains the public HTTPS
	// URL registered with Twitch, while
	// RELAY_LOCAL_CALLBACK_URL is where this agent replays frames locally.
	// The HMAC is still verified by webhook.Handler — the relay never holds
	// the secret.
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

	// Scheduler: wire the EventSub manager first so the snapshot task
	// has something to call. Skip entirely if cfg.App.Scheduler.Enabled
	// is false — useful for one-off CLI invocations or tests.
	var sched *scheduler.Service
	if cfg.App.Scheduler.Enabled {
		var esvc *eventsub.Service
		if cfg.ServerMode.CreatesTwitchSubscriptions() {
			esvc = eventsubSvc
		}
		sched = scheduler.NewService(repo, log, 15*time.Second, bus)
		// store is always non-nil by here (selected/validated above), so
		// the per-schedule recordings auto-delete sweep is always wired.
		retentionSvc := retention.New(repo, store, log)
		if err := scheduler.RegisterStandardTasks(sched, cfg, repo, esvc, artSvc, retentionSvc, log); err != nil {
			log.Error("Failed to register scheduler tasks", "error", err)
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

	// Wait for shutdown signal
	<-signalCtx.Done()
	log.Info("Shutting down server...")
	if sched != nil {
		sched.Stop()
	}
	// Wait for an in-flight live-poll tick to finish so we don't os.Exit
	// mid-dispatch (after a video/job row is written). signalCtx is already
	// cancelled, so Run returns once the current tick completes; bound the
	// wait so a stuck Helix call can't hang shutdown indefinitely.
	awaitLivePollShutdown(livePollDone, 5*time.Second, log)
	// Drain in-flight webhook deliveries (bounded) and stop the listener via the
	// shared shutdown path, so the normal and fatal exits stay symmetric: the
	// poll loop already stopped on signalCtx, so this lets a POST that was mid-
	// flight finish and record its durable result rather than being killed by
	// os.Exit.
	shutdown(0)
}

// awaitLivePollShutdown blocks until the live poller's goroutine signals it has
// stopped, or the grace period elapses, so an in-flight tick finishes before
// os.Exit (avoiding a mid-dispatch kill) without letting a stuck Helix call hang
// shutdown indefinitely. A nil channel means the poller was never started.
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

// resolveOrDegrade decides how boot reacts to eventsubconfig.Resolve's outcome,
// split out from main so the policy is unit-testable. A clean resolve is used
// as-is. An invalid *app-managed* config degrades to setup-required so the owner
// can re-onboard from the dashboard instead of the process refusing to boot.
// Anything else is fatal: an invalid *env-managed* config is an operator mistake
// that must be fixed at the source, and a non-ErrInvalid error (e.g. the DB read
// failed) means no resolution can be trusted. The caller logs and exits on fatal.
func resolveOrDegrade(resolved config.ServerModeConfig, err error) (final config.ServerModeConfig, fatal bool) {
	if err == nil {
		return resolved, false
	}
	if errors.Is(err, eventsubconfig.ErrInvalid) && !resolved.EnvManaged() {
		return config.ServerModeConfig{Source: config.ServerModeConfigSourceUnset}, false
	}
	return resolved, true
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
