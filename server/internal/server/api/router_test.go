package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/recordingwebhook"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	schedulesvc "github.com/befabri/replayvod/server/internal/service/schedule"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

const routerWebhookSecret = "router-webhook-secret"

// roleGateRequest drives the real tRPC router and returns the status code.
// httptest defaults the Host to example.com; it sends a matching same-origin
// Origin so a POST clears trpcgo's CSRF check and the assertion lands on the
// role gate under test, not the CSRF gate in front of it. A real browser always
// sends Origin on a mutation.
func roleGateRequest(router http.Handler, method, path, body string, cookie *http.Cookie) int {
	var rdr io.Reader
	if body != "" {
		rdr = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Origin", "http://example.com")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr.Code
}

func TestSetupRouter_ServerModeControlsWebhookProcessor(t *testing.T) {
	tests := []struct {
		name          string
		eventSub      config.ServerModeConfig
		wantStatus    string
		wantProcessed bool
		wantStatusBus bool
	}{
		{
			name: "off audits only",
			eventSub: config.ServerModeConfig{
				Source: config.ServerModeConfigSourceEnv,
				Mode:   config.ServerModeOff,
			},
			wantStatus:    repository.WebhookStatusProcessed,
			wantProcessed: true,
			wantStatusBus: false,
		},
		{
			name: "direct processes notifications",
			eventSub: config.ServerModeConfig{
				Source:             config.ServerModeConfigSourceEnv,
				Mode:               config.ServerModeDirect,
				WebhookCallbackURL: "https://replayvod.example/api/v1/webhook/callback",
			},
			wantStatus:    repository.WebhookStatusProcessed,
			wantProcessed: true,
			wantStatusBus: true,
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
			cfg := &config.Config{
				Env: config.Environment{
					HMACSecret:  routerWebhookSecret,
					CallbackURL: "http://localhost:8080/api/v1/auth/twitch/callback",
					FrontendURL: "http://localhost:3000",
				},
				ServerMode: tt.eventSub,
			}
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			bus := eventbus.New()
			eventProcessor := schedulesvc.NewEventProcessor(repo, nil, nil, nil, bus, log)
			statusCtx, cancelStatus := context.WithCancel(context.Background())
			t.Cleanup(cancelStatus)
			statusCh := bus.StreamStatus.Subscribe(statusCtx)

			router, closeTRPC := SetupRouter(cfg, repo, nil, nil, nil, nil, nil, bus, eventProcessor, nil, nil, log)
			if closeTRPC != nil {
				t.Cleanup(func() {
					if err := closeTRPC(); err != nil {
						t.Errorf("close tRPC router: %v", err)
					}
				})
			}

			messageID := fmt.Sprintf("router-msg-%d", i)
			body := []byte(routerNotificationBody("12345", "sub-router", "event-router"))
			req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/callback", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(twitch.EventSubHeaderMessageType, string(twitch.MsgTypeNotification))
			signRouterWebhookRequest(req, messageID, time.Now().UTC().Format(time.RFC3339Nano), body)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			if rr.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
			}

			stored, err := repo.GetWebhookEventByEventID(context.Background(), messageID)
			if err != nil {
				t.Fatalf("audit lookup: %v", err)
			}
			if stored.Status != tt.wantStatus {
				t.Fatalf("Status = %q, want %q", stored.Status, tt.wantStatus)
			}
			if gotProcessed := stored.ProcessedAt != nil; gotProcessed != tt.wantProcessed {
				t.Fatalf("ProcessedAt set = %v, want %v", gotProcessed, tt.wantProcessed)
			}

			select {
			case ev := <-statusCh:
				if !tt.wantStatusBus {
					t.Fatalf("stream status event published in audit-only mode: %+v", ev)
				}
				if ev.Kind != eventbus.StreamStatusOnline || ev.BroadcasterID != "12345" || ev.StreamID != "event-router" {
					t.Fatalf("stream status event = %+v, want online event for broadcaster 12345", ev)
				}
			default:
				if tt.wantStatusBus {
					t.Fatal("stream status event was not published; webhook processor may not be wired")
				}
			}
		})
	}
}

