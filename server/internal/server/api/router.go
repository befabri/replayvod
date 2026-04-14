package api

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/auth"
	"github.com/befabri/replayvod/server/internal/server/api/category"
	"github.com/befabri/replayvod/server/internal/server/api/channel"
	"github.com/befabri/replayvod/server/internal/server/api/eventsub"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/server/api/schedule"
	"github.com/befabri/replayvod/server/internal/server/api/settings"
	"github.com/befabri/replayvod/server/internal/server/api/sse"
	"github.com/befabri/replayvod/server/internal/server/api/stream"
	"github.com/befabri/replayvod/server/internal/server/api/system"
	"github.com/befabri/replayvod/server/internal/server/api/tag"
	"github.com/befabri/replayvod/server/internal/server/api/task"
	"github.com/befabri/replayvod/server/internal/server/api/video"
	"github.com/befabri/replayvod/server/internal/server/api/videorequest"
	"github.com/befabri/replayvod/server/internal/server/api/webhook"
	eventsubsvc "github.com/befabri/replayvod/server/internal/service/eventsub"
	schedulesvc "github.com/befabri/replayvod/server/internal/service/schedule"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/befabri/replayvod/server/internal/validate"
	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// SetupRouter creates and configures the Chi router and returns a cleanup
// hook for the tRPC router lifecycle.
func SetupRouter(cfg *config.Config, repo repository.Repository, sessionMgr *session.Manager, twitchClient *twitch.Client, store storage.Storage, dl *downloader.Service, hydrator *streammeta.Hydrator, bus *eventbus.Buses, log *slog.Logger) (*chi.Mux, func() error) {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(middleware.Logger(log))
	r.Use(middleware.Recoverer(log))
	r.Use(middleware.CORS(cfg.App.Server.AllowedOrigins))

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
	scheduleSvc := schedulesvc.New(repo, log)

	// Chi routes (non-tRPC: OAuth, webhooks, video streaming, thumbnails).
	// Video/thumbnail routes reuse the session middleware — auth required
	// for both, and we want the same context population the tRPC side gets.
	authHandler := auth.NewHandler(cfg, twitchClient, sessionMgr, authSvc, log)
	videoStream := video.NewStreamHandler(repo, store, log)
	// The webhook handler needs the raw body for HMAC verification, so it
	// must live on the Chi side (no tRPC JSON middleware) and outside the
	// csrfProtection group (Twitch can't provide a CSRF cookie). The
	// schedule-service processor dispatches stream.online events to the
	// auto-download pipeline; other event types are audit-logged only.
	scheduleProcessor := schedulesvc.NewEventProcessor(repo, dl, twitchClient, hydrator, bus, log)
	webhookHandler := webhook.NewHandler(repo, cfg.Env.HMACSecret, scheduleProcessor, log)
	sessionMw := middleware.Auth(sessionMgr, repo, log)
	r.Route("/api/v1", func(r chi.Router) {
		if cfg.App.Health.Enabled {
			r.Get("/health", healthHandler(repo, log))
		}
		authHandler.SetupRoutes(r)
		videoStream.SetupRoutes(r, sessionMw)
		webhookHandler.SetupRoutes(r)
	})

	// tRPC router with CSRF/origin protection.
	trpcRouter := setupTRPCRouter(cfg, repo, sessionMgr, twitchClient, dl, hydrator, store, bus, authSvc, scheduleSvc, log)
	csrfProtection := http.NewCrossOriginProtection()
	for _, origin := range cfg.App.Server.AllowedOrigins {
		if err := csrfProtection.AddTrustedOrigin(origin); err != nil {
			log.Warn("invalid trusted origin for CSRF protection", "origin", origin, "error", err)
		}
	}
	r.Group(func(r chi.Router) {
		r.Use(csrfProtection.Handler)
		r.Handle("/trpc/*", trpc.NewHandler(trpcRouter, "/trpc"))
	})

	// SPA fallback
	if cfg.Env.DashboardDir != "" {
		setupDashboardRoutes(r, cfg.Env.DashboardDir, log)
	}

	return r, trpcRouter.Close
}

