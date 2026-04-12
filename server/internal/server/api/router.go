package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// SetupRouter creates and configures the Chi router.
func SetupRouter(cfg *config.Config, repo repository.Repository, log *slog.Logger) *chi.Mux {
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

	// TODO: tRPC router will be mounted here
	// r.Handle("/trpc/*", trpcRouter.Handler("/trpc"))

	// TODO: Chi routes for non-tRPC endpoints
	// r.Route("/api/v1", func(r chi.Router) {
	//     auth.SetupOAuthRoutes(r, ...)
	//     webhook.SetupRoutes(r, ...)
	//     video.SetupStreamingRoutes(r, ...)
	// })

	// SPA fallback
	if cfg.Env.DashboardDir != "" {
		setupDashboardRoutes(r, cfg.Env.DashboardDir, log)
	}

	return r
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
