//go:build integration

package e2e_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

// TestSmoke exercises the tRPC surface an authenticated operator
// touches on first load against both driver backends. The point is to
// catch SQL-dialect drift and middleware wiring regressions — not to
// re-test business logic that's already covered by service- and
// repo-level tests.
func TestSmoke(t *testing.T) {
	for _, d := range []driver{driverSQLite, driverPG} {
		d := d
		t.Run(string(d), func(t *testing.T) {
			t.Parallel()
			ts := newTestServer(t, d)

			t.Run("auth.session", func(t *testing.T) {
				var out struct {
					UserID      string `json:"user_id"`
					Login       string `json:"login"`
					DisplayName string `json:"display_name"`
					Role        string `json:"role"`
				}
				trpcQuery(t, ts, "auth.session", nil, &out)
				if out.UserID != ts.userID {
					t.Fatalf("user_id: want %q, got %q", ts.userID, out.UserID)
				}
				if out.Role != "owner" {
					t.Fatalf("role: want owner, got %q", out.Role)
				}
			})

			t.Run("auth.sessions lists current session", func(t *testing.T) {
				var sessions []struct {
					HashedID string `json:"hashed_id"`
					Current  bool   `json:"current"`
				}
				trpcQuery(t, ts, "auth.sessions", nil, &sessions)
				if len(sessions) != 1 {
					t.Fatalf("expected 1 session, got %d", len(sessions))
				}
				if !sessions[0].Current {
					t.Fatalf("seeded session not marked current")
				}
			})

			t.Run("video.statistics on empty repo", func(t *testing.T) {
				var stats struct {
					Total             int `json:"total"`
					TotalSize         int `json:"total_size"`
					TotalDurationSecs int `json:"total_duration_seconds"`
				}
				trpcQuery(t, ts, "video.statistics", nil, &stats)
				if stats.Total != 0 {
					t.Fatalf("expected 0 videos, got %d", stats.Total)
				}
			})

			t.Run("category.list + channel.list read as empty", func(t *testing.T) {
				var cats []any
				trpcQuery(t, ts, "category.list", nil, &cats)
				if len(cats) != 0 {
					t.Fatalf("category.list: want empty, got %d", len(cats))
				}
				var chans []any
				trpcQuery(t, ts, "channel.list", nil, &chans)
				if len(chans) != 0 {
					t.Fatalf("channel.list: want empty, got %d", len(chans))
				}
			})

			t.Run("video.list with pagination args", func(t *testing.T) {
				var videos []any
				trpcQuery(t, ts, "video.list",
					map[string]any{"limit": 50, "offset": 0}, &videos)
				if len(videos) != 0 {
					t.Fatalf("video.list: want empty, got %d", len(videos))
				}
			})

			t.Run("task.list returns registered tasks", func(t *testing.T) {
				var out struct {
					Data []struct {
						Name string `json:"name"`
					} `json:"data"`
				}
				trpcQuery(t, ts, "task.list", nil, &out)
				// The scheduler registers tasks on boot; e2e harness
				// doesn't boot the scheduler, so the repo should be empty.
				// What we're really asserting is the pagination shape
				// serializes correctly across both drivers.
				if out.Data == nil {
					t.Fatalf("task.list: expected non-nil data array")
				}
			})

			t.Run("system role + whitelist round-trips", func(t *testing.T) {
				ctx := context.Background()
				if _, err := ts.repo.UpsertUser(ctx, &repository.User{
					ID: "viewer-1", Login: "viewer1", DisplayName: "Viewer One", Role: "viewer",
				}); err != nil {
					t.Fatalf("seed viewer: %v", err)
				}

				var promoted smokeUser
				trpcMutation(t, ts, "system.updateUserRole",
					map[string]any{"user_id": "viewer-1", "role": "admin"}, &promoted)
				if promoted.ID != "viewer-1" || promoted.Role != "admin" {
					t.Fatalf("updateUserRole returned id=%q role=%q, want viewer-1/admin", promoted.ID, promoted.Role)
				}
				var users []smokeUser
				trpcQuery(t, ts, "system.listUsers", nil, &users)
				if !roleOf(users, "viewer-1", "admin") {
					t.Fatalf("listUsers does not show viewer-1 as admin: %+v", users)
				}

				trpcMutation(t, ts, "system.addWhitelist", map[string]any{"twitch_user_id": "wl-1"}, nil)
				var wl []smokeWhitelistEntry
				trpcQuery(t, ts, "system.listWhitelist", nil, &wl)
				if !whitelisted(wl, "wl-1") {
					t.Fatalf("wl-1 not present after addWhitelist: %+v", wl)
				}
				trpcMutation(t, ts, "system.removeWhitelist", map[string]any{"twitch_user_id": "wl-1"}, nil)
				trpcQuery(t, ts, "system.listWhitelist", nil, &wl)
				if whitelisted(wl, "wl-1") {
					t.Fatalf("wl-1 still present after removeWhitelist: %+v", wl)
				}

				// Owners cannot strip their own owner role.
				status, body := rawRequest(t, ts, http.MethodPost, "system.updateUserRole",
					map[string]any{"user_id": ts.userID, "role": "viewer"}, ts.sessionID)
				if status != http.StatusBadRequest {
					t.Fatalf("self-demotion status = %d, want 400 (ErrCannotDemoteSelf); body=%s", status, body)
				}
				trpcQuery(t, ts, "system.listUsers", nil, &users)
				if !roleOf(users, ts.userID, "owner") {
					t.Fatalf("owner role changed despite self-demotion guard: %+v", users)
				}

				// A viewer is rejected from owner-only routes.
				if _, err := ts.repo.UpsertUser(ctx, &repository.User{
					ID: "viewer-2", Login: "viewer2", DisplayName: "Viewer Two", Role: "viewer",
				}); err != nil {
					t.Fatalf("seed second viewer: %v", err)
				}
				viewerCookie := seedSession(t, ts.repo, ts.sessionMgr, "viewer-2")
				if status, body := rawRequest(t, ts, http.MethodGet, "system.listUsers", nil, viewerCookie); status != http.StatusForbidden {
					t.Fatalf("viewer hitting system.listUsers = %d, want 403; body=%s", status, body)
				}
			})
		})
	}
}

type smokeUser struct {
	ID   string `json:"id"`
	Role string `json:"role"`
}

type smokeWhitelistEntry struct {
	TwitchUserID string `json:"twitch_user_id"`
}

// roleOf reports whether the user list contains id with the expected role.
func roleOf(users []smokeUser, id, role string) bool {
	for _, u := range users {
		if u.ID == id {
			return u.Role == role
		}
	}
	return false
}

// whitelisted reports whether the whitelist contains the given twitch id.
func whitelisted(wl []smokeWhitelistEntry, id string) bool {
	for _, e := range wl {
		if e.TwitchUserID == id {
			return true
		}
	}
	return false
}
