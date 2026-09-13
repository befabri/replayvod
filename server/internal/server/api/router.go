package api

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/invite"
	"github.com/befabri/replayvod/server/internal/playbackauth"
	"github.com/befabri/replayvod/server/internal/recordinglock"
	"github.com/befabri/replayvod/server/internal/recordingwebhook"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/auth"
	"github.com/befabri/replayvod/server/internal/server/api/category"
	"github.com/befabri/replayvod/server/internal/server/api/channel"
	"github.com/befabri/replayvod/server/internal/server/api/eventsub"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	playbackauthapi "github.com/befabri/replayvod/server/internal/server/api/playbackauth"
	recordingwebhookapi "github.com/befabri/replayvod/server/internal/server/api/recordingwebhook"
	"github.com/befabri/replayvod/server/internal/server/api/schedule"
	"github.com/befabri/replayvod/server/internal/server/api/settings"
	"github.com/befabri/replayvod/server/internal/server/api/sse"
	"github.com/befabri/replayvod/server/internal/server/api/storageapi"
	"github.com/befabri/replayvod/server/internal/server/api/stream"
	"github.com/befabri/replayvod/server/internal/server/api/subscriptions"
	"github.com/befabri/replayvod/server/internal/server/api/system"
	"github.com/befabri/replayvod/server/internal/server/api/tag"
	"github.com/befabri/replayvod/server/internal/server/api/task"
	"github.com/befabri/replayvod/server/internal/server/api/video"
	"github.com/befabri/replayvod/server/internal/server/api/webhook"
	eventsubsvc "github.com/befabri/replayvod/server/internal/service/eventsub"
	"github.com/befabri/replayvod/server/internal/service/eventsubconfig"
	"github.com/befabri/replayvod/server/internal/service/followsync"
	"github.com/befabri/replayvod/server/internal/service/playbackcache"
	"github.com/befabri/replayvod/server/internal/service/retention"
	schedulesvc "github.com/befabri/replayvod/server/internal/service/schedule"
	"github.com/befabri/replayvod/server/internal/service/storagehealth"
	"github.com/befabri/replayvod/server/internal/service/storagescan"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/befabri/replayvod/server/internal/validate"
	"github.com/befabri/replayvod/server/internal/videodownload"
	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

const bundledDashboardDir = "/app/dashboard"

// RecordingServices is shared by HTTP and scheduler workers. Keeping construction
// at the composition root makes deletion availability and scan progress agree.
type RecordingServices struct {
	Retention    *retention.Service
	StorageScan  *storagescan.Service
	PlaybackAuth *playbackauth.Service
	// StorageHealth vouches for the storage before anything records into it,
	// scans it or tombstones from it. Unsupported backends have a monitor that
	// reports unavailable, keeping the dashboard accessible and storage paused.
	StorageHealth  *storagehealth.Monitor
	RecordingLocks *recordinglock.Locks
}

func NewRecordingServices(cfg *config.Config, repo repository.Repository, store storage.Storage, bus *eventbus.Buses, log *slog.Logger) *RecordingServices {
	health := storagehealth.New(repo, store, bus, log, storageBackend(cfg), storageLocation(cfg))
	locks := &recordinglock.Locks{}
	return &RecordingServices{
		Retention:      retention.New(repo, store, health, log, retention.WithManualDeletionWorkerAvailable(cfg.App.Scheduler.Enabled), retention.WithEventBus(bus), retention.WithRecordingLocks(locks)),
		StorageScan:    storagescan.New(repo, store, health, log, storagescan.WithEventBus(bus), storagescan.WithRecordingLocks(locks)),
		PlaybackAuth:   playbackauth.New(repo, cfg.Env.SessionSecret, playbackauth.NewTwitchValidator()),
		StorageHealth:  health,
		RecordingLocks: locks,
	}
}

func storageBackend(cfg *config.Config) string {
	if cfg.App.Storage.Type == "s3" {
		return "s3"
	}
	return "local"
}

func storageLocation(cfg *config.Config) string {
	if cfg.App.Storage.Type == "s3" {
		return cfg.App.Storage.S3.Bucket
	}
	return cfg.App.Storage.LocalPath
}

