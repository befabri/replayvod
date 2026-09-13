//go:build integration

// Package e2e_test exercises authenticated tRPC routes against SQLite and
// PostgreSQL. Run with go test -tags integration ./internal/e2e/.
// The shared PostgreSQL fixture requires Docker for the whole package.
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
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/server/api"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

func TestMain(m *testing.M) {
	os.Exit(testdb.SetupPG(m))
}

type driver string

const (
	driverSQLite driver = "sqlite"
	driverPG     driver = "postgres"
)

type testServer struct {
	baseURL    string
	sessionID  string
	userID     string
	repo       repository.Repository
	sessionMgr *session.Manager
}

func newTestServer(t *testing.T, d driver) *testServer {
	t.Helper()

	repo := newRepo(t, d)

	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn}))

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
	recordings := api.NewRecordingServices(cfg, repo, store, nil, log)
	if _, err := recordings.StorageHealth.Attach(t.Context()); err != nil {
		t.Fatal(err)
	}
	dl := downloader.NewService(cfg, repo, recordings.Media, nil, nil, nil, log)

	hydrator := streammeta.NewHydrator(repo, twitchClient, streammeta.Config{}, log)
	router, closeTRPC := api.SetupRouter(cfg, repo, sessionMgr, twitchClient, store, dl, hydrator, nil, nil, nil, nil, log, recordings)
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
		baseURL:    srv.URL,
		sessionID:  rawID,
		userID:     user.ID,
		repo:       repo,
		sessionMgr: sessionMgr,
	}
}

// rawRequest preserves non-200 responses for authorization and validation assertions.
func rawRequest(t *testing.T, ts *testServer, method, procedure string, input any, cookie string) (int, []byte) {
	t.Helper()
	endpoint := ts.baseURL + "/trpc/" + procedure
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatalf("marshal input: %v", err)
		}
		if method == http.MethodGet {
			endpoint += "?input=" + url.QueryEscape(string(raw))
		} else {
			body = bytes.NewReader(raw)
		}
	}
	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Origin", ts.baseURL)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, endpoint, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, raw
}

func newRepo(t *testing.T, d driver) repository.Repository {
	t.Helper()
	switch d {
	case driverSQLite:
		db := testdb.NewSQLiteDB(t)
		return sqliteadapter.New(db)
	case driverPG:
		pool := testdb.NewPGPool(t)
		return pgadapter.New(pool)
	}
	t.Fatalf("unknown driver %q", d)
	return nil
}

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

func defaultAppForTest() config.AppConfig {
	return config.AppConfig{
		Server: config.ServerConfig{},
		Download: config.DownloadConfig{
			MaxConcurrent:        2,
			SegmentConcurrency:   4,
			NetworkAttempts:      5,
			ServerErrorAttempts:  5,
			CDNLagAttempts:       3,
			AuthRefreshAttempts:  2,
			MaxGapRatio:          0.01,
			MaxRestartGapSeconds: 120,
		},
		Storage: config.StorageConfig{Type: "local", LocalPath: ""},
		Logging: config.LoggingConfig{LogLevel: "warn"},
		PostgresPool: config.PostgresPoolConfig{
			MaxConns: 25, MinConns: 5,
			MaxConnLifetimeMs: 1800000, MaxConnIdleTimeMs: 300000,
			HealthCheckPeriodMs: 30000,
		},
	}
}

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

// seedSession uses the real manager so encryption, hashing and cookie
// derivation are exercised by HTTP tests.
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

// trpcQuery decodes the result.data envelope and fails on non-200 status.
// Use rawRequest when asserting errors.
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
	// Same-origin Origin satisfies trpcgo's CSRF check on mutations.
	req.Header.Set("Origin", ts.baseURL)
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

// decodeEnvelope unwraps result.data into dst, if non-nil, and fails on API errors.
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
