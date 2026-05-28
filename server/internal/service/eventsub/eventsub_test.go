package eventsub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRevokeAllActiveDeletesOnTwitchAndMarksLocalRows(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))

	seedSubscriptionChannel(t, ctx, repo, "b-revoke-1")
	seedSubscriptionChannel(t, ctx, repo, "b-revoke-2")
	createTestSubscription(t, ctx, repo, "sub-revoke-1", "b-revoke-1")
	createTestSubscription(t, ctx, repo, "sub-revoke-2", "b-revoke-2")

	deleted := map[string]bool{}
	tc := twitch.NewClient("client-id", "client-secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	tc.SetHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Host == "id.twitch.tv" && req.URL.Path == "/oauth2/token":
				return textResponse(http.StatusOK, `{"access_token":"app-token","expires_in":3600,"token_type":"bearer"}`), nil
			case req.Host == "api.twitch.tv" && req.Method == http.MethodDelete && req.URL.Path == "/helix/eventsub/subscriptions":
				deleted[req.URL.Query().Get("id")] = true
				return textResponse(http.StatusNoContent, ""), nil
			default:
				t.Fatalf("unexpected Twitch request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		}),
	})
	svc := New(repo, tc, "https://replayvod.example/api/v1/webhook/callback", "secret", slog.New(slog.NewTextHandler(io.Discard, nil)))

	revoked, err := svc.RevokeAllActive(ctx, "delivery disabled")
	if err != nil {
		t.Fatalf("RevokeAllActive() error = %v, want nil", err)
	}
	if revoked != 2 {
		t.Fatalf("RevokeAllActive() revoked = %d, want 2", revoked)
	}
	for _, id := range []string{"sub-revoke-1", "sub-revoke-2"} {
		if !deleted[id] {
			t.Fatalf("Twitch DELETE was not called for %s", id)
		}
		sub, err := repo.GetSubscription(ctx, id)
		if err != nil {
			t.Fatalf("GetSubscription(%s): %v", id, err)
		}
		if sub.RevokedAt == nil {
			t.Fatalf("%s RevokedAt = nil, want set", id)
		}
		if sub.RevokedReason == nil || *sub.RevokedReason != "delivery disabled" {
			t.Fatalf("%s RevokedReason = %v, want delivery disabled", id, sub.RevokedReason)
		}
	}
	active, err := repo.ListActiveSubscriptions(ctx, 100, 0)
	if err != nil {
		t.Fatalf("ListActiveSubscriptions: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active subscriptions after revoke = %d, want 0", len(active))
	}
}

func TestRevokeAllActiveSnapshotsTiedCreatedAtAcrossPages(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)

	const total = 105
	for i := 1; i <= total; i++ {
		broadcasterID := fmt.Sprintf("b-revoke-page-%03d", i)
		seedSubscriptionChannel(t, ctx, repo, broadcasterID)
		createTestSubscription(t, ctx, repo, fmt.Sprintf("sub-revoke-page-%03d", i), broadcasterID)
	}
	if _, err := db.ExecContext(ctx, `UPDATE subscriptions SET created_at = ?`, "2026-01-01 00:00:00"); err != nil {
		t.Fatalf("force tied created_at: %v", err)
	}

	deleted := map[string]bool{}
	tc := twitch.NewClient("client-id", "client-secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	tc.SetHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Host == "id.twitch.tv" && req.URL.Path == "/oauth2/token":
				return textResponse(http.StatusOK, `{"access_token":"app-token","expires_in":3600,"token_type":"bearer"}`), nil
			case req.Host == "api.twitch.tv" && req.Method == http.MethodDelete && req.URL.Path == "/helix/eventsub/subscriptions":
				deleted[req.URL.Query().Get("id")] = true
				return textResponse(http.StatusNoContent, ""), nil
			default:
				t.Fatalf("unexpected Twitch request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		}),
	})
	svc := New(repo, tc, "https://replayvod.example/api/v1/webhook/callback", "secret", slog.New(slog.NewTextHandler(io.Discard, nil)))

	revoked, err := svc.RevokeAllActive(ctx, "delivery disabled")
	if err != nil {
		t.Fatalf("RevokeAllActive() error = %v, want nil", err)
	}
	if revoked != total {
		t.Fatalf("RevokeAllActive() revoked = %d, want %d", revoked, total)
	}
	if len(deleted) != total {
		t.Fatalf("Twitch DELETE calls = %d, want %d", len(deleted), total)
	}
	active, err := repo.ListActiveSubscriptions(ctx, total, 0)
	if err != nil {
		t.Fatalf("ListActiveSubscriptions: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active subscriptions after revoke = %d, want 0", len(active))
	}
}

