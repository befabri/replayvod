//go:build integration

// Package e2e exercises the tRPC HTTP surface against both database
// drivers. The harness wires the real router, repository, session
// manager, storage (local, tmpdir), and Twitch client (stubbed
// credentials — smoke cases don't hit Twitch) into an httptest.Server
// and seeds a signed-in user so cookie-authenticated procedures are
// callable without going through the Twitch OAuth dance.
//
// Run with:
//
//	go test -tags integration ./internal/e2e/
//
// SetupPG pulls postgres:16-alpine on first run (~40 MB); the SQLite
// side needs no containers.
package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
	"github.com/befabri/replayvod/server/internal/server/api"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

func TestMain(m *testing.M) {
	// PG container boots once for every test in this package; SQLite
	// tests still run even if Docker isn't present, they just pay the
	// spin-up cost as a no-op.
	os.Exit(testdb.SetupPG(m))
}

// driver names what DB backend the matrix subtest is exercising.
type driver string

const (
	driverSQLite driver = "sqlite"
	driverPG     driver = "postgres"
)

// testServer is everything a test needs to talk to a running
// single-user copy of the stack: the base URL of the httptest server,
// the raw session ID to set as the cookie, and the seeded user's ID
// for assertions.
type testServer struct {
	baseURL   string
	sessionID string
	userID    string
	repo      repository.Repository
}

// newTestServer spins up the full tRPC stack against the requested
// driver and signs in a fresh "owner"-role user by writing a session
// row directly — no Twitch OAuth involvement.
func newTestServer(t *testing.T, d driver) *testServer {
	t.Helper()

	repo := newRepo(t, d)

	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Fixed secret: deterministic key derivation makes failing tests
	// easier to reproduce manually by copying the session cookie.
	const sessionSecret = "00000000000000000000000000000000"
	sessionMgr, err := session.NewManager(repo, sessionSecret, false, log)
	if err != nil {
		t.Fatalf("session manager: %v", err)
	}

	cfg := newTestConfig(t)

	twitchClient := twitch.NewClient("test-client-id", "test-client-secret", log)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	dl := downloader.NewService(cfg, repo, store, log)

	// bus=nil is fine: subscription procedures return pre-closed
	// channels when the bus is absent, which is the same contract the
	// production server exposes when started without SSE.
	router, closeTRPC := api.SetupRouter(cfg, repo, sessionMgr, twitchClient, store, dl, nil, log)
	srv := httptest.NewServer(router)
	t.Cleanup(func() {
		srv.Close()
		if err := closeTRPC(); err != nil {
			t.Logf("closeTRPC: %v", err)
		}
	})

	user := seedOwner(t, repo)
	rawID := seedSession(t, repo, sessionMgr, user.ID)

	return &testServer{
		baseURL:   srv.URL,
		sessionID: rawID,
		userID:    user.ID,
		repo:      repo,
	}
}

func newRepo(t *testing.T, d driver) repository.Repository {
	t.Helper()
	switch d {
	case driverSQLite:
		db := testdb.NewSQLiteDB(t)
		return sqliteadapter.New(sqlitegen.New(db))
	case driverPG:
		pool := testdb.NewPGPool(t)
		return pgadapter.New(pggen.New(pool))
	}
	t.Fatalf("unknown driver %q", d)
	return nil
}

// newTestConfig builds a Config with just enough populated for the
// smoke flow: a unique session secret, the default app config, and
// a dashboard-dir-less env so the SPA fallback doesn't fight the
// tRPC handler on non-matching paths.
func newTestConfig(t *testing.T) *config.Config {
	t.Helper()
	app := defaultAppForTest()
	return &config.Config{
		Env: config.Environment{
			SessionSecret:    "00000000000000000000000000000000",
			TwitchClientID:   "test-client-id",
			TwitchSecret:     "test-client-secret",
			HMACSecret:       "test-hmac",
			Host:             "127.0.0.1",
			Port:             0,
			WhitelistEnabled: false,
			VideoDir:         t.TempDir(),
			ThumbnailDir:     t.TempDir(),
			ScratchDir:       t.TempDir(),
		},
		App: app,
	}
}

