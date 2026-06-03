package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// pinger is the narrow slice of the repository the health probe needs.
// Keeping it local lets the handler be tested with a fake and keeps the
// probe from depending on the full 205-method repository surface.
type pinger interface {
	Ping(ctx context.Context) error
}

// healthHandler is a readiness probe: pings the backing database and
// returns 200 with {"status":"ok"} on success, 503 with
// {"status":"unhealthy"} when the ping fails. The route is registered
// before session middleware (router.go), so it is unauthenticated: the
// raw database error is logged but deliberately kept out of the response
// body so an anonymous caller can't probe internals. Gated on config.toml
// `[health] enabled` so deployments that don't want an unauthenticated
// probe simply leave it off.
func healthHandler(repo pinger, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		w.Header().Set("Content-Type", "application/json")
		if err := repo.Ping(ctx); err != nil {
			log.Warn("health: database ping failed", "error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}
