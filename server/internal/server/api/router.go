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

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/invite"
	"github.com/befabri/replayvod/server/internal/mediastore"
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

// RecordingServices shares storage identity, recording locks and media ownership
// between HTTP handlers and background workers.
type RecordingServices struct {
	Retention    *retention.Service
	StorageScan  *storagescan.Service
	PlaybackAuth *playbackauth.Service
	// StorageHealth reports unsupported backends as unavailable, leaving the
	// dashboard accessible while storage work is paused.
	StorageHealth *storagehealth.Monitor
	Media         *mediastore.Store
}

func NewRecordingServices(cfg *config.Config, repo repository.Repository, store storage.Storage, bus *eventbus.Buses, log *slog.Logger) *RecordingServices {
	health := storagehealth.New(repo, store, bus, log, storageBackend(cfg), storageLocation(cfg))
	locks := &recordinglock.Locks{}
	media := mediastore.New(repo, store, health, locks, cfg.Env.ScratchDir)
	return &RecordingServices{
		Retention:     retention.New(repo, media, log, retention.WithManualDeletionWorkerAvailable(cfg.App.Scheduler.Enabled), retention.WithEventBus(bus)),
		StorageScan:   storagescan.New(repo, media, log, storagescan.WithEventBus(bus)),
		PlaybackAuth:  playbackauth.New(repo, cfg.Env.SessionSecret, playbackauth.NewTwitchValidator()),
		StorageHealth: health,
		Media:         media,
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

// SetupRouter requires shared recording services. Its cleanup function closes
// transports and stops and joins follow imports and waveform generation.
func SetupRouter(cfg *config.Config, repo repository.Repository, sessionMgr *session.Manager, twitchClient *twitch.Client, store storage.Storage, dl *downloader.Service, hydrator *streammeta.Hydrator, bus *eventbus.Buses, eventProcessor *schedulesvc.EventProcessor, webhookDispatcher *recordingwebhook.Dispatcher, playbackCache *playbackcache.Service, log *slog.Logger, recordings *RecordingServices) (*chi.Mux, func() error) {
	r := chi.NewRouter()
	trustedBrowserOrigins := cfg.TrustedBrowserOrigins()

	// Mount the standard /debug paths expected by go tool pprof.
	if cfg.App.Development {
		r.Mount("/debug", chimiddleware.Profiler())
	}

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

	followSync := followsync.New(repo, twitchClient, log)
	authHandler := auth.NewHandler(cfg, twitchClient, sessionMgr, authSvc, followSync, log)
	if recordings == nil || recordings.Media == nil {
		panic("shared recording services required")
	}
	storageGate := recordings.StorageHealth
	streamOpts := []video.StreamHandlerOption{
		video.WithPlaybackBuilder(playbackCache),
		video.WithMissingMarker(recordings.StorageScan),
	}
	videoStream := video.NewStreamHandler(
		repo,
		recordings.Media,
		videodownload.NewVerifier(cfg.Env.HMACSecret),
		log,
		streamOpts...,
	)
	// Twitch needs raw-body HMAC verification and cannot provide CSRF cookies.
	// Disabled EventSub deliveries are audited without starting recordings.
	var webhookProcessor webhook.EventProcessor
	if cfg.ServerMode.ProcessesWebhookNotifications() {
		webhookProcessor = eventProcessor
	}
	webhookHandler := webhook.NewHandler(repo, cfg.Env.HMACSecret, webhookProcessor, log)
	tokenProvider := middleware.NewSessionTokenProvider(sessionMgr, twitchClient, log)
	authenticator := middleware.NewAuthenticator(sessionMgr, repo, tokenProvider, log)
	r.Route("/api/v1", func(r chi.Router) {
		if cfg.App.Health.Enabled {
			r.Get("/health", healthHandler(repo, storageGate, log))
		}
		authHandler.SetupRoutes(r)
		videoStream.SetupRoutes(r, authenticator.HTTP)
		videoStream.SetupSignedRoutes(r)
		webhookHandler.SetupRoutes(r)
	})

	trpcRouter := setupTRPCRouter(cfg, repo, sessionMgr, authenticator, twitchClient, dl, hydrator, store, bus, authSvc, scheduleSvc, webhookDispatcher, recordings, log)
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
	r.With(authenticator.HTTP).Get("/trpc/ws", wsHandler.ServeHTTP)
	r.Group(func(r chi.Router) {
		r.Use(csrfProtection.Handler)
		// Register methods explicitly so CORS discovers them and chi rejects others with 405.
		for _, method := range trpc.Methods() {
			r.Method(method, "/trpc/*", trpcHandler)
		}
	})

	// Warn when an explicit dashboard path is missing; silently skip an absent
	// bundled dashboard in source deployments.
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
		followSync.Stop()
		_ = wsHandler.Close()
		// Cancelled imports must finish using the repository before its owner closes it.
		return errors.Join(videoStream.Close(), followSync.Wait(context.Background()), trpcRouter.Close())
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

func setupTRPCRouter(cfg *config.Config, repo repository.Repository, sessionMgr *session.Manager, authenticator *middleware.Authenticator, twitchClient *twitch.Client, dl *downloader.Service, hydrator *streammeta.Hydrator, store storage.Storage, bus *eventbus.Buses, authSvc *auth.Service, scheduleSvc *schedulesvc.Service, webhookDispatcher *recordingwebhook.Dispatcher, recordings *RecordingServices, log *slog.Logger) *trpcgo.Router {
	opts := []trpcgo.Option{
		trpcgo.WithContextCreator(middleware.WithContextCreator),
		trpcgo.WithValidator(validate.V.Struct),
		trpcgo.WithBatching(true),
		// Dashboard views exceed the default batch limit of 10; 50 still bounds request work.
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

	adminMw := middleware.TRPCRequireRole(middleware.RoleAdmin)
	ownerMw := middleware.TRPCRequireRole(middleware.RoleOwner)

	authed := trpcgo.Procedure().Use(authenticator.TRPC)
	viewer := authed
	admin := authed.Use(adminMw)
	owner := authed.Use(ownerMw)

	eventsubMgr := eventsubsvc.New(repo, twitchClient, cfg.ServerModeCallbackURL(), cfg.Env.HMACSecret, log)
	eventsubConfigSvc := eventsubconfig.New(repo, cfg, log)

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
	video.RegisterRoutes(tr, repo, dl, twitchClient, hydrator, recordings.Retention, recordings.StorageScan, recordings.Media, log, viewer, admin)

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
