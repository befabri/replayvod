package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/service/storagehealth"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestViewerStorageSubscriptionContainsOnlyStateAndTimestamp(t *testing.T) {
	h := newPermissionHarness(t)
	server := httptest.NewServer(h.router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/trpc/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {server.URL}, "Cookie": {h.viewer.String()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if err := wsjson.Write(ctx, conn, map[string]any{"id": 1, "method": "subscription", "params": map[string]any{"path": "storage.statusLive"}}); err != nil {
		t.Fatal(err)
	}
	var msg struct {
		Result struct {
			Type string         `json:"type"`
			Data map[string]any `json:"data"`
		} `json:"result"`
	}
	if err := wsjson.Read(ctx, conn, &msg); err != nil || msg.Result.Type != "started" {
		t.Fatalf("subscription did not start: %+v %v", msg, err)
	}
	id, err := storage.NewStorageID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.SetStorageID(ctx, id); err != nil {
		t.Fatal(err)
	}
	privatePath := filepath.Join(t.TempDir(), "owner-only-storage-location")
	local, err := storage.NewLocal(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	mon := storagehealth.New(h.repo, local, h.bus, slog.New(slog.DiscardHandler), "local", privatePath)
	status, err := mon.Attach(ctx)
	if !errors.Is(err, storage.ErrUnreachable) || !strings.Contains(status.Reason, privatePath) {
		t.Fatalf("fixture lacks private diagnostic: %+v %v", status, err)
	}
	if err := wsjson.Read(ctx, conn, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Result.Type != "data" || len(msg.Result.Data) != 2 || msg.Result.Data["state"] != "unreachable" {
		t.Fatalf("viewer event must contain only state and timestamp: %+v", msg)
	}
	at, ok := msg.Result.Data["at"].(string)
	if !ok {
		t.Fatalf("missing timestamp: %+v", msg)
	}
	if got, err := time.Parse(time.RFC3339Nano, at); err != nil || !got.Equal(status.CheckedAt) {
		t.Fatalf("event timestamp=%q status=%s error=%v", at, status.CheckedAt, err)
	}
	if got := h.do(http.MethodGet, "/trpc/storage.details", "", h.viewer); got != http.StatusForbidden {
		t.Fatalf("viewer accessed diagnostics query: %d", got)
	}
}

func TestWebSocketSubscriptionPermissions(t *testing.T) {
	h := newPermissionHarness(t, "http://localhost:3000")
	server := httptest.NewServer(h.router)
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/trpc/ws"
	for _, tc := range []struct {
		name, origin string
		cookie       *http.Cookie
		handshake    int
		owner        bool
	}{
		{"anonymous", server.URL, nil, 401, false},
		{"foreign origin", "https://evil.example", h.owner, 403, false},
		{"viewer", server.URL, h.viewer, 101, false},
		{"admin", server.URL, h.admin, 101, false},
		{"owner", server.URL, h.owner, 101, true},
		{"configured frontend", "http://localhost:3000", h.owner, 101, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			headers := http.Header{"Origin": {tc.origin}}
			if tc.cookie != nil {
				headers.Set("Cookie", tc.cookie.String())
			}
			conn, response, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPHeader: headers})
			if conn != nil {
				defer conn.CloseNow()
			}
			if response == nil || response.StatusCode != tc.handshake {
				t.Fatalf("handshake response=%v error=%v", response, err)
			}
			if tc.handshake != 101 {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for id, path := range []string{"storage.statusLive", "video.removalsLive", "task.status"} {
				if err := wsjson.Write(ctx, conn, map[string]any{"id": id + 1, "method": "subscription", "params": map[string]any{"path": path}}); err != nil {
					t.Fatal(err)
				}
				var msg map[string]any
				if err := wsjson.Read(ctx, conn, &msg); err != nil {
					t.Fatal(err)
				}
				if path == "task.status" && !tc.owner {
					if msg["error"].(map[string]any)["data"].(map[string]any)["code"] != "FORBIDDEN" {
						t.Fatalf("owner feed allowed: %v", msg)
					}
				} else if msg["result"].(map[string]any)["type"] != "started" {
					t.Fatalf("subscription did not start: %v", msg)
				}
			}
		})
	}
}

func TestWebSocketSubscriptionsRecheckRolesAndRevokedSessions(t *testing.T) {
	h := newPermissionHarness(t)
	server := httptest.NewServer(h.router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/trpc/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {server.URL}, "Cookie": {h.owner.String()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	check := func(id int, method, path, wantResult, wantError string) {
		t.Helper()
		if err := wsjson.Write(ctx, conn, map[string]any{"id": id, "method": method, "params": map[string]any{"path": path}}); err != nil {
			t.Fatal(err)
		}
		var msg struct {
			ID     int `json:"id"`
			Result struct {
				Type string `json:"type"`
			} `json:"result"`
			Error struct {
				Data struct {
					Code string `json:"code"`
				} `json:"data"`
			} `json:"error"`
		}
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			t.Fatal(err)
		}
		if msg.ID != id || msg.Result.Type != wantResult || msg.Error.Data.Code != wantError {
			t.Fatalf("subscription response=%+v, want id=%d result=%q error=%q", msg, id, wantResult, wantError)
		}
	}
	check(1, "subscription", "task.status", "started", "")
	check(1, "subscription.stop", "", "stopped", "")

	// The upgrade request still contains the original owner context. Every new
	// procedure must replace it with the current user and session from storage.
	if err := h.repo.UpdateUserRole(ctx, "perm-owner-1", "viewer"); err != nil {
		t.Fatal(err)
	}
	check(2, "subscription", "task.status", "", "FORBIDDEN")
	if got := h.do(http.MethodGet, "/trpc/task.status", "", h.owner); got != http.StatusForbidden {
		t.Fatalf("fresh owner subscription after demotion = %d", got)
	}
	check(3, "subscription", "storage.statusLive", "started", "")
	check(3, "subscription.stop", "", "stopped", "")
	if err := h.repo.DeleteUserSessions(ctx, "perm-owner-1"); err != nil {
		t.Fatal(err)
	}
	check(4, "subscription", "storage.statusLive", "", "UNAUTHORIZED")
	check(5, "subscription", "task.status", "", "UNAUTHORIZED")
	if got := h.do(http.MethodGet, "/trpc/storage.statusLive", "", h.owner); got != http.StatusUnauthorized {
		t.Fatalf("fresh viewer subscription after revocation = %d", got)
	}
}
