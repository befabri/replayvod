package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/befabri/replayvod/server/internal/relayclient"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// Logger returns a middleware that logs HTTP requests.
func Logger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			// The relay client dispatches every queued webhook to this
			// loopback path and emits its own wide event per frame; gate
			// the skip on its sentinel header so manual curls, tests, or
			// any non-relay caller hitting the callback still get an
			// access log line.
			if r.URL.Path == "/api/v1/webhook/callback" && r.Header.Get(relayclient.RelayDispatchHeader) != "" {
				return
			}

			log.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"duration", time.Since(start).String(),
				"bytes", ww.BytesWritten(),
			)
		})
	}
}