func TestSetupRouterServesConfiguredDashboardDir(t *testing.T) {
	dashboardDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dashboardDir, "index.html"), []byte("configured dashboard"), 0o600); err != nil {
		t.Fatal(err)
	}

	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Env: config.Environment{
			HMACSecret:   routerWebhookSecret,
			CallbackURL:  "http://localhost:8080/api/v1/auth/twitch/callback",
			FrontendURL:  "http://localhost:3000",
			DashboardDir: dashboardDir,
		},
		ServerMode: config.ServerModeConfig{Source: config.ServerModeConfigSourceUnset},
	}
	bus := eventbus.New()
	eventProcessor := schedulesvc.NewEventProcessor(repo, nil, nil, nil, bus, log)
	router, closeTRPC := SetupRouter(cfg, repo, nil, nil, nil, nil, nil, bus, eventProcessor, nil, nil, log)
	if closeTRPC != nil {
		t.Cleanup(func() {
			if err := closeTRPC(); err != nil {
				t.Errorf("close tRPC router: %v", err)
			}
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if body := rr.Body.String(); body != "configured dashboard" {
		t.Fatalf("body = %q, want configured dashboard", body)
	}
}

func TestTRPCMutationTrustsPublicBaseOriginBehindProxy(t *testing.T) {
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessionMgr, err := session.NewManager(repo, "public-origin-test-session-secret-0123456789", false, log)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	cfg := &config.Config{
		Env: config.Environment{
			HMACSecret:    routerWebhookSecret,
			PublicBaseURL: "https://ReplayVOD.Madata.OVH:443",
			CallbackURL:   "https://replayvod.madata.ovh/api/v1/auth/twitch/callback",
			FrontendURL:   "https://replayvod.madata.ovh",
		},
		ServerMode: config.ServerModeConfig{Source: config.ServerModeConfigSourceUnset},
	}
	bus := eventbus.New()
	eventProcessor := schedulesvc.NewEventProcessor(repo, nil, nil, nil, bus, log)
	router, closeTRPC := SetupRouter(cfg, repo, sessionMgr, nil, nil, nil, nil, bus, eventProcessor, nil, nil, log)
	if closeTRPC != nil {
		t.Cleanup(func() {
			if err := closeTRPC(); err != nil {
				t.Errorf("close tRPC router: %v", err)
			}
		})
	}

	req := httptest.NewRequest(http.MethodPost, "/trpc/eventsub.updateConfig", bytes.NewReader([]byte(`{"mode":"off"}`)))
	req.Host = "replayvod:8080"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://replayvod.madata.ovh")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want auth failure after CSRF passes: %s", rr.Code, rr.Body.String())
	}
	if bytes.Contains(rr.Body.Bytes(), []byte("CSRF")) {
		t.Fatalf("unexpected CSRF rejection body: %s", rr.Body.String())
	}
}

// TestEventSubProceduresAreOwnerGated drives the eventsub.* procedures through
// the fully wired router and asserts they sit behind the owner role. This is the
// only thing that catches a routes.go/router.go edit swapping `owner` for a
// lower-privilege builder: the handler unit tests bypass dispatch entirely, so a
// viewer reaching these procedures would otherwise go unnoticed. The query is a
// CSRF-safe GET; the mutations carry valid bodies so the role middleware (which
// runs after input validation) is what rejects them.
func TestEventSubProceduresAreOwnerGated(t *testing.T) {
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessionMgr, err := session.NewManager(repo, "owner-gate-test-session-secret-0123456789", false, log)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	cfg := &config.Config{
		Env: config.Environment{
			HMACSecret:  routerWebhookSecret,
			CallbackURL: "http://localhost:8080/api/v1/auth/twitch/callback",
			FrontendURL: "http://localhost:3000",
		},
		ServerMode: config.ServerModeConfig{Source: config.ServerModeConfigSourceUnset},
	}
	bus := eventbus.New()
	eventProcessor := schedulesvc.NewEventProcessor(repo, nil, nil, nil, bus, log)
	router, closeTRPC := SetupRouter(cfg, repo, sessionMgr, nil, nil, nil, nil, bus, eventProcessor, nil, nil, log)
	if closeTRPC != nil {
		t.Cleanup(func() {
			if err := closeTRPC(); err != nil {
				t.Errorf("close tRPC router: %v", err)
			}
		})
	}

	viewer := mintSessionCookie(t, repo, sessionMgr, "viewer-1", "viewer")
	admin := mintSessionCookie(t, repo, sessionMgr, "admin-1", "admin")
	owner := mintSessionCookie(t, repo, sessionMgr, "owner-1", "owner")

	do := func(method, path, body string, cookie *http.Cookie) int {
		return roleGateRequest(router, method, path, body, cookie)
	}

	// eventsub.config is a void query (GET). Pin the exact role boundary: a
	// viewer and an admin are both rejected, only an owner gets through.
	if got := do(http.MethodGet, "/trpc/eventsub.config", "", nil); got != http.StatusUnauthorized {
		t.Fatalf("eventsub.config without a session = %d, want 401", got)
	}
	if got := do(http.MethodGet, "/trpc/eventsub.config", "", viewer); got != http.StatusForbidden {
		t.Fatalf("eventsub.config as viewer = %d, want 403", got)
	}
	if got := do(http.MethodGet, "/trpc/eventsub.config", "", admin); got != http.StatusForbidden {
		t.Fatalf("eventsub.config as admin = %d, want 403 (owner-only, not merely admin)", got)
	}
	if got := do(http.MethodGet, "/trpc/eventsub.config", "", owner); got != http.StatusOK {
		t.Fatalf("eventsub.config as owner = %d, want 200", got)
	}

	// The owner-only mutations carry valid bodies so input validation passes and
	// the role middleware is the gate that rejects a viewer.
	if got := do(http.MethodPost, "/trpc/eventsub.updateConfig", `{"mode":"off"}`, viewer); got != http.StatusForbidden {
		t.Fatalf("eventsub.updateConfig as viewer = %d, want 403", got)
	}
	if got := do(http.MethodPost, "/trpc/eventsub.subscribeStreamOnline", `{"broadcaster_id":"12345"}`, viewer); got != http.StatusForbidden {
		t.Fatalf("eventsub.subscribeStreamOnline as viewer = %d, want 403", got)
	}
}

func TestExistingSessionUsesFreshRoleAfterDemotion(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessionMgr, err := session.NewManager(repo, "role-demotion-session-secret-0123456789", false, log)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	cfg := &config.Config{
		Env: config.Environment{
			HMACSecret:  routerWebhookSecret,
			CallbackURL: "http://localhost:8080/api/v1/auth/twitch/callback",
			FrontendURL: "http://localhost:3000",
		},
		ServerMode: config.ServerModeConfig{Source: config.ServerModeConfigSourceUnset},
	}
	bus := eventbus.New()
	eventProcessor := schedulesvc.NewEventProcessor(repo, nil, nil, nil, bus, log)
	router, closeTRPC := SetupRouter(cfg, repo, sessionMgr, nil, nil, nil, nil, bus, eventProcessor, nil, nil, log)
	if closeTRPC != nil {
		t.Cleanup(func() {
			if err := closeTRPC(); err != nil {
				t.Errorf("close tRPC router: %v", err)
			}
		})
	}

	cookie := mintSessionCookie(t, repo, sessionMgr, "demoted-owner-1", "owner")
	if got := roleGateRequest(router, http.MethodGet, "/trpc/eventsub.config", "", cookie); got != http.StatusOK {
		t.Fatalf("eventsub.config before demotion = %d, want 200", got)
	}
	if err := repo.UpdateUserRole(ctx, "demoted-owner-1", "viewer"); err != nil {
		t.Fatalf("demote user: %v", err)
	}
	if got := roleGateRequest(router, http.MethodGet, "/trpc/eventsub.config", "", cookie); got != http.StatusForbidden {
		t.Fatalf("eventsub.config after demotion with same session = %d, want 403", got)
	}
}

// TestRecordingWebhookProceduresAreOwnerGated is the route-level regression
// guard for the custom outbound webhook surface. Handler unit tests do not catch
// a route accidentally registered with `viewer`/`admin`; this drives the real
// tRPC router so the signing-secret read path and egress-triggering mutations
// stay owner-only.
func TestRecordingWebhookProceduresAreOwnerGated(t *testing.T) {
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessionMgr, err := session.NewManager(repo, "recording-webhook-gate-session-secret-0123456789", false, log)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	cfg := &config.Config{
		Env: config.Environment{
			HMACSecret:  routerWebhookSecret,
			CallbackURL: "http://localhost:8080/api/v1/auth/twitch/callback",
			FrontendURL: "http://localhost:3000",
		},
		ServerMode: config.ServerModeConfig{Source: config.ServerModeConfigSourceUnset},
	}
	bus := eventbus.New()
	eventProcessor := schedulesvc.NewEventProcessor(repo, nil, nil, nil, bus, log)
	dispatcher := recordingwebhook.NewDispatcher(repo, nil, log)
	router, closeTRPC := SetupRouter(cfg, repo, sessionMgr, nil, nil, nil, nil, bus, eventProcessor, dispatcher, nil, log)
	if closeTRPC != nil {
		t.Cleanup(func() {
			if err := closeTRPC(); err != nil {
				t.Errorf("close tRPC router: %v", err)
			}
		})
	}

	viewer := mintSessionCookie(t, repo, sessionMgr, "rw-viewer-1", "viewer")
	admin := mintSessionCookie(t, repo, sessionMgr, "rw-admin-1", "admin")
	owner := mintSessionCookie(t, repo, sessionMgr, "rw-owner-1", "owner")

	do := func(method, path, body string, cookie *http.Cookie) int {
		return roleGateRequest(router, method, path, body, cookie)
	}

	cases := []struct {
		name      string
		method    string
		path      string
		body      string
		ownerWant int
	}{
		{name: "config", method: http.MethodGet, path: "/trpc/recordingWebhook.config", ownerWant: http.StatusOK},
		{name: "deliveries", method: http.MethodGet, path: "/trpc/recordingWebhook.deliveries", ownerWant: http.StatusOK},
		{name: "updateConfig", method: http.MethodPost, path: "/trpc/recordingWebhook.updateConfig", body: `{"enabled":false,"url":"","events":[]}`, ownerWant: http.StatusOK},
		{name: "regenerateSecret", method: http.MethodPost, path: "/trpc/recordingWebhook.regenerateSecret", ownerWant: http.StatusOK},
		{name: "testDelivery", method: http.MethodPost, path: "/trpc/recordingWebhook.testDelivery", ownerWant: http.StatusOK},
		// Missing id is the handler's expected owner-visible result here. The
		// important assertion is that viewer/admin are stopped at the role gate
		// before the handler can even inspect the id.
		{name: "retryDelivery", method: http.MethodPost, path: "/trpc/recordingWebhook.retryDelivery", body: `{"id":123}`, ownerWant: http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := do(tc.method, tc.path, tc.body, nil); got != http.StatusUnauthorized {
				t.Fatalf("%s without a session = %d, want 401", tc.path, got)
			}
			if got := do(tc.method, tc.path, tc.body, viewer); got != http.StatusForbidden {
				t.Fatalf("%s as viewer = %d, want 403", tc.path, got)
			}
			if got := do(tc.method, tc.path, tc.body, admin); got != http.StatusForbidden {
				t.Fatalf("%s as admin = %d, want 403 (owner-only, not merely admin)", tc.path, got)
			}
			if got := do(tc.method, tc.path, tc.body, owner); got != tc.ownerWant {
				t.Fatalf("%s as owner = %d, want %d", tc.path, got, tc.ownerWant)
			}
		})
	}
}