// TestRevokeAllActiveDrainsHealthySubsDespiteOnePoisonSub pins that a single
// subscription whose Twitch DELETE keeps failing does not stall cleanup of the
// others: every healthy sub is still revoked, the failure is reported, and the
// poison sub stays active for a later retry/reconcile instead of being marked
// revoked locally while still live on Twitch.
func TestRevokeAllActiveDrainsHealthySubsDespiteOnePoisonSub(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))

	for _, id := range []string{"sub-ok-1", "sub-poison", "sub-ok-2"} {
		broadcasterID := "b-" + id
		seedSubscriptionChannel(t, ctx, repo, broadcasterID)
		createTestSubscription(t, ctx, repo, id, broadcasterID)
	}

	deleted := map[string]bool{}
	tc := twitch.NewClient("client-id", "client-secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	tc.SetHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Host == "id.twitch.tv" && req.URL.Path == "/oauth2/token":
				return textResponse(http.StatusOK, `{"access_token":"app-token","expires_in":3600,"token_type":"bearer"}`), nil
			case req.Host == "api.twitch.tv" && req.Method == http.MethodDelete && req.URL.Path == "/helix/eventsub/subscriptions":
				id := req.URL.Query().Get("id")
				if id == "sub-poison" {
					// 400 (not 404) so Unsubscribe surfaces the error rather
					// than treating it as already-gone.
					return textResponse(http.StatusBadRequest, `{"error":"Bad Request","status":400,"message":"boom"}`), nil
				}
				deleted[id] = true
				return textResponse(http.StatusNoContent, ""), nil
			default:
				t.Fatalf("unexpected Twitch request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		}),
	})
	svc := New(repo, tc, "https://replayvod.example/api/v1/webhook/callback", "secret", slog.New(slog.NewTextHandler(io.Discard, nil)))

	revoked, err := svc.RevokeAllActive(ctx, "delivery disabled")
	if err == nil {
		t.Fatal("RevokeAllActive() error = nil, want error reporting the poison sub")
	}
	if revoked != 2 {
		t.Fatalf("RevokeAllActive() revoked = %d, want 2 (healthy subs drained past the poison sub)", revoked)
	}
	for _, id := range []string{"sub-ok-1", "sub-ok-2"} {
		if !deleted[id] {
			t.Fatalf("healthy sub %s was not deleted on Twitch", id)
		}
		sub, err := repo.GetSubscription(ctx, id)
		if err != nil {
			t.Fatalf("GetSubscription(%s): %v", id, err)
		}
		if sub.RevokedAt == nil {
			t.Fatalf("%s RevokedAt = nil, want set", id)
		}
	}
	poison, err := repo.GetSubscription(ctx, "sub-poison")
	if err != nil {
		t.Fatalf("GetSubscription(sub-poison): %v", err)
	}
	if poison.RevokedAt != nil {
		t.Fatal("sub-poison RevokedAt = set, want still active after a failed Twitch delete")
	}
}

func TestSubscribeReplacesActiveLocalSubWhenCallbackChanged(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	const (
		broadcasterID = "b-callback-local"
		oldCallback   = "https://old.example/api/v1/webhook/callback"
		newCallback   = "https://replayvod.example/api/v1/webhook/callback"
	)
	seedSubscriptionChannel(t, ctx, repo, broadcasterID)
	createTestSubscriptionWithTypeCallback(t, ctx, repo, "old-local-sub", broadcasterID, "stream.online", oldCallback)

	var deletedOld bool
	var createdCallback string
	tc := twitch.NewClient("client-id", "client-secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	tc.SetHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Host == "id.twitch.tv" && req.URL.Path == "/oauth2/token":
				return textResponse(http.StatusOK, `{"access_token":"app-token","expires_in":3600,"token_type":"bearer"}`), nil
			case req.Host == "api.twitch.tv" && req.Method == http.MethodDelete && req.URL.Path == "/helix/eventsub/subscriptions":
				deletedOld = req.URL.Query().Get("id") == "old-local-sub"
				return textResponse(http.StatusNoContent, ""), nil
			case req.Host == "api.twitch.tv" && req.Method == http.MethodPost && req.URL.Path == "/helix/eventsub/subscriptions":
				var body struct {
					Type      string `json:"type"`
					Version   string `json:"version"`
					Transport struct {
						Callback string `json:"callback"`
						Secret   string `json:"secret"`
					} `json:"transport"`
				}
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Fatalf("decode create body: %v", err)
				}
				createdCallback = body.Transport.Callback
				if body.Transport.Secret != "0123456789abcdef" {
					t.Fatalf("create secret = %q, want configured secret", body.Transport.Secret)
				}
				return textResponse(http.StatusAccepted, eventSubCreateResponse("new-local-sub", body.Type, body.Version, broadcasterID, body.Transport.Callback)), nil
			default:
				t.Fatalf("unexpected Twitch request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		}),
	})
	svc := New(repo, tc, newCallback, "0123456789abcdef", slog.New(slog.NewTextHandler(io.Discard, nil)))

	sub, err := svc.SubscribeStreamOnline(ctx, broadcasterID)
	if err != nil {
		t.Fatalf("SubscribeStreamOnline() error = %v, want nil", err)
	}
	if sub.ID != "new-local-sub" {
		t.Fatalf("SubscribeStreamOnline() ID = %q, want new-local-sub", sub.ID)
	}
	if !deletedOld {
		t.Fatal("old callback subscription was not deleted before create")
	}
	if createdCallback != newCallback {
		t.Fatalf("created callback = %q, want %q", createdCallback, newCallback)
	}
	old, err := repo.GetSubscription(ctx, "old-local-sub")
	if err != nil {
		t.Fatalf("GetSubscription(old-local-sub): %v", err)
	}
	if old.RevokedAt == nil {
		t.Fatal("old-local-sub RevokedAt = nil, want revoked")
	}
}

