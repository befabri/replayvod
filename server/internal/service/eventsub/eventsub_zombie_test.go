package eventsub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"

	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

func TestReconcile_DeadSubWhoseDeleteFailsDefersOnlyItsReplacement(t *testing.T) {
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
		// Reconciliation calls this transport concurrently.
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
	if posted["stream.online"] != 0 || posted["stream.offline"] != 1 {
		t.Fatalf("creates = %v, want only the missing stream.offline", posted)
	}
	if sub, err := repo.GetActiveSubscriptionForBroadcasterType(ctx, "b-dead", "stream.online"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("dead mirror still reported active: %+v, %v", sub, err)
	}
	dead, err := repo.GetSubscription(ctx, "on-dead")
	if err != nil {
		t.Fatalf("dead mirror: %v", err)
	}
	if dead.Status != "revoked" {
		t.Fatalf("dead mirror status = %q, want revoked", dead.Status)
	}
}

func TestZombieDeleteFailuresDoNotStarveHealthySubscriptionsOrOrphanCleanup(t *testing.T) {
	ctx := t.Context()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	const cb = "https://replayvod.example/api/v1/webhook/callback"
	channels := map[string]bool{}
	var dead []string
	for i := range 6 {
		id := fmt.Sprintf("channel-%d", i)
		channels[id] = true
		seedSubscriptionChannel(t, ctx, repo, id)
		createTestSubscriptionWithTypeCallback(t, ctx, repo, "dead-"+id, id, "stream.online", cb)
		dead = append(dead, eventSubSubJSONWithStatus("dead-"+id, "stream.online", "1", id, cb, "notification_failures_exceeded"))
	}
	seedSubscriptionChannel(t, ctx, repo, "orphan")
	createTestSubscriptionWithTypeCallback(t, ctx, repo, "orphan", "orphan", "stream.offline", cb)
	var mu sync.Mutex
	created, duplicates, orphanDeletes := 0, 0, 0
	tc := twitch.NewClient("cid", "csec", slog.New(slog.DiscardHandler))
	tc.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case req.Host == "id.twitch.tv":
			return textResponse(200, `{"access_token":"t","expires_in":3600,"token_type":"bearer"}`), nil
		case req.Method == http.MethodGet:
			if req.URL.Query().Get("type") == "stream.online" {
				return textResponse(200, eventSubListOf(dead...)), nil
			}
			return textResponse(200, eventSubListOf(eventSubSubJSONWithStatus("orphan", "stream.offline", "1", "orphan", cb, "enabled"))), nil
		case req.Method == http.MethodDelete:
			if req.URL.Query().Get("id") == "orphan" {
				orphanDeletes++
				return textResponse(204, ""), nil
			}
			return textResponse(400, `{"status":400,"message":"delete failed"}`), nil
		case req.Method == http.MethodPost:
			var body struct {
				Type      string `json:"type"`
				Condition struct {
					BroadcasterID string `json:"broadcaster_user_id"`
				} `json:"condition"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				return nil, err
			}
			if body.Type == "stream.online" {
				duplicates++
				return textResponse(409, `{"status":409,"message":"subscription exists"}`), nil
			}
			created++
			return textResponse(202, eventSubCreateResponse("offline-"+body.Condition.BroadcasterID, body.Type, "1", body.Condition.BroadcasterID, cb)), nil
		}
		return nil, fmt.Errorf("unexpected %s %s", req.Method, req.URL)
	})})
	svc := New(repo, tc, cb, "0123456789abcdef", slog.New(slog.DiscardHandler))
	if err := svc.ReconcileChannelSubs(ctx, channels); err != nil {
		t.Fatal(err)
	}
	if created != 6 || duplicates != 0 || orphanDeletes != 1 {
		t.Fatalf("created=%d duplicate POSTs=%d orphan deletes=%d", created, duplicates, orphanDeletes)
	}
	for id := range channels {
		if _, err := repo.GetActiveSubscriptionForBroadcasterType(ctx, id, "stream.offline"); err != nil {
			t.Fatalf("healthy subscription %s missing: %v", id, err)
		}
	}
}

func TestReconcileReplacesActiveMirrorMissingOnTwitch(t *testing.T) {
	for _, subType := range []string{"stream.online", "channel.update"} {
		for _, status := range []string{"enabled", "notification_failures_exceeded"} {
			t.Run(subType+"/"+status, func(t *testing.T) {
				testReconcileReplacesMissingMirror(t, subType, status)
			})
		}
	}
}

func testReconcileReplacesMissingMirror(t *testing.T, subType, status string) {
	t.Helper()
	ctx := t.Context()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	const cb = "https://replayvod.example/api/v1/webhook/callback"
	seedSubscriptionChannel(t, ctx, repo, "missing")
	createTestSubscriptionWithTypeCallback(t, ctx, repo, "stale", "missing", subType, cb)
	if err := repo.UpdateSubscriptionStatus(ctx, "stale", status); err != nil {
		t.Fatal(err)
	}
	tc := twitch.NewClient("cid", "csec", slog.New(slog.DiscardHandler))
	tc.SetHTTPClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Host == "id.twitch.tv" {
			return textResponse(200, `{"access_token":"t","expires_in":3600,"token_type":"bearer"}`), nil
		}
		if req.Method == http.MethodGet {
			return textResponse(200, eventSubListOf()), nil
		}
		if req.Method == http.MethodPost {
			var body struct {
				Type string `json:"type"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				return nil, err
			}
			return textResponse(202, eventSubCreateResponse("new-"+body.Type, body.Type, "1", "missing", cb)), nil
		}
		return nil, fmt.Errorf("unexpected %s", req.Method)
	})})
	svc := New(repo, tc, cb, "0123456789abcdef", slog.New(slog.DiscardHandler))
	reconcile := svc.ReconcileChannelSubs
	if subType == "channel.update" {
		reconcile = svc.ReconcileChannelUpdateSubs
	}
	if err := reconcile(ctx, map[string]bool{"missing": true}); err != nil {
		t.Fatal(err)
	}
	row, err := repo.GetActiveSubscriptionForBroadcasterType(ctx, "missing", subType)
	if err != nil || row.ID != "new-"+subType {
		t.Fatalf("missing remote sub not repaired: %+v, %v", row, err)
	}
	stale, err := repo.GetSubscription(ctx, "stale")
	if err != nil || stale.Status != "revoked" {
		t.Fatalf("absent mirror remained active: %+v, %v", stale, err)
	}
}