// defaultAppForTest mirrors the validated-default AppConfig produced
// by config.LoadConfig for a missing/empty TOML.
func defaultAppForTest() config.AppConfig {
	return config.AppConfig{
		Server: config.ServerConfig{AllowedOrigins: []string{}},
		Download: config.DownloadConfig{
			MaxConcurrent:        2,
			PreferredQuality:     "1080",
			SegmentConcurrency:   4,
			NetworkAttempts:      5,
			ServerErrorAttempts:  5,
			CDNLagAttempts:       3,
			AuthRefreshAttempts:  2,
			MaxGapRatio:          0.01,
			MaxRestartGapSeconds: 120,
			AudioRate:            48000,
		},
		Storage:  config.StorageConfig{Type: "local", LocalPath: ""},
		Logging:  config.LoggingConfig{SampleRate: 1.0, LogLevel: "warn"},
		PostgresPool: config.PostgresPoolConfig{
			MaxConns: 25, MinConns: 5,
			MaxConnLifetimeMs: 1800000, MaxConnIdleTimeMs: 300000,
			HealthCheckPeriodMs: 30000,
		},
	}
}

// seedOwner writes a test user with owner role directly into the
// repository. OAuth isn't exercised by the smoke flow — it's the DB
// write behind the callback that matters for auth.session et al.
func seedOwner(t *testing.T, repo repository.Repository) *repository.User {
	t.Helper()
	u := &repository.User{
		ID:          "test-user-1",
		Login:       "testuser",
		DisplayName: "Test User",
		Role:        "owner",
	}
	saved, err := repo.UpsertUser(context.Background(), u)
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	return saved
}

// seedSession creates a session via the real manager so encryption,
// hashing, and cookie derivation stay in sync with production —
// writing straight to the sessions table would silently skip those
// code paths.
func seedSession(t *testing.T, repo repository.Repository, mgr *session.Manager, userID string) string {
	t.Helper()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/seed", nil)
	tokens := &session.TwitchTokens{
		AccessToken:  "test-access",
		RefreshToken: "test-refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	if err := mgr.Create(req.Context(), rec, userID, tokens, req); err != nil {
		t.Fatalf("session create: %v", err)
	}

	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			return c.Value
		}
	}
	t.Fatalf("session cookie not set after Create")
	return ""
}

// trpcQuery performs a tRPC query (GET) and unmarshals the `result.data`
// envelope into dst. Status codes other than 200 fail the test —
// callers that expect errors should use trpcRequest directly.
func trpcQuery(t *testing.T, ts *testServer, procedure string, input any, dst any) {
	t.Helper()
	endpoint := ts.baseURL + "/trpc/" + procedure
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatalf("marshal input: %v", err)
		}
		endpoint += "?input=" + url.QueryEscape(string(raw))
	}
	body := doRequest(t, ts, http.MethodGet, endpoint, nil)
	decodeEnvelope(t, procedure, body, dst)
}

// trpcMutation is the POST counterpart to trpcQuery — same envelope,
// body carries the input instead of the querystring.
func trpcMutation(t *testing.T, ts *testServer, procedure string, input any, dst any) {
	t.Helper()
	endpoint := ts.baseURL + "/trpc/" + procedure
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatalf("marshal input: %v", err)
		}
		body = bytes.NewReader(raw)
	}
	respBody := doRequest(t, ts, http.MethodPost, endpoint, body)
	decodeEnvelope(t, procedure, respBody, dst)
}

func doRequest(t *testing.T, ts *testServer, method, endpoint string, body io.Reader) []byte {
	t.Helper()
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: ts.sessionID})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, endpoint, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s %s: status %d, body=%s", method, endpoint, resp.StatusCode, raw)
	}
	return raw
}

// decodeEnvelope unwraps {"result":{"data":...}} and decodes into dst
// if dst is non-nil. Errors in the envelope fail the test.
func decodeEnvelope(t *testing.T, procedure string, raw []byte, dst any) {
	t.Helper()
	var env struct {
		Result *struct {
			Data json.RawMessage `json:"data"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("%s: decode envelope: %v (raw=%s)", procedure, err, raw)
	}
	if env.Error != nil {
		t.Fatalf("%s: server error: code=%d message=%q", procedure, env.Error.Code, env.Error.Message)
	}
	if env.Result == nil {
		t.Fatalf("%s: missing result (raw=%s)", procedure, raw)
	}
	if dst != nil {
		if err := json.Unmarshal(env.Result.Data, dst); err != nil {
			t.Fatalf("%s: decode data: %v (raw=%s)", procedure, err, env.Result.Data)
		}
	}
}