func TestReconcileChannelSubsReplacesStaleCallbackSubscriptions(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	const (
		broadcasterID = "b-callback-reconcile"
		oldCallback   = "https://old.example/api/v1/webhook/callback"
		newCallback   = "https://replayvod.example/api/v1/webhook/callback"
	)
	seedSubscriptionChannel(t, ctx, repo, broadcasterID)
	createTestSubscriptionWithTypeCallback(t, ctx, repo, "old-online", broadcasterID, "stream.online", oldCallback)
	createTestSubscriptionWithTypeCallback(t, ctx, repo, "old-offline", broadcasterID, "stream.offline", oldCallback)

	var mu sync.Mutex
	deleted := map[string]bool{}
	created := map[string]string{}
	tc := twitch.NewClient("client-id", "client-secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	tc.SetHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Host == "id.twitch.tv" && req.URL.Path == "/oauth2/token":
				return textResponse(http.StatusOK, `{"access_token":"app-token","expires_in":3600,"token_type":"bearer"}`), nil
			case req.Host == "api.twitch.tv" && req.Method == http.MethodGet && req.URL.Path == "/helix/eventsub/subscriptions":
				switch req.URL.Query().Get("type") {
				case "stream.online":
					return textResponse(http.StatusOK, eventSubListResponse("old-online", "stream.online", "1", broadcasterID, oldCallback)), nil
				case "stream.offline":
					return textResponse(http.StatusOK, eventSubListResponse("old-offline", "stream.offline", "1", broadcasterID, oldCallback)), nil
				default:
					t.Fatalf("unexpected EventSub list type: %q", req.URL.Query().Get("type"))
					return nil, nil
				}
			case req.Host == "api.twitch.tv" && req.Method == http.MethodDelete && req.URL.Path == "/helix/eventsub/subscriptions":
				mu.Lock()
				deleted[req.URL.Query().Get("id")] = true
				mu.Unlock()
				return textResponse(http.StatusNoContent, ""), nil
			case req.Host == "api.twitch.tv" && req.Method == http.MethodPost && req.URL.Path == "/helix/eventsub/subscriptions":
				var body struct {
					Type      string `json:"type"`
					Version   string `json:"version"`
					Transport struct {
						Callback string `json:"callback"`
					} `json:"transport"`
				}
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Fatalf("decode create body: %v", err)
				}
				id := "new-" + strings.ReplaceAll(body.Type, ".", "-")
				mu.Lock()
				created[body.Type] = body.Transport.Callback
				mu.Unlock()
				return textResponse(http.StatusAccepted, eventSubCreateResponse(id, body.Type, body.Version, broadcasterID, body.Transport.Callback)), nil
			default:
				t.Fatalf("unexpected Twitch request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		}),
	})
	svc := New(repo, tc, newCallback, "0123456789abcdef", slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := svc.ReconcileChannelSubs(ctx, map[string]bool{broadcasterID: true}); err != nil {
		t.Fatalf("ReconcileChannelSubs() error = %v, want nil", err)
	}
	for _, id := range []string{"old-online", "old-offline"} {
		if !deleted[id] {
			t.Fatalf("stale subscription %s was not deleted", id)
		}
		sub, err := repo.GetSubscription(ctx, id)
		if err != nil {
			t.Fatalf("GetSubscription(%s): %v", id, err)
		}
		if sub.RevokedAt == nil {
			t.Fatalf("%s RevokedAt = nil, want revoked", id)
		}
	}
	for _, typ := range []string{"stream.online", "stream.offline"} {
		if created[typ] != newCallback {
			t.Fatalf("created[%s] callback = %q, want %q", typ, created[typ], newCallback)
		}
		sub, err := repo.GetActiveSubscriptionForBroadcasterType(ctx, broadcasterID, typ)
		if err != nil {
			t.Fatalf("GetActiveSubscriptionForBroadcasterType(%s): %v", typ, err)
		}
		if sub.TransportCallback != newCallback {
			t.Fatalf("%s active callback = %q, want %q", typ, sub.TransportCallback, newCallback)
		}
	}
}