// setupTRPCRouter builds the tRPC router and dispatches procedure
// registration to each domain's RegisterRoutes. authSvc + scheduleSvc
// are the shared domain services constructed by SetupRouter; everything
// else each domain owns.
func setupTRPCRouter(cfg *config.Config, repo repository.Repository, sessionMgr *session.Manager, twitchClient *twitch.Client, dl *downloader.Service, hydrator *streammeta.Hydrator, store storage.Storage, bus *eventbus.Buses, authSvc *auth.Service, scheduleSvc *schedulesvc.Service, log *slog.Logger) *trpcgo.Router {
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
			if err.Code == trpcgo.CodeUnauthorized || err.Code == trpcgo.CodeBadRequest {
				return
			}
			log.Error("tRPC error", "path", path, "code", trpcgo.NameFromCode(err.Code), "message", err.Message)
		}),
	}

	if cfg.App.Development {
		opts = append(opts,
			trpcgo.WithTypeOutput("../dashboard/src/api/generated/trpc.ts"),
			trpcgo.WithZodOutput("../dashboard/src/api/generated/zod.ts"),
			trpcgo.WithWatchPackages("./..."),
		)
	}

	tr := trpcgo.NewRouter(opts...)

	// Procedure builders: authed is the base, viewer/admin/owner layer role
	// middleware on top. Each domain's RegisterRoutes picks the ones it
	// needs, so we pass only what's relevant per call.
	authMw := middleware.TRPCAuth(sessionMgr, repo, log)
	adminMw := middleware.TRPCRequireRole(middleware.RoleAdmin)
	ownerMw := middleware.TRPCRequireRole(middleware.RoleOwner)

	authed := trpcgo.Procedure().Use(authMw)
	viewer := authed
	admin := authed.Use(adminMw)
	owner := authed.Use(ownerMw)

	// EventSub domain service — the tRPC handler shares it with the
	// scheduler cron task. Constructed here rather than in cmd/server
	// because the tRPC side is where it's consumed, but main.go also
	// builds its own copy for the scheduler (see main.go).
	eventsubMgr := eventsubsvc.New(repo, twitchClient, cfg.Env.WebhookCallbackURL, cfg.Env.HMACSecret, log)

	// Dispatch to each domain. Keeps this function stable when a domain
	// adds a new procedure — the change lives in that domain's routes.go.
	auth.RegisterTRPC(tr, authSvc, sessionMgr, log, authed)
	category.RegisterRoutes(tr, repo, log, viewer)
	channel.RegisterRoutes(tr, repo, twitchClient, log, viewer, owner)
	eventsub.RegisterRoutes(tr, eventsubMgr, log, owner)
	schedule.RegisterRoutes(tr, scheduleSvc, log, viewer, admin)
	settings.RegisterRoutes(tr, repo, log, viewer)
	sse.RegisterRoutes(tr, bus, log, viewer, owner)
	stream.RegisterRoutes(tr, repo, twitchClient, log, viewer)
	system.RegisterRoutes(tr, repo, log, owner)
	tag.RegisterRoutes(tr, repo, log, viewer)
	task.RegisterRoutes(tr, repo, log, owner)
	video.RegisterRoutes(tr, repo, dl, twitchClient, hydrator, store, log, viewer, admin)
	videorequest.RegisterRoutes(tr, repo, log, viewer)

	return tr
}

// setupDashboardRoutes serves the dashboard SPA with proper 404→index.html fallback.
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

		// Skip API and tRPC routes (already handled)
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/trpc/") {
			http.NotFound(w, r)
			return
		}

		// Try to serve the file if it exists
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

// isStaticAsset returns true for files that should be cached long-term.
func isStaticAsset(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".js", ".css", ".woff", ".woff2", ".ttf", ".eot", ".svg", ".png", ".jpg", ".jpeg", ".gif", ".ico", ".webp":
		return true
	}
	return false
}
