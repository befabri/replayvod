package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// healthHandler is a readiness probe: pings the backing database and
// returns 200 with {"status":"ok"} on success, 503 with
// {"status":"unhealthy","error":...} when the ping fails. Gated on
// config.toml `[health] enabled` so deployments that don't want an
// unauthenticated probe simply leave it off.
func healthHandler(repo repository.Repository, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		w.Header().Set("Content-Type", "application/json")
		if err := repo.Ping(ctx); err != nil {
			log.Warn("health: database ping failed", "error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "unhealthy",
				"error":  err.Error(),
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}