// channelUpdateReconcileTwitch builds a Twitch client whose GET channel.update
// list returns listJSON, recording DELETE ids and POST creates (keyed by the
// condition's broadcaster). deleteStatus lets a test force a DELETE failure for
// specific sub IDs.
func channelUpdateReconcileTwitch(t *testing.T, listJSON string, deleteStatus map[string]int) (*twitch.Client, *reconcileCalls) {
	t.Helper()
	calls := &reconcileCalls{deleted: map[string]bool{}, created: map[string]string{}}
	tc := twitch.NewClient("client-id", "client-secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	tc.SetHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Host == "id.twitch.tv" && req.URL.Path == "/oauth2/token":
				return textResponse(http.StatusOK, `{"access_token":"app-token","expires_in":3600,"token_type":"bearer"}`), nil
			case req.Host == "api.twitch.tv" && req.Method == http.MethodGet && req.URL.Path == "/helix/eventsub/subscriptions":
				if got := req.URL.Query().Get("type"); got != "channel.update" {
					t.Fatalf("unexpected list type %q, want channel.update", got)
				}
				return textResponse(http.StatusOK, listJSON), nil
			case req.Host == "api.twitch.tv" && req.Method == http.MethodDelete && req.URL.Path == "/helix/eventsub/subscriptions":
				id := req.URL.Query().Get("id")
				if status := deleteStatus[id]; status != 0 {
					return textResponse(status, `{"error":"Bad Request","status":400,"message":"boom"}`), nil
				}
				calls.mu.Lock()
				calls.deleted[id] = true
				calls.mu.Unlock()
				return textResponse(http.StatusNoContent, ""), nil
			case req.Host == "api.twitch.tv" && req.Method == http.MethodPost && req.URL.Path == "/helix/eventsub/subscriptions":
				var body struct {
					Type      string `json:"type"`
					Version   string `json:"version"`
					Condition struct {
						BroadcasterUserID string `json:"broadcaster_user_id"`
					} `json:"condition"`
					Transport struct {
						Callback string `json:"callback"`
					} `json:"transport"`
				}
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Fatalf("decode create body: %v", err)
				}
				bid := body.Condition.BroadcasterUserID
				calls.mu.Lock()
				calls.created[bid] = body.Transport.Callback
				calls.mu.Unlock()
				return textResponse(http.StatusAccepted, eventSubCreateResponse("new-cu-"+bid, body.Type, body.Version, bid, body.Transport.Callback)), nil
			default:
				t.Fatalf("unexpected Twitch request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		}),
	})
	return tc, calls
}

type reconcileCalls struct {
	mu      sync.Mutex
	deleted map[string]bool
	created map[string]string
}

func (c *reconcileCalls) deletedID(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deleted[id]
}

func (c *reconcileCalls) createdFor(bid string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cb, ok := c.created[bid]
	return cb, ok
}

// TestReconcileChannelUpdateSubs exercises all four branches of the
// channel.update reconcile in one pass: a zombie sub and a stale-callback sub are
// deleted, an alive-correct sub for a broadcaster with no active recording is
// deleted as an orphan, the alive-correct sub for a broadcaster that still has an
// active recording is kept, and a broadcaster with an active recording but no sub
// gets one created.
func TestReconcileChannelUpdateSubs(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	const (
		newCallback = "https://replayvod.example/api/v1/webhook/callback"
		oldCallback = "https://old.example/api/v1/webhook/callback"
	)

	type listed struct {
		id, bid, callback, status string
	}
	subs := []listed{
		{id: "cu-zombie", bid: "b-zombie", callback: newCallback, status: "authorization_revoked"},
		{id: "cu-stale", bid: "b-stale", callback: oldCallback, status: "enabled"},
		{id: "cu-orphan", bid: "b-orphan", callback: newCallback, status: "enabled"},
		{id: "cu-keep", bid: "b-keep", callback: newCallback, status: "enabled"},
	}
	var subsJSON []string
	for _, s := range subs {
		seedSubscriptionChannel(t, ctx, repo, s.bid)
		createTestSubscriptionWithTypeCallback(t, ctx, repo, s.id, s.bid, "channel.update", s.callback)
		subsJSON = append(subsJSON, eventSubSubJSONWithStatus(s.id, "channel.update", "2", s.bid, s.callback, s.status))
	}
	seedSubscriptionChannel(t, ctx, repo, "b-missing")

	tc, calls := channelUpdateReconcileTwitch(t, eventSubListOf(subsJSON...), nil)
	svc := New(repo, tc, newCallback, "0123456789abcdef", slog.New(slog.NewTextHandler(io.Discard, nil)))

	active := map[string]bool{"b-keep": true, "b-missing": true}
	if err := svc.ReconcileChannelUpdateSubs(ctx, active); err != nil {
		t.Fatalf("ReconcileChannelUpdateSubs() = %v, want nil", err)
	}

	for _, id := range []string{"cu-zombie", "cu-stale", "cu-orphan"} {
		if !calls.deletedID(id) {
			t.Fatalf("%s was not deleted on Twitch", id)
		}
		sub, err := repo.GetSubscription(ctx, id)
		if err != nil {
			t.Fatalf("GetSubscription(%s): %v", id, err)
		}
		if sub.RevokedAt == nil {
			t.Fatalf("%s RevokedAt = nil, want revoked", id)
		}
	}
	if calls.deletedID("cu-keep") {
		t.Fatal("cu-keep was deleted; an alive, correct-callback sub for an active recording must be kept")
	}
	keep, err := repo.GetSubscription(ctx, "cu-keep")
	if err != nil {
		t.Fatalf("GetSubscription(cu-keep): %v", err)
	}
	if keep.RevokedAt != nil {
		t.Fatal("cu-keep RevokedAt = set, want still active")
	}
	if cb, ok := calls.createdFor("b-missing"); !ok || cb != newCallback {
		t.Fatalf("b-missing create callback = %q (ok=%v), want %q", cb, ok, newCallback)
	}
	if _, ok := calls.createdFor("b-keep"); ok {
		t.Fatal("b-keep was re-created; an already-current broadcaster must not be re-subscribed")
	}
	if sub, err := repo.GetActiveSubscriptionForBroadcasterType(ctx, "b-missing", "channel.update"); err != nil {
		t.Fatalf("b-missing should have an active channel.update sub: %v", err)
	} else if sub.TransportCallback != newCallback {
		t.Fatalf("b-missing sub callback = %q, want %q", sub.TransportCallback, newCallback)
	}
}

