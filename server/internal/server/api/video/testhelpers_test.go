package video

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/befabri/replayvod/server/internal/session"
)

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