func TestInfiniteQueryDirectionInputIsAccepted(t *testing.T) {
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessionMgr, err := session.NewManager(repo, "infinite-direction-session-secret-0123456789", false, log)
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	cfg := &config.Config{
		Env: config.Environment{
			HMACSecret:  routerWebhookSecret,
			CallbackURL: "http://localhost:8080/api/v1/auth/twitch/callback",
			FrontendURL: "http://localhost:3000",
		},
		ServerMode: config.ServerModeConfig{Source: config.ServerModeConfigSourceUnset},
	}
	bus := eventbus.New()
	eventProcessor := schedulesvc.NewEventProcessor(repo, nil, nil, nil, bus, log)
	router, closeTRPC := SetupRouter(cfg, repo, sessionMgr, nil, nil, nil, nil, bus, eventProcessor, nil, nil, log)
	if closeTRPC != nil {
		t.Cleanup(func() {
			if err := closeTRPC(); err != nil {
				t.Errorf("close tRPC router: %v", err)
			}
		})
	}
	viewer := mintSessionCookie(t, repo, sessionMgr, "infinite-direction-viewer", "viewer")

	cases := []struct {
		name  string
		path  string
		input string
	}{
		{
			name:  "channel list page",
			path:  "/trpc/channel.listPage",
			input: `{"0":{"limit":60,"sort":"name_asc","live_only":false,"direction":"forward"}}`,
		},
		{
			name:  "video list page",
			path:  "/trpc/video.listPage",
			input: `{"0":{"limit":24,"direction":"forward"}}`,
		},
		{
			name:  "video by broadcaster",
			path:  "/trpc/video.byBroadcaster",
			input: `{"0":{"broadcaster_id":"56649026","limit":24,"direction":"forward"}}`,
		},
		{
			name:  "video by category",
			path:  "/trpc/video.byCategory",
			input: `{"0":{"category_id":"509658","limit":24,"direction":"forward"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path+"?batch=1&input="+url.QueryEscape(tc.input), nil)
			req.AddCookie(viewer)
			rr := httptest.NewRecorder()

			router.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("%s status = %d, want %d; body: %s", tc.path, rr.Code, http.StatusOK, rr.Body.String())
			}
		})
	}
}

// mintSessionCookie seeds a user with the given role and returns a valid session
// cookie for them, so router tests can exercise role-gated procedures end to end.
func mintSessionCookie(t *testing.T, repo repository.Repository, sessionMgr *session.Manager, userID, role string) *http.Cookie {
	t.Helper()
	if _, err := repo.UpsertUser(context.Background(), &repository.User{
		ID:          userID,
		Login:       userID,
		DisplayName: userID,
		Role:        role,
	}); err != nil {
		t.Fatalf("seed user %s: %v", userID, err)
	}
	rec := httptest.NewRecorder()
	seedReq := httptest.NewRequest(http.MethodGet, "/", nil)
	if err := sessionMgr.Create(context.Background(), rec, userID, &session.TwitchTokens{
		AccessToken:  "access-" + userID,
		RefreshToken: "refresh-" + userID,
		ExpiresAt:    time.Now().Add(time.Hour),
	}, seedReq); err != nil {
		t.Fatalf("create session for %s: %v", userID, err)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			return c
		}
	}
	t.Fatalf("session cookie %q not set for %s", session.CookieName, userID)
	return nil
}

func signRouterWebhookRequest(req *http.Request, id, timestamp string, body []byte) {
	mac := hmac.New(sha256.New, []byte(routerWebhookSecret))
	mac.Write([]byte(id))
	mac.Write([]byte(timestamp))
	mac.Write(body)
	req.Header.Set(twitch.EventSubHeaderMessageID, id)
	req.Header.Set(twitch.EventSubHeaderMessageTimestamp, timestamp)
	req.Header.Set(twitch.EventSubHeaderMessageSignature, "sha256="+hex.EncodeToString(mac.Sum(nil)))
}

func routerNotificationBody(broadcasterID, subID, eventID string) string {
	return fmt.Sprintf(`{
		"subscription": {
			"id": %q,
			"status": "enabled",
			"type": "stream.online",
			"version": "1",
			"condition": {"broadcaster_user_id": %q},
			"transport": {"method": "webhook", "callback": "https://example/cb"},
			"created_at": "2026-04-12T00:00:00Z",
			"cost": 1
		},
		"event": {
			"id": %q,
			"broadcaster_user_id": %q,
			"broadcaster_user_login": "coolstreamer",
			"broadcaster_user_name": "CoolStreamer",
			"type": "live",
			"started_at": "2026-04-12T00:05:00Z"
		}
	}`, subID, broadcasterID, eventID, broadcasterID)
}