// TestReconcileChannelUpdateSubsBlockedZombieIsNotRecreated pins the blocked-map
// guard: when a zombie sub's delete fails for a broadcaster that DOES have an
// active recording, we must not turn around and create a replacement in the same
// pass (Twitch still has the dead sub, so a create would 409 or duplicate). The
// broadcaster is left for the next reconcile.
func TestReconcileChannelUpdateSubsBlockedZombieIsNotRecreated(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	const newCallback = "https://replayvod.example/api/v1/webhook/callback"

	seedSubscriptionChannel(t, ctx, repo, "b-x")
	createTestSubscriptionWithTypeCallback(t, ctx, repo, "cu-x", "b-x", "channel.update", newCallback)
	listJSON := eventSubListOf(eventSubSubJSONWithStatus("cu-x", "channel.update", "2", "b-x", newCallback, "authorization_revoked"))

	// DELETE cu-x fails with a 400 so it can't be treated as already-gone.
	tc, calls := channelUpdateReconcileTwitch(t, listJSON, map[string]int{"cu-x": http.StatusBadRequest})
	svc := New(repo, tc, newCallback, "0123456789abcdef", slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := svc.ReconcileChannelUpdateSubs(ctx, map[string]bool{"b-x": true}); err != nil {
		t.Fatalf("ReconcileChannelUpdateSubs() = %v, want nil (best-effort)", err)
	}
	if _, ok := calls.createdFor("b-x"); ok {
		t.Fatal("b-x was re-created after a failed zombie delete; the blocked guard must skip it this pass")
	}
}

// TestReconcileChannelSubsSweepsZombieAndCreatesReplacement pins that a
// terminal-status (zombie) stream.online sub is deleted and replaced with a fresh
// create, even though our local mirror still shows it active. Every other
// reconcile test hardcodes status "enabled", so this is the only coverage of the
// zombie sweep path.
func TestReconcileChannelSubsSweepsZombieAndCreatesReplacement(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	const newCallback = "https://replayvod.example/api/v1/webhook/callback"
	seedSubscriptionChannel(t, ctx, repo, "b-z")
	createTestSubscriptionWithTypeCallback(t, ctx, repo, "on-zombie", "b-z", "stream.online", newCallback)

	calls := &reconcileCalls{deleted: map[string]bool{}, created: map[string]string{}}
	tc := twitch.NewClient("client-id", "client-secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	tc.SetHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Host == "id.twitch.tv" && req.URL.Path == "/oauth2/token":
				return textResponse(http.StatusOK, `{"access_token":"app-token","expires_in":3600,"token_type":"bearer"}`), nil
			case req.Method == http.MethodGet && req.URL.Path == "/helix/eventsub/subscriptions":
				switch req.URL.Query().Get("type") {
				case "stream.online":
					return textResponse(http.StatusOK, eventSubListOf(eventSubSubJSONWithStatus("on-zombie", "stream.online", "1", "b-z", newCallback, "verification_failed"))), nil
				case "stream.offline":
					return textResponse(http.StatusOK, eventSubListOf()), nil
				default:
					t.Fatalf("unexpected list type %q", req.URL.Query().Get("type"))
					return nil, nil
				}
			case req.Method == http.MethodDelete && req.URL.Path == "/helix/eventsub/subscriptions":
				calls.mu.Lock()
				calls.deleted[req.URL.Query().Get("id")] = true
				calls.mu.Unlock()
				return textResponse(http.StatusNoContent, ""), nil
			case req.Method == http.MethodPost && req.URL.Path == "/helix/eventsub/subscriptions":
				var body struct {
					Type      string `json:"type"`
					Version   string `json:"version"`
					Condition struct {
						BroadcasterUserID string `json:"broadcaster_user_id"`
					} `json:"condition"`
					Transport struct {
						Callback string `json:"callback"`
					} `json:"transport"`
				}
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Fatalf("decode create body: %v", err)
				}
				calls.mu.Lock()
				calls.created[body.Type] = body.Transport.Callback
				calls.mu.Unlock()
				return textResponse(http.StatusAccepted, eventSubCreateResponse("new-"+body.Type, body.Type, body.Version, body.Condition.BroadcasterUserID, body.Transport.Callback)), nil
			default:
				t.Fatalf("unexpected Twitch request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		}),
	})
	svc := New(repo, tc, newCallback, "0123456789abcdef", slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := svc.ReconcileChannelSubs(ctx, map[string]bool{"b-z": true}); err != nil {
		t.Fatalf("ReconcileChannelSubs() = %v, want nil", err)
	}
	if !calls.deletedID("on-zombie") {
		t.Fatal("zombie stream.online sub was not deleted")
	}
	if zombie, err := repo.GetSubscription(ctx, "on-zombie"); err != nil {
		t.Fatalf("GetSubscription(on-zombie): %v", err)
	} else if zombie.RevokedAt == nil {
		t.Fatal("on-zombie RevokedAt = nil, want revoked after zombie sweep")
	}
	if _, ok := calls.createdFor("stream.online"); !ok {
		t.Fatal("no replacement stream.online sub was created after the zombie sweep")
	}
	if _, ok := calls.createdFor("stream.offline"); !ok {
		t.Fatal("no stream.offline sub was created for the broadcaster")
	}
}

// TestReconcileChannelSubsCircuitBreakerAborts pins that a run of consecutive
// subscribe failures trips the breaker: the reconcile returns an error and stops
// launching the remaining creates instead of hammering Twitch once per channel.
func TestReconcileChannelSubsCircuitBreakerAborts(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	const newCallback = "https://replayvod.example/api/v1/webhook/callback"

	const channels = 20
	channelIDs := make(map[string]bool, channels)
	for i := 0; i < channels; i++ {
		bid := fmt.Sprintf("b-breaker-%02d", i)
		seedSubscriptionChannel(t, ctx, repo, bid)
		channelIDs[bid] = true
	}

	var mu sync.Mutex
	var postAttempts int
	tc := twitch.NewClient("client-id", "client-secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	tc.SetHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Host == "id.twitch.tv" && req.URL.Path == "/oauth2/token":
				return textResponse(http.StatusOK, `{"access_token":"app-token","expires_in":3600,"token_type":"bearer"}`), nil
			case req.Method == http.MethodGet && req.URL.Path == "/helix/eventsub/subscriptions":
				return textResponse(http.StatusOK, eventSubListOf()), nil
			case req.Method == http.MethodPost && req.URL.Path == "/helix/eventsub/subscriptions":
				mu.Lock()
				postAttempts++
				mu.Unlock()
				// 401 is non-transient: an app-token rejection the breaker should
				// bail on rather than retry per channel.
				return textResponse(http.StatusUnauthorized, `{"error":"Unauthorized","status":401,"message":"nope"}`), nil
			default:
				t.Fatalf("unexpected Twitch request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		}),
	})
	svc := New(repo, tc, newCallback, "0123456789abcdef", slog.New(slog.NewTextHandler(io.Discard, nil)))

	err := svc.ReconcileChannelSubs(ctx, channelIDs)
	if err == nil {
		t.Fatal("ReconcileChannelSubs() = nil, want circuit-breaker error")
	}
	if !strings.Contains(err.Error(), "consecutive subscribe failures") {
		t.Fatalf("error = %v, want circuit-breaker message", err)
	}
	mu.Lock()
	attempts := postAttempts
	mu.Unlock()
	// 20 channels × 2 sub types = 40 creates if it never aborted. The breaker
	// must stop well short of that.
	if attempts >= channels*2 {
		t.Fatalf("post attempts = %d, want fewer than %d (breaker should abort early)", attempts, channels*2)
	}
}

// failingCreateSubRepo wraps a real repo but fails CreateSubscription, to drive
// the "Twitch accepted but local mirror failed" path in subscribe().
type failingCreateSubRepo struct {
	repository.Repository
	err error
}

func (r *failingCreateSubRepo) CreateSubscription(context.Context, *repository.SubscriptionInput) (*repository.Subscription, error) {
	return nil, r.err
}

// TestSubscribeMirrorInsertFailureSurfacesError pins the half-applied path: when
// Twitch accepts the subscription but the local mirror insert fails, subscribe
// returns a wrapped error so the caller knows the mirror is out of sync (the next
// Snapshot self-heals it).
func TestSubscribeMirrorInsertFailureSurfacesError(t *testing.T) {
	ctx := context.Background()
	base := sqliteadapter.New(testdb.NewSQLiteDB(t))
	seedSubscriptionChannel(t, ctx, base, "b-mirror")
	repo := &failingCreateSubRepo{Repository: base, err: errors.New("mirror write failed")}

	const newCallback = "https://replayvod.example/api/v1/webhook/callback"
	var created bool
	tc := twitch.NewClient("client-id", "client-secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	tc.SetHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Host == "id.twitch.tv" && req.URL.Path == "/oauth2/token":
				return textResponse(http.StatusOK, `{"access_token":"app-token","expires_in":3600,"token_type":"bearer"}`), nil
			case req.Method == http.MethodPost && req.URL.Path == "/helix/eventsub/subscriptions":
				created = true
				return textResponse(http.StatusAccepted, eventSubCreateResponse("on-mirror", "stream.online", "1", "b-mirror", newCallback)), nil
			default:
				t.Fatalf("unexpected Twitch request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		}),
	})
	svc := New(repo, tc, newCallback, "0123456789abcdef", slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, err := svc.SubscribeStreamOnline(ctx, "b-mirror")
	if err == nil {
		t.Fatal("SubscribeStreamOnline() = nil, want mirror-insert error")
	}
	if !strings.Contains(err.Error(), "mirror subscription") {
		t.Fatalf("error = %v, want a mirror-subscription wrap", err)
	}
	if !created {
		t.Fatal("Twitch create was not called; the mirror failure must come AFTER Twitch accepts")
	}
}

// TestReconcileChannelSubsBlockedStaleCallbackIsNotDuplicated pins the
// sweepStaleCallbacks blocked path: when deleting a stale-callback sub fails, it
// stays in the have-set so the reconcile does NOT create a duplicate on the new
// callback while Twitch still has the old transport.
func TestReconcileChannelSubsBlockedStaleCallbackIsNotDuplicated(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	const (
		newCallback = "https://replayvod.example/api/v1/webhook/callback"
		oldCallback = "https://old.example/api/v1/webhook/callback"
	)
	seedSubscriptionChannel(t, ctx, repo, "b-blocked")

	var mu sync.Mutex
	createdTypes := map[string]bool{}
	deleteAttempted := false
	tc := twitch.NewClient("client-id", "client-secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	tc.SetHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Host == "id.twitch.tv" && req.URL.Path == "/oauth2/token":
				return textResponse(http.StatusOK, `{"access_token":"app-token","expires_in":3600,"token_type":"bearer"}`), nil
			case req.Method == http.MethodGet && req.URL.Path == "/helix/eventsub/subscriptions":
				switch req.URL.Query().Get("type") {
				case "stream.online":
					// Alive sub on the OLD callback -> stale, must be swept.
					return textResponse(http.StatusOK, eventSubListOf(eventSubSubJSONWithStatus("on-stale", "stream.online", "1", "b-blocked", oldCallback, "enabled"))), nil
				default:
					return textResponse(http.StatusOK, eventSubListOf()), nil
				}
			case req.Method == http.MethodDelete && req.URL.Path == "/helix/eventsub/subscriptions":
				mu.Lock()
				deleteAttempted = true
				mu.Unlock()
				// Delete fails so the stale sub is "blocked" and kept in the have-set.
				return textResponse(http.StatusBadRequest, `{"error":"Bad Request","status":400,"message":"boom"}`), nil
			case req.Method == http.MethodPost && req.URL.Path == "/helix/eventsub/subscriptions":
				var body struct {
					Type string `json:"type"`
				}
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Fatalf("decode create body: %v", err)
				}
				mu.Lock()
				createdTypes[body.Type] = true
				mu.Unlock()
				return textResponse(http.StatusAccepted, eventSubCreateResponse("new-"+body.Type, body.Type, "1", "b-blocked", newCallback)), nil
			default:
				t.Fatalf("unexpected Twitch request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		}),
	})
	svc := New(repo, tc, newCallback, "0123456789abcdef", slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := svc.ReconcileChannelSubs(ctx, map[string]bool{"b-blocked": true}); err != nil {
		t.Fatalf("ReconcileChannelSubs() = %v, want nil (best-effort)", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !deleteAttempted {
		t.Fatal("stale-callback delete was not attempted")
	}
	if createdTypes["stream.online"] {
		t.Fatal("a duplicate stream.online sub was created while the stale one's delete was still pending")
	}
	if !createdTypes["stream.offline"] {
		t.Fatal("the missing stream.offline sub should still have been created")
	}
}

// TestSnapshotSelfHealsUntrackedSubscription pins the Snapshot self-heal: a sub
// Twitch reports but we don't mirror locally is upserted so the snapshot's
// junction link succeeds and historical snapshots stay complete.
func TestSnapshotSelfHealsUntrackedSubscription(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	seedSubscriptionChannel(t, ctx, repo, "b-orphan")

	const callback = "https://replayvod.example/api/v1/webhook/callback"
	tc := twitch.NewClient("client-id", "client-secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	tc.SetHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.Host == "id.twitch.tv" && req.URL.Path == "/oauth2/token":
				return textResponse(http.StatusOK, `{"access_token":"app-token","expires_in":3600,"token_type":"bearer"}`), nil
			case req.Method == http.MethodGet && req.URL.Path == "/helix/eventsub/subscriptions":
				return textResponse(http.StatusOK, eventSubListOf(eventSubSubJSONWithStatus("orphan-sub", "stream.online", "1", "b-orphan", callback, "enabled"))), nil
			default:
				t.Fatalf("unexpected Twitch request: %s %s", req.Method, req.URL.String())
				return nil, nil
			}
		}),
	})
	svc := New(repo, tc, callback, "0123456789abcdef", slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := repo.GetSubscription(ctx, "orphan-sub"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("precondition: GetSubscription(orphan-sub) = %v, want ErrNotFound", err)
	}

	snap, err := svc.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot() = %v, want nil", err)
	}
	if snap == nil {
		t.Fatal("Snapshot() returned nil snapshot")
	}
	healed, err := repo.GetSubscription(ctx, "orphan-sub")
	if err != nil {
		t.Fatalf("orphan sub was not self-healed into the mirror: %v", err)
	}
	if healed.Type != "stream.online" || healed.TransportCallback != callback {
		t.Fatalf("self-healed sub = %+v, want stream.online on the configured callback", healed)
	}
}

