package eventsub

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// TestReconcile_DeadSubWhoseDeleteFailsIsStillReplaced pins that a dead
// subscription on the current callback does not keep live detection dark
// because Twitch refuses to delete it: the local mirror is retired and a
// replacement is created in the same reconcile.
func TestReconcile_DeadSubWhoseDeleteFailsIsStillReplaced(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	const cb = "https://replayvod.example/api/v1/webhook/callback"
	seedSubscriptionChannel(t, ctx, repo, "b-dead")
	createTestSubscriptionWithTypeCallback(t, ctx, repo, "on-dead", "b-dead", "stream.online", cb)

	posted := map[string]int{}
	deletes := 0
	var mu sync.Mutex
	tc := twitch.NewClient("cid", "csec", slog.New(slog.NewTextHandler(io.Discard, nil)))
	tc.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// Reconciliation creates online and offline subscriptions in parallel.
		mu.Lock()
		defer mu.Unlock()
		switch {
		case req.Host == "id.twitch.tv":
			return textResponse(http.StatusOK, `{"access_token":"t","expires_in":3600,"token_type":"bearer"}`), nil
		case req.Method == http.MethodGet:
			if req.URL.Query().Get("type") == "stream.online" {
				return textResponse(http.StatusOK, eventSubListOf(eventSubSubJSONWithStatus("on-dead", "stream.online", "1", "b-dead", cb, "notification_failures_exceeded"))), nil
			}
			return textResponse(http.StatusOK, eventSubListOf()), nil
		case req.Method == http.MethodDelete:
			deletes++
			return textResponse(http.StatusBadRequest, `{"error":"Bad Request","status":400,"message":"boom"}`), nil
		case req.Method == http.MethodPost:
			var body struct {
				Type string `json:"type"`
			}
			_ = json.NewDecoder(req.Body).Decode(&body)
			posted[body.Type]++
			return textResponse(http.StatusAccepted, eventSubCreateResponse("new-"+body.Type, body.Type, "1", "b-dead", cb)), nil
		}
		t.Fatalf("unexpected %s %s", req.Method, req.URL)
		return nil, nil
	})})
	svc := New(repo, tc, cb, "0123456789abcdef", slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := svc.ReconcileChannelSubs(ctx, map[string]bool{"b-dead": true}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if deletes != 1 {
		t.Fatalf("twitch deletes = %d, want the dead sub deleted once", deletes)
	}
	if posted["stream.online"] != 1 || posted["stream.offline"] != 1 {
		t.Fatalf("creates = %v, want one stream.online replacement and the missing stream.offline", posted)
	}
	sub, err := repo.GetActiveSubscriptionForBroadcasterType(ctx, "b-dead", "stream.online")
	if err != nil {
		t.Fatalf("active stream.online mirror: %v", err)
	}
	if sub.ID != "new-stream.online" {
		t.Fatalf("active mirror = %s, want the replacement", sub.ID)
	}
	dead, err := repo.GetSubscription(ctx, "on-dead")
	if err != nil {
		t.Fatalf("dead mirror: %v", err)
	}
	if dead.Status != "revoked" {
		t.Fatalf("dead mirror status = %q, want revoked", dead.Status)
	}
}
