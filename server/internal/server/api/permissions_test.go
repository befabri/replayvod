package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"testing"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	schedulesvc "github.com/befabri/replayvod/server/internal/service/schedule"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/testdb"
)

type permissionHarness struct {
	router http.Handler
	repo   repository.Repository
	viewer *http.Cookie
	admin  *http.Cookie
	owner  *http.Cookie
}

func newPermissionHarness(t *testing.T) *permissionHarness {
	t.Helper()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessionMgr, err := session.NewManager(repo, "permission-matrix-session-secret-0123456789", false, log)
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
	return &permissionHarness{
		router: router,
		repo:   repo,
		viewer: mintSessionCookie(t, repo, sessionMgr, "perm-viewer-1", "viewer"),
		admin:  mintSessionCookie(t, repo, sessionMgr, "perm-admin-1", "admin"),
		owner:  mintSessionCookie(t, repo, sessionMgr, "perm-owner-1", "owner"),
	}
}

func (h *permissionHarness) do(method, path, body string, cookie *http.Cookie) int {
	return roleGateRequest(h.router, method, path, body, cookie)
}

func queryWithInput(path, input string) string {
	return path + "?input=" + url.QueryEscape(input)
}

// TestSystemProceduresRoleMatrix exercises router registration, which handler
// tests bypass.
func TestSystemProceduresRoleMatrix(t *testing.T) {
	h := newPermissionHarness(t)

	if _, err := h.repo.UpsertUser(context.Background(), &repository.User{
		ID: "perm-target-1", Login: "perm-target-1", DisplayName: "perm-target-1", Role: "viewer",
	}); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	adminTier := []struct {
		name   string
		method string
		path   string
		body   string
		// want is the status after authorization succeeds.
		want int
	}{
		{"listUsers", http.MethodGet, "/trpc/system.listUsers", "", http.StatusOK},
		{"listWhitelist", http.MethodGet, "/trpc/system.listWhitelist", "", http.StatusOK},
		{"addWhitelist", http.MethodPost, "/trpc/system.addWhitelist", `{"twitch_user_id":"12345"}`, http.StatusOK},
		{"removeWhitelist", http.MethodPost, "/trpc/system.removeWhitelist", `{"twitch_user_id":"12345"}`, http.StatusOK},
		{"updateUserRole", http.MethodPost, "/trpc/system.updateUserRole", `{"user_id":"perm-target-1","role":"viewer"}`, http.StatusOK},
		{"listInvites", http.MethodGet, "/trpc/system.listInvites", "", http.StatusOK},
		{"createInvite", http.MethodPost, "/trpc/system.createInvite", `{"role":"viewer","ttl_minutes":60}`, http.StatusOK},
		{"revokeInvite", http.MethodPost, "/trpc/system.revokeInvite", `{"id":99999}`, http.StatusNotFound},
	}
	for _, tc := range adminTier {
		t.Run("admin-tier/"+tc.name, func(t *testing.T) {
			if got := h.do(tc.method, tc.path, tc.body, nil); got != http.StatusUnauthorized {
				t.Fatalf("%s without a session = %d, want 401", tc.path, got)
			}
			if got := h.do(tc.method, tc.path, tc.body, h.viewer); got != http.StatusForbidden {
				t.Fatalf("%s as viewer = %d, want 403", tc.path, got)
			}
			if got := h.do(tc.method, tc.path, tc.body, h.admin); got != tc.want {
				t.Fatalf("%s as admin = %d, want %d", tc.path, got, tc.want)
			}
			if got := h.do(tc.method, tc.path, tc.body, h.owner); got != tc.want {
				t.Fatalf("%s as owner = %d, want %d", tc.path, got, tc.want)
			}
		})
	}

	ownerTier := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"fetchLogs", http.MethodGet, queryWithInput("/trpc/system.fetchLogs", `{"limit":10,"offset":0}`), "", http.StatusOK},
		{"eventLogs", http.MethodGet, queryWithInput("/trpc/system.eventLogs", `{"limit":10,"offset":0}`), "", http.StatusOK},
		{"searchEventLogs", http.MethodGet, queryWithInput("/trpc/system.searchEventLogs", `{"query":"x","limit":10,"offset":0}`), "", http.StatusOK},
		{"playbackCacheConfig", http.MethodGet, "/trpc/system.playbackCacheConfig", "", http.StatusOK},
		{"updatePlaybackCacheConfig", http.MethodPost, "/trpc/system.updatePlaybackCacheConfig", `{"enabled":false,"max_percent":10,"auto_generate":false}`, http.StatusOK},
	}
	for _, tc := range ownerTier {
		t.Run("owner-tier/"+tc.name, func(t *testing.T) {
			if got := h.do(tc.method, tc.path, tc.body, nil); got != http.StatusUnauthorized {
				t.Fatalf("%s without a session = %d, want 401", tc.path, got)
			}
			if got := h.do(tc.method, tc.path, tc.body, h.viewer); got != http.StatusForbidden {
				t.Fatalf("%s as viewer = %d, want 403", tc.path, got)
			}
			if got := h.do(tc.method, tc.path, tc.body, h.admin); got != http.StatusForbidden {
				t.Fatalf("%s as admin = %d, want 403 (owner-only, not merely admin)", tc.path, got)
			}
			if got := h.do(tc.method, tc.path, tc.body, h.owner); got != tc.want {
				t.Fatalf("%s as owner = %d, want %d", tc.path, got, tc.want)
			}
		})
	}
}