func SetupRouter(cfg *config.Config, repo repository.Repository, sessionMgr *session.Manager, twitchClient *twitch.Client, store storage.Storage, dl *downloader.Service, hydrator *streammeta.Hydrator, bus *eventbus.Buses, eventProcessor *schedulesvc.EventProcessor, webhookDispatcher *recordingwebhook.Dispatcher, playbackCache *playbackcache.Service, log *slog.Logger, services ...*RecordingServices) (*chi.Mux, func() error) {
	r := chi.NewRouter()
	trustedBrowserOrigins := cfg.TrustedBrowserOrigins()

	// Pprof endpoints, dev-only. Production config.toml leaves
	// Development=false so this never listens on a hardened deploy.
	// Mounted directly (not under /api/) to match the default
	// net/http/pprof paths that `go tool pprof` expects.
	if cfg.App.Development {
		r.Mount("/debug", chimiddleware.Profiler())
	}

	// Shared domain services — used across multiple transports. Construct
	// once so the OAuth Chi handler + tRPC handler share an auth Service,
	// and the schedule webhook processor + tRPC handler share a schedule
	// Service.
	authSvc := auth.New(repo, sessionMgr, twitchClient, auth.Config{
		WhitelistEnabled: cfg.Env.WhitelistEnabled,
		OwnerTwitchID:    cfg.Env.OwnerTwitchID,
	}, log)
	var scheduleOpts []schedulesvc.Option
	if cfg.ServerMode.RunsLiveAutomation() && twitchClient != nil && eventProcessor != nil {
		scheduleOpts = append(scheduleOpts, schedulesvc.WithImmediateLiveTrigger(
			schedulesvc.NewImmediateTrigger(twitchClient, eventProcessor, log),
		))
	}
	scheduleSvc := schedulesvc.New(repo, log, scheduleOpts...)

	// Chi routes (non-tRPC: OAuth, webhooks, video streaming, thumbnails).
	// Video/thumbnail routes reuse the session middleware — auth required
	// for both, and we want the same context population the tRPC side gets.
	followSync := followsync.New(repo, twitchClient, log)
	authHandler := auth.NewHandler(cfg, twitchClient, sessionMgr, authSvc, followSync, log)
	var recordings *RecordingServices
	if len(services) > 0 {
		recordings = services[0]
	}
	if recordings == nil {
		recordings = NewRecordingServices(cfg, repo, store, bus, log)
	}
	storageGate := recordings.StorageHealth
	// The video stream handler also serves signed, unauthenticated per-part
	// download URLs (handed to recording-webhook consumers). The verifier shares
	// the server HMAC secret; the route is registered outside the session
	// middleware below since the signature, not a cookie, authorizes it.
	streamOpts := []video.StreamHandlerOption{
		// Lazily build the single-file playback artifact the first time a part is
		// streamed (i.e. someone actually watches), instead of eagerly on every
		// recording's completion.
		video.WithPlaybackBuilder(playbackCache),
		video.WithMissingMarker(recordings.StorageScan),
		video.WithStorageGate(recordings.StorageHealth),
		video.WithRecordingLocks(recordings.RecordingLocks),
	}
	videoStream := video.NewStreamHandler(
		repo,
		store,
		videodownload.NewVerifier(cfg.Env.HMACSecret),
		log,
		streamOpts...,
	)
	// The webhook handler needs the raw body for HMAC verification, so it
	// must live on the Chi side (no tRPC JSON middleware) and outside the
	// csrfProtection group (Twitch can't provide a CSRF cookie). Only wire the
	// notification processor when this process is configured to receive
	// EventSub; stale Twitch deliveries after EventSub is turned off should be
	// audited, not dispatched into the recording pipeline.
	var webhookProcessor webhook.EventProcessor
	if cfg.ServerMode.ProcessesWebhookNotifications() {
		webhookProcessor = eventProcessor
	}
	webhookHandler := webhook.NewHandler(repo, cfg.Env.HMACSecret, webhookProcessor, log)
	tokenProvider := middleware.NewSessionTokenProvider(sessionMgr, twitchClient, log)
	sessionMw := middleware.Auth(sessionMgr, repo, tokenProvider, log)
	r.Route("/api/v1", func(r chi.Router) {
		if cfg.App.Health.Enabled {
			r.Get("/health", healthHandler(repo, storageGate, log))
		}
		authHandler.SetupRoutes(r)
		videoStream.SetupRoutes(r, sessionMw)
		// Signed per-part download route: no session middleware — the URL's
		// HMAC signature and expiry are the authorization.
		videoStream.SetupSignedRoutes(r)
		webhookHandler.SetupRoutes(r)
	})

	// tRPC router with CSRF/origin protection.
	trpcRouter := setupTRPCRouter(cfg, repo, sessionMgr, tokenProvider, twitchClient, dl, hydrator, store, bus, authSvc, scheduleSvc, webhookDispatcher, recordings, log)
	csrfProtection := http.NewCrossOriginProtection()
	for _, origin := range trustedBrowserOrigins {
		if err := csrfProtection.AddTrustedOrigin(origin); err != nil {
			log.Warn("invalid trusted origin for CSRF protection", "origin", origin, "error", err)
		}
	}
	trpcHandler := trpc.NewHandler(trpcRouter, "/trpc",
		trpc.WithPublicOrigins(trustedBrowserOrigins...),
	)
	wsHandler := subscriptions.NewHandler(trpcRouter, trustedBrowserOrigins)
	r.With(sessionMw).Get("/trpc/ws", wsHandler.ServeHTTP)
	r.Group(func(r chi.Router) {
		r.Use(csrfProtection.Handler)
		// Per method rather than a catch-all so routedMethods sees what tRPC
		// serves and chi answers anything else with 405.
		for _, method := range trpc.Methods() {
			r.Method(method, "/trpc/*", trpcHandler)
		}
	})

	// SPA fallback. Docker images place the built dashboard at
	// bundledDashboardDir. Manual/source packages can set DASHBOARD_DIR; for an
	// explicit path, keep setupDashboardRoutes' warning when files are missing.
	if cfg.Env.DashboardDir != "" {
		setupDashboardRoutes(r, cfg.Env.DashboardDir, log)
	} else if dashboardBuildExists(bundledDashboardDir) {
		setupDashboardRoutes(r, bundledDashboardDir, log)
	}

	root := chi.NewRouter()
	root.Use(chimiddleware.RequestID)
	root.Use(middleware.CaptureTransportPeer)
	root.Use(chimiddleware.RealIP)
	root.Use(middleware.Logger(log))
	root.Use(middleware.Recoverer(log))
	// The method list is read from r, so CORS must be mounted here, after
	// every route is registered.
	root.Use(middleware.CORS(trustedBrowserOrigins, routedMethods(r), trpc.RequestHeaders()))
	root.Mount("/", r)
	return root, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		followSync.Stop()
		_ = wsHandler.Close()
		return errors.Join(followSync.Wait(ctx), trpcRouter.Close())
	}
}

