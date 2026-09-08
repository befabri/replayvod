package twitch

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestPlaybackAttemptDoesNotForwardWebsiteSessionOnRedirect(t *testing.T) {
	var leaked atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked.Store(true)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/integrity" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	c := New(Config{GQLURL: origin.URL + "/gql", IntegrityURL: origin.URL + "/integrity"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := c.PlaybackToken(context.Background(), "viewer", "private-website-session"); err == nil {
		t.Fatal("redirect was accepted")
	}
	if leaked.Load() {
		t.Fatal("website session forwarded to redirect target")
	}
}