func TestUpdateUserRoleOwnerCarveOutOverHTTP(t *testing.T) {
	h := newPermissionHarness(t)
	ctx := context.Background()
	if _, err := h.repo.UpsertUser(ctx, &repository.User{
		ID: "carveout-target-1", Login: "carveout-target-1", DisplayName: "carveout-target-1", Role: "viewer",
	}); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	roleBody := func(target, role string) string {
		return fmt.Sprintf(`{"user_id":%q,"role":%q}`, target, role)
	}

	if got := h.do(http.MethodPost, "/trpc/system.updateUserRole", roleBody("carveout-target-1", "owner"), h.admin); got != http.StatusForbidden {
		t.Fatalf("admin grants owner = %d, want 403", got)
	}
	if got := h.do(http.MethodPost, "/trpc/system.updateUserRole", roleBody("perm-owner-1", "viewer"), h.admin); got != http.StatusForbidden {
		t.Fatalf("admin demotes an owner = %d, want 403", got)
	}
	if got := h.do(http.MethodPost, "/trpc/system.updateUserRole", roleBody("carveout-target-1", "admin"), h.admin); got != http.StatusOK {
		t.Fatalf("admin promotes viewer→admin = %d, want 200", got)
	}
	if got := h.do(http.MethodPost, "/trpc/system.updateUserRole", roleBody("carveout-target-1", "owner"), h.owner); got != http.StatusOK {
		t.Fatalf("owner grants owner = %d, want 200", got)
	}
	if got := h.do(http.MethodPost, "/trpc/system.updateUserRole", roleBody("perm-owner-1", "viewer"), h.owner); got != http.StatusBadRequest {
		t.Fatalf("owner self-demotes = %d, want 400", got)
	}

	target, err := h.repo.GetUser(ctx, "carveout-target-1")
	if err != nil {
		t.Fatalf("reload target: %v", err)
	}
	if target.Role != "owner" {
		t.Fatalf("target role after owner grant = %q, want owner", target.Role)
	}
}

// TestRemoveWhitelistOwnerCarveOutOverHTTP checks that admins cannot revoke
// owner access.
func TestRemoveWhitelistOwnerCarveOutOverHTTP(t *testing.T) {
	h := newPermissionHarness(t)
	ctx := context.Background()
	for _, id := range []string{"perm-owner-1", "perm-viewer-1"} {
		if err := h.repo.AddToWhitelist(ctx, id); err != nil {
			t.Fatalf("seed whitelist %s: %v", id, err)
		}
	}

	body := func(target string) string {
		return fmt.Sprintf(`{"twitch_user_id":%q}`, target)
	}

	if got := h.do(http.MethodPost, "/trpc/system.removeWhitelist", body("perm-owner-1"), h.admin); got != http.StatusForbidden {
		t.Fatalf("admin de-whitelists owner = %d, want 403", got)
	}
	if got := h.do(http.MethodPost, "/trpc/system.removeWhitelist", body("perm-viewer-1"), h.admin); got != http.StatusOK {
		t.Fatalf("admin de-whitelists viewer = %d, want 200", got)
	}
	if got := h.do(http.MethodPost, "/trpc/system.removeWhitelist", body("perm-owner-1"), h.owner); got != http.StatusOK {
		t.Fatalf("owner de-whitelists self = %d, want 200", got)
	}
}