// routedMethods returns the methods the routes register explicitly, sorted.
// Catch-all handlers are skipped because chi expands them to every method.
func routedMethods(routes chi.Routes) []string {
	seen := map[string]bool{}
	var walk func(chi.Routes)
	walk = func(routes chi.Routes) {
		for _, route := range routes.Routes() {
			if route.SubRoutes != nil {
				walk(route.SubRoutes)
				continue
			}
			if _, catchAll := route.Handlers["*"]; catchAll {
				continue
			}
			for method := range route.Handlers {
				seen[method] = true
			}
		}
	}
	walk(routes)
	return slices.Sorted(maps.Keys(seen))
}

func setupTRPCRouter(cfg *config.Config, repo repository.Repository, sessionMgr *session.Manager, tokenProvider *middleware.SessionTokenProvider, twitchClient *twitch.Client, dl *downloader.Service, hydrator *streammeta.Hydrator, store storage.Storage, bus *eventbus.Buses, authSvc *auth.Service, scheduleSvc *schedulesvc.Service, webhookDispatcher *recordingwebhook.Dispatcher, recordings *RecordingServices, log *slog.Logger) *trpcgo.Router {
	opts := []trpcgo.Option{
		trpcgo.WithContextCreator(middleware.WithContextCreator),
		trpcgo.WithValidator(validate.V.Struct),
		trpcgo.WithBatching(true),
		// Default is 10; the dashboard routinely composes 10-15
		// parallel queries per view (videos grid + session +
		// settings + SSE bootstraps). 50 gives 3-5× headroom over
		// any current page without removing the abuse guardrail.
		trpcgo.WithMaxBatchSize(50),
		trpcgo.WithMethodOverride(true),
		trpcgo.WithDev(cfg.App.Development),
		trpcgo.WithErrorFormatter(func(input trpcgo.ErrorFormatterInput) any {
			return map[string]any{
				"error": map[string]any{
					"message": input.Error.Message,
					"code":    input.Shape.Error.Code,
					"data":    input.Shape.Error.Data,
				},
			}
		}),
		trpcgo.WithOnError(func(ctx context.Context, err *trpcgo.Error, path string) {
			if err.Code == trpcgo.CodeUnauthorized || err.Code == trpcgo.CodeBadRequest || err.Code == trpcgo.CodeClientClosed {
				return
			}
			log.Error("tRPC error", "path", path, "code", trpcgo.NameFromCode(err.Code), "message", err.Message)
		}),
	}

	if cfg.App.Development {
		opts = append(opts,
			trpcgo.WithTypeOutput("../dashboard/src/api/generated/trpc.ts"),
			trpcgo.WithZodOutput("../dashboard/src/api/generated/zod.ts"),
			trpcgo.WithEnumsOutput("../dashboard/src/api/generated/enums.ts"),
			trpcgo.WithWatchPackages("./..."),
		)
	}

	tr := trpcgo.NewRouter(opts...)

	// Procedure builders: authed is the base, viewer/admin/owner layer role
	// middleware on top. Each domain's RegisterRoutes picks the ones it
	// needs, so we pass only what's relevant per call.
	authMw := middleware.TRPCAuth(sessionMgr, repo, tokenProvider, log)
	adminMw := middleware.TRPCRequireRole(middleware.RoleAdmin)
	ownerMw := middleware.TRPCRequireRole(middleware.RoleOwner)

	authed := trpcgo.Procedure().Use(authMw)
	viewer := authed
	admin := authed.Use(adminMw)
	owner := authed.Use(ownerMw)

	// EventSub domain service — the tRPC handler uses the same domain service
	// type as the scheduler, but main.go builds a separate boot-time instance
	// for background jobs.
	eventsubMgr := eventsubsvc.New(repo, twitchClient, cfg.ServerModeCallbackURL(), cfg.Env.HMACSecret, log)
	eventsubConfigSvc := eventsubconfig.New(repo, cfg, log)

	// Dispatch to each domain. Keeps this function stable when a domain
	// adds a new procedure — the change lives in that domain's routes.go.
	auth.RegisterTRPC(tr, authSvc, sessionMgr, log, authed)
	category.RegisterRoutes(tr, repo, twitchClient, log, viewer)
	channel.RegisterRoutes(tr, repo, twitchClient, log, viewer, owner)
	eventsub.RegisterRoutes(tr, eventsubMgr, eventsubConfigSvc, log, owner)
	recordingwebhookapi.RegisterRoutes(tr, repo, webhookDispatcher, log, owner)
	schedule.RegisterRoutes(tr, scheduleSvc, log, viewer, admin)
	settings.RegisterRoutes(tr, repo, log, viewer)
	playbackauthapi.RegisterRoutes(tr, recordings.PlaybackAuth, cfg.PublicAPIBaseURL(), log, owner)
	sse.RegisterRoutes(tr, bus, log, viewer, owner)
	var scanTasks storageapi.TaskRunner
	if cfg.App.Scheduler.Enabled && cfg.App.Scheduler.StorageScanIntervalMinutes > 0 {
		scanTasks = task.New(repo, log)
	}
	storageapi.RegisterRoutes(tr, recordings.StorageHealth, scanTasks, log, viewer, owner)
	stream.RegisterRoutes(tr, repo, twitchClient, log, viewer)
	system.RegisterRoutes(tr, repo, invite.New(repo, cfg.Env.FrontendURL, log), log, admin, owner)
	tag.RegisterRoutes(tr, repo, log, viewer)
	task.RegisterRoutes(tr, repo, log, owner)
	video.RegisterRoutes(tr, repo, dl, twitchClient, hydrator, recordings.Retention, recordings.StorageScan, store, log, viewer, admin)

	return tr
}

func dashboardBuildExists(dashboardDir string) bool {
	info, err := os.Stat(filepath.Join(dashboardDir, "index.html"))
	return err == nil && !info.IsDir()
}

func setupDashboardRoutes(r *chi.Mux, dashboardDir string, log *slog.Logger) {
	if _, err := os.Stat(dashboardDir); os.IsNotExist(err) {
		log.Warn("Dashboard directory not found, skipping dashboard routes", "path", dashboardDir)
		return
	}

	indexPath := filepath.Join(dashboardDir, "index.html")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		log.Warn("Dashboard index.html not found, skipping dashboard routes", "path", indexPath)
		return
	}

	log.Info("Serving dashboard", "dir", dashboardDir)
	fileServer := http.FileServer(http.Dir(dashboardDir))

	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/trpc/") {
			http.NotFound(w, r)
			return
		}

		filePath := filepath.Join(dashboardDir, path)
		if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
			if isStaticAsset(path) {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html for all other routes
		http.ServeFile(w, r, indexPath)
	})
}

func isStaticAsset(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".js", ".css", ".woff", ".woff2", ".ttf", ".eot", ".svg", ".png", ".jpg", ".jpeg", ".gif", ".ico", ".webp":
		return true
	}
	return false
}
