package video

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/befabri/replayvod/server/internal/session"
)

// capturingHandler is a slog.Handler that records every emitted record so tests
// can assert on log output (e.g. that a warning fires once, not per request).
type capturingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(string) slog.Handler      { return h }

// countWarn returns how many WARN records carry msgSubstr.
func (h *capturingHandler) countWarn(msgSubstr string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.Level == slog.LevelWarn && strings.Contains(r.Message, msgSubstr) {
			n++
		}
	}
	return n
}

// countAtLeast returns how many records at or above lvl were emitted.
func (h *capturingHandler) countAtLeast(lvl slog.Level) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.Level >= lvl {
			n++
		}
	}
	return n
}

type providerRoundTripFunc func(*http.Request) (*http.Response, error)

func (f providerRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fakeSessionUpdater struct {
	updates int
	last    *session.TwitchTokens
}

func (f *fakeSessionUpdater) UpdateTokens(_ context.Context, _ string, tokens *session.TwitchTokens) error {
	f.updates++
	copy := *tokens
	f.last = &copy
	return nil
}

func testClientLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
