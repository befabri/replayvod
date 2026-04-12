package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/server/api/routes/auth"
	"github.com/befabri/replayvod/server/internal/server/api/routes/category"
	"github.com/befabri/replayvod/server/internal/server/api/routes/channel"
	systemroute "github.com/befabri/replayvod/server/internal/server/api/routes/system"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/befabri/replayvod/server/internal/validate"
	"github.com/befabri/trpcgo"
	"github.com/befabri/trpcgo/trpc"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// SetupRouter creates and configures the Chi router.
func SetupRouter(cfg *config.Config, repo repository.Repository, sessionMgr *session.Manager, twitchClient *twitch.Client, log *slog.Logger) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(middleware.Logger(log))
	r.Use(middleware.Recoverer(log))
	r.Use(middleware.CORS(cfg.App.Server.AllowedOrigins))

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Chi routes (non-tRPC: OAuth, webhooks, video streaming)
	authHandler := auth.NewHandler(cfg, repo, twitchClient, sessionMgr, log)
	r.Route("/api/v1", func(r chi.Router) {
		authHandler.SetupRoutes(r)
	})

	// tRPC router with CSRF/origin protection
	trpcRouter := setupTRPCRouter(cfg, repo, sessionMgr, twitchClient, log)
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

	return r
}

// setupTRPCRouter builds the tRPC router with all procedures.
func setupTRPCRouter(cfg *config.Config, repo repository.Repository, sessionMgr *session.Manager, twitchClient *twitch.Client, log *slog.Logger) *trpcgo.Router {
	// Services
	authSvc := auth.NewService(repo, sessionMgr, log)
	channelSvc := channel.NewService(repo, twitchClient, log)
	categorySvc := category.NewService(repo, log)
	systemSvc := systemroute.NewService(repo, log)

	// Middleware
	authMw := middleware.TRPCAuth(sessionMgr, repo, log)
	ownerMw := middleware.TRPCRequireRole(middleware.RoleOwner)

	// Base procedures (ProcedureBuilder pattern)
	authedProcedure := trpcgo.Procedure().Use(authMw)
	viewerProcedure := authedProcedure
	ownerProcedure := authedProcedure.Use(ownerMw)

	opts := []trpcgo.Option{
		trpcgo.WithContextCreator(middleware.WithContextCreator),
		trpcgo.WithValidator(validate.V.Struct),
		trpcgo.WithBatching(true),
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

	// Auth procedures (all require authenticated session)
	trpcgo.MustVoidQuery(tr, "auth.session", authSvc.Session, authedProcedure)
	trpcgo.MustVoidMutation(tr, "auth.logout", authSvc.Logout, authedProcedure)
	trpcgo.MustVoidQuery(tr, "auth.sessions", authSvc.ListSessions, authedProcedure)
	trpcgo.MustMutation(tr, "auth.revokeSession", authSvc.RevokeSession, authedProcedure)

	// Channel procedures
	trpcgo.MustQuery(tr, "channel.getById", channelSvc.GetByID, viewerProcedure)
	trpcgo.MustQuery(tr, "channel.getByLogin", channelSvc.GetByLogin, viewerProcedure)
	trpcgo.MustVoidQuery(tr, "channel.list", channelSvc.List, viewerProcedure)
	trpcgo.MustVoidQuery(tr, "channel.listFollowed", channelSvc.ListFollowed, viewerProcedure)
	trpcgo.MustMutation(tr, "channel.syncFromTwitch", channelSvc.SyncFromTwitch, ownerProcedure)

	// Category procedures
	trpcgo.MustQuery(tr, "category.getById", categorySvc.GetByID, viewerProcedure)
	trpcgo.MustVoidQuery(tr, "category.list", categorySvc.List, viewerProcedure)

	// System procedures (owner only)
	trpcgo.MustQuery(tr, "system.fetchLogs", systemSvc.FetchLogs, ownerProcedure)
	trpcgo.MustVoidQuery(tr, "system.listUsers", systemSvc.ListUsers, ownerProcedure)
	trpcgo.MustMutation(tr, "system.updateUserRole", systemSvc.UpdateUserRole, ownerProcedure)
	trpcgo.MustVoidQuery(tr, "system.listWhitelist", systemSvc.ListWhitelist, ownerProcedure)
	trpcgo.MustMutation(tr, "system.addWhitelist", systemSvc.AddWhitelist, ownerProcedure)
	trpcgo.MustMutation(tr, "system.removeWhitelist", systemSvc.RemoveWhitelist, ownerProcedure)

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
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/trpc/") || path == "/health" {
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
