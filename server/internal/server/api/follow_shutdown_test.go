package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

type shutdownFollowTransport func(*http.Request) (*http.Response, error)

func (f shutdownFollowTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRouterOwnsLoginFollowImportThroughShutdown(t *testing.T) {
	synctest.Test(t, testRouterOwnsLoginFollowImportThroughShutdown)
}

func testRouterOwnsLoginFollowImportThroughShutdown(t *testing.T) {
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	log := slog.New(slog.DiscardHandler)
	raw, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Env: config.Environment{Host: "localhost", ScratchDir: t.TempDir(), CallbackURL: "http://localhost/api/v1/auth/twitch/callback", FrontendURL: "http://localhost:3000"},
		App: config.AppConfig{Storage: config.StorageConfig{Type: "local", LocalPath: raw.Root}},
	}
	mgr, err := session.NewManager(repo, "follow-shutdown-session-secret-0123456789", false, log)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan context.Context, 1)
	cancelled, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	tc := twitch.NewClient("client", "secret", log)
	tc.SetHTTPClient(&http.Client{Transport: shutdownFollowTransport(func(req *http.Request) (*http.Response, error) {
		var body string
		switch {
		case req.URL.Host == "id.twitch.tv":
			body = `{"access_token":"user-token","refresh_token":"refresh-token","expires_in":3600,"token_type":"bearer"}`
		case strings.HasSuffix(req.URL.Path, "/users"):
			body = `{"data":[{"id":"viewer","login":"viewer","display_name":"Viewer"}]}`
		case strings.HasSuffix(req.URL.Path, "/channels/followed"):
			if calls.Add(1) != 1 {
				t.Error("login launched follow import after shutdown")
				return nil, errors.New("unexpected follow import")
			}
			started <- req.Context()
			<-req.Context().Done()
			close(cancelled)
			<-release
			return nil, req.Context().Err()
		default:
			return nil, fmt.Errorf("unexpected Twitch request: %s", req.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	bus := eventbus.New()
	recordings := NewRecordingServices(cfg, repo, raw, bus, log)
	router, closeRouter := SetupRouter(cfg, repo, mgr, tc, raw, nil, nil, bus, nil, nil, nil, log, recordings)
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }); _ = closeRouter() })
	login := func(ctx context.Context) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/twitch/callback?state=state&code=code", nil).WithContext(ctx)
		req.AddCookie(&http.Cookie{Name: "twitch_oauth_state", Value: "state"})
		req.AddCookie(&http.Cookie{Name: "twitch_oauth_verifier", Value: "verifier"})
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusTemporaryRedirect || rr.Header().Get("Location") != "http://localhost:3000/dashboard" {
			t.Fatalf("login failed: status=%d redirect=%s body=%s", rr.Code, rr.Header().Get("Location"), rr.Body.String())
		}
		if sessions, err := repo.ListUserSessions(t.Context(), "viewer"); err != nil || len(sessions) == 0 {
			t.Fatalf("login did not establish a session: %+v, %v", sessions, err)
		}
	}
	requestCtx, disconnect := context.WithCancel(t.Context())
	defer disconnect()
	login(requestCtx)
	var importCtx context.Context
	select {
	case importCtx = <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("login did not start follow import")
	}
	disconnect()
	select {
	case <-importCtx.Done():
		t.Fatal("HTTP disconnect cancelled follow import")
	case <-time.After(20 * time.Millisecond):
	}
	closed := make(chan error, 1)
	go func() { closed <- closeRouter() }()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("router shutdown did not cancel follow import")
	}
	select {
	case err := <-closed:
		t.Fatalf("router returned before follow I/O joined: %v", err)
	case <-time.After(31 * time.Second):
	}
	unblock.Do(func() { close(release) })
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("router did not finish shutdown")
	}
	// A callback already in flight can still establish its session after import
	// admission closes; an optional follow import must not turn login into failure.
	login(t.Context())
	if calls.Load() != 1 {
		t.Fatal("shutdown accepted a new import")
	}
}