func seedSubscriptionChannel(t *testing.T, ctx context.Context, repo repository.Repository, broadcasterID string) {
	t.Helper()
	if _, err := repo.UpsertUser(ctx, &repository.User{
		ID:          broadcasterID,
		Login:       broadcasterID,
		DisplayName: broadcasterID,
		Role:        "viewer",
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID:    broadcasterID,
		BroadcasterLogin: broadcasterID,
		BroadcasterName:  broadcasterID,
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
}

func createTestSubscription(t *testing.T, ctx context.Context, repo repository.Repository, id, broadcasterID string) {
	t.Helper()
	createTestSubscriptionWithTypeCallback(t, ctx, repo, id, broadcasterID, "stream.online", "https://replayvod.example/api/v1/webhook/callback")
}

func createTestSubscriptionWithTypeCallback(t *testing.T, ctx context.Context, repo repository.Repository, id, broadcasterID, subType, callbackURL string) {
	t.Helper()
	if _, err := repo.CreateSubscription(ctx, &repository.SubscriptionInput{
		ID:                id,
		Status:            "enabled",
		Type:              subType,
		Version:           "1",
		Cost:              1,
		Condition:         []byte(`{"broadcaster_user_id":"` + broadcasterID + `"}`),
		BroadcasterID:     &broadcasterID,
		TransportMethod:   "webhook",
		TransportCallback: callbackURL,
		TwitchCreatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("create subscription %s: %v", id, err)
	}
}

func textResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func eventSubListResponse(id, typ, version, broadcasterID, callbackURL string) string {
	return fmt.Sprintf(`{"data":[%s],"pagination":{},"total":1,"total_cost":1,"max_total_cost":10000}`,
		eventSubSubscriptionJSON(id, typ, version, broadcasterID, callbackURL))
}

func eventSubCreateResponse(id, typ, version, broadcasterID, callbackURL string) string {
	return fmt.Sprintf(`{"data":[%s]}`, eventSubSubscriptionJSON(id, typ, version, broadcasterID, callbackURL))
}

func eventSubSubscriptionJSON(id, typ, version, broadcasterID, callbackURL string) string {
	return eventSubSubJSONWithStatus(id, typ, version, broadcasterID, callbackURL, "enabled")
}

func eventSubSubJSONWithStatus(id, typ, version, broadcasterID, callbackURL, status string) string {
	return fmt.Sprintf(`{"id":%q,"status":%q,"type":%q,"version":%q,"condition":{"broadcaster_user_id":%q},"created_at":%q,"transport":{"method":"webhook","callback":%q},"cost":1}`,
		id, status, typ, version, broadcasterID, time.Now().UTC().Format(time.RFC3339Nano), callbackURL)
}

func eventSubListOf(subsJSON ...string) string {
	return fmt.Sprintf(`{"data":[%s],"pagination":{},"total":%d,"total_cost":1,"max_total_cost":10000}`,
		strings.Join(subsJSON, ","), len(subsJSON))
}

// TestIsCallbackURLUsable_MatchesConfigRule keeps the service's callback check
// in lockstep with config.IsUsableWebhookURL, which is the single rule for
// "Twitch will accept this callback". The two had drifted: config rejects
// loopback hosts (Twitch can never reach them) while this helper did not, so a
// dev pointing direct delivery at https://localhost would be rejected at boot
// but accepted by the subscribe/reconcile guard.
func TestIsCallbackURLUsable_MatchesConfigRule(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{raw: "https://replayvod.example/api/v1/webhook/callback", want: true},
		{raw: "https://replayvod.example:443/api/v1/webhook/callback", want: true},
		{raw: "https://localhost/api/v1/webhook/callback", want: false},
		{raw: "https://127.0.0.1/api/v1/webhook/callback", want: false},
		{raw: "http://replayvod.example/api/v1/webhook/callback", want: false},
		{raw: "https://replayvod.example:8080/api/v1/webhook/callback", want: false},
		{raw: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			if got := isCallbackURLUsable(tc.raw); got != tc.want {
				t.Fatalf("isCallbackURLUsable(%q) = %v, want %v", tc.raw, got, tc.want)
			}
			if got := config.IsUsableWebhookURL(tc.raw); got != tc.want {
				t.Fatalf("config.IsUsableWebhookURL(%q) = %v, want %v (rules must agree)", tc.raw, got, tc.want)
			}
		})
	}
}
