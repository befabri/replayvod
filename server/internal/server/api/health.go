package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/befabri/replayvod/server/internal/storage"
)

// pinger is the narrow slice of the repository the health probe needs.
// Keeping it local lets the handler be tested with a fake and keeps the
// probe from depending on the full 205-method repository surface.
type pinger interface {
	Ping(ctx context.Context) error
}

// storageReadiness is the cached verdict of the storage monitor. Nil skips the
// storage field.
type storageReadiness interface {
	Ready() error
}

// healthHandler is a readiness probe: pings the backing database and
// returns 200 with {"status":"ok"} on success, 503 with
// {"status":"unhealthy"} when the ping fails. Storage is reported as
// attached or unattached and turns the answer into a 503 as well, so
// container orchestration sees an unmounted volume; read-only or full storage
// still counts as attached. The route is registered before session
// middleware (router.go), so it is unauthenticated: the raw database and
// storage errors are logged but deliberately kept out of the response body
// so an anonymous caller can't probe internals. Gated on config.toml
// `[health] enabled` so deployments that don't want an unauthenticated
// probe simply leave it off.
func healthHandler(repo pinger, store storageReadiness, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		w.Header().Set("Content-Type", "application/json")
		body := map[string]string{"status": "ok"}
		code := http.StatusOK
		if err := repo.Ping(ctx); err != nil {
			log.Warn("health: database ping failed", "error", err)
			body["status"] = "unhealthy"
			code = http.StatusServiceUnavailable
		}
		if store != nil {
			body["storage"] = "attached"
			if err := store.Ready(); !storage.CanRead(err) {
				body["storage"] = "unattached"
				code = http.StatusServiceUnavailable
			}
		}
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(body)
	}
}
