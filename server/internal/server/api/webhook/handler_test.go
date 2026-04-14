package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/go-chi/chi/v5"
)

const testSecret = "test-webhook-secret"

// newTestHandler spins up the webhook handler backed by an in-memory-ish
// SQLite adapter. Callers mutate the returned processor's behavior via the
// processorFn. The adapter is fully migrated.
type fakeProcessor struct {
	calls atomic.Int32
	fn    func(context.Context, *twitch.EventSubNotification) error
}

func (f *fakeProcessor) Process(ctx context.Context, n *twitch.EventSubNotification) error {
	f.calls.Add(1)
	if f.fn != nil {
		return f.fn(ctx, n)
	}
	return nil
}

func newTestServer(t *testing.T, proc EventProcessor) (*httptest.Server, repository.Repository) {
	t.Helper()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	h := NewHandler(repo, testSecret, proc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { h.SetupRoutes(r) })
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, repo
}

// signRequest computes the Twitch-Eventsub-Message-Signature for a payload.
func signRequest(req *http.Request, id, timestamp string, body []byte, secret string) {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(id))
	mac.Write([]byte(timestamp))
	mac.Write(body)
	req.Header.Set(twitch.EventSubHeaderMessageID, id)
	req.Header.Set(twitch.EventSubHeaderMessageTimestamp, timestamp)
	req.Header.Set(twitch.EventSubHeaderMessageSignature, "sha256="+hex.EncodeToString(mac.Sum(nil)))
}

func verificationBody(broadcasterID, challenge, subID string) string {
	return fmt.Sprintf(`{
		"challenge": %q,
		"subscription": {
			"id": %q,
			"status": "webhook_callback_verification_pending",
			"type": "stream.online",
			"version": "1",
			"condition": {"broadcaster_user_id": %q},
			"transport": {"method": "webhook", "callback": "https://example/cb"},
			"created_at": "2026-04-12T00:00:00Z",
			"cost": 1
		}
	}`, challenge, subID, broadcasterID)
}

func notificationBody(broadcasterID, subID, eventID string) string {
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

func revocationBody(broadcasterID, subID, reason string) string {
	return fmt.Sprintf(`{
		"subscription": {
			"id": %q,
			"status": %q,
			"type": "stream.online",
			"version": "1",
			"condition": {"broadcaster_user_id": %q},
			"transport": {"method": "webhook", "callback": "https://example/cb"},
			"created_at": "2026-04-12T00:00:00Z",
			"cost": 1
		}
	}`, subID, reason, broadcasterID)
}

// TestWebhook_Verification_EchoesChallenge is the handshake path: when Twitch
// creates a subscription it expects the handler to echo the challenge string
// verbatim with 200. Getting this wrong silently breaks subscription creation
// — Twitch shows "webhook_callback_verification_failed" and we never receive
// events for that sub.
func TestWebhook_Verification_EchoesChallenge(t *testing.T) {
	srv, repo := newTestServer(t, &fakeProcessor{})
	body := []byte(verificationBody("12345", "pogchamp", "sub-v1"))

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/webhook/callback", strings.NewReader(string(body)))
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	req.Header.Set(twitch.EventSubHeaderMessageType, string(twitch.MsgTypeVerification))
	signRequest(req, "verify-msg-1", ts, body, testSecret)

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if string(got) != "pogchamp" {
		t.Errorf("body = %q, want pogchamp (exact string, no newline, no json wrap)", string(got))
	}

	// Verification is also recorded in the audit log — operators expect to
	// see the handshake in /system/eventsub even when no subscription data
	// follows.
	stored, err := repo.GetWebhookEventByEventID(context.Background(), "verify-msg-1")
	if err != nil {
		t.Fatalf("audit lookup: %v", err)
	}
	if stored.MessageType != repository.WebhookMessageVerification {
		t.Errorf("MessageType = %q", stored.MessageType)
	}
}

// TestWebhook_Notification_DedupsOnMessageIDRetry covers Twitch's at-least-once
// retry semantics: on delivery failure Twitch retries with the same
// Message-Id. The ON CONFLICT DO NOTHING path in CreateWebhookEvent must
// recognize the repeat, NOT invoke the processor a second time (that would
// double-download), and return 2xx so Twitch stops retrying.
func TestWebhook_Notification_DedupsOnMessageIDRetry(t *testing.T) {
	proc := &fakeProcessor{}
	srv, repo := newTestServer(t, proc)
	body := []byte(notificationBody("12345", "sub-n1", "event-1"))

	post := func() int {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/webhook/callback", strings.NewReader(string(body)))
		ts := time.Now().UTC().Format(time.RFC3339Nano)
		req.Header.Set(twitch.EventSubHeaderMessageType, string(twitch.MsgTypeNotification))
		signRequest(req, "same-id-always", ts, body, testSecret)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	if s := post(); s != http.StatusNoContent {
		t.Fatalf("first post status = %d, want 204", s)
	}
	if s := post(); s != http.StatusNoContent {
		t.Fatalf("second (dup) post status = %d, want 204", s)
	}

	if got := proc.calls.Load(); got != 1 {
		t.Fatalf("processor calls = %d, want 1 (second delivery must be de-duped)", got)
	}

	count, err := repo.CountWebhookEvents(context.Background())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("webhook_events rows = %d, want 1", count)
	}
}

// TestWebhook_ReplayOutsideWindow_Returns403 guards the replay-attack boundary.
// Twitch documents a 10-minute window; anything older must be rejected before
// we touch the DB, because an attacker with a recorded signed body could
// otherwise re-deliver it indefinitely.
func TestWebhook_ReplayOutsideWindow_Returns403(t *testing.T) {
	srv, repo := newTestServer(t, &fakeProcessor{})
	body := []byte(notificationBody("12345", "sub-r1", "event-r1"))

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/webhook/callback", strings.NewReader(string(body)))
	oldTS := time.Now().Add(-15 * time.Minute).UTC().Format(time.RFC3339Nano)
	req.Header.Set(twitch.EventSubHeaderMessageType, string(twitch.MsgTypeNotification))
	signRequest(req, "replay-msg", oldTS, body, testSecret)

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}

	// Replay must not even be recorded — acceptance is the opposite of what
	// we want, because acceptance would poison the dedup key forever.
	count, _ := repo.CountWebhookEvents(context.Background())
	if count != 0 {
		t.Errorf("webhook_events rows = %d, want 0 (replay must not persist)", count)
	}
}

// TestWebhook_SignatureMismatch_Returns403 is the core HMAC boundary: a
// request signed with the wrong secret must never reach the repository.
// Returning 403 (not 400) intentionally reveals nothing about WHY we
// rejected.
func TestWebhook_SignatureMismatch_Returns403(t *testing.T) {
	srv, repo := newTestServer(t, &fakeProcessor{})
	body := []byte(notificationBody("12345", "sub-s1", "event-s1"))

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/webhook/callback", strings.NewReader(string(body)))
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	req.Header.Set(twitch.EventSubHeaderMessageType, string(twitch.MsgTypeNotification))
	signRequest(req, "bad-sig-msg", ts, body, "wrong-secret")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	count, _ := repo.CountWebhookEvents(context.Background())
	if count != 0 {
		t.Errorf("webhook_events rows = %d, want 0 (bad signature must not persist)", count)
	}
}

// TestWebhook_TamperedBody_Returns403 confirms the signature covers every
// byte of the payload. If the HMAC input were computed post-parse (a real
// past bug in other eventsub consumers), an attacker could tweak event
// fields and still pass verification.
func TestWebhook_TamperedBody_Returns403(t *testing.T) {
	srv, repo := newTestServer(t, &fakeProcessor{})
	body := []byte(notificationBody("12345", "sub-t1", "event-t1"))
	tampered := []byte(notificationBody("67890", "sub-t1", "event-t1"))

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/webhook/callback", strings.NewReader(string(tampered)))
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	req.Header.Set(twitch.EventSubHeaderMessageType, string(twitch.MsgTypeNotification))
	signRequest(req, "tampered-msg", ts, body, testSecret) // sign ORIGINAL body

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	count, _ := repo.CountWebhookEvents(context.Background())
	if count != 0 {
		t.Errorf("webhook_events rows = %d, want 0", count)
	}
}

// TestWebhook_Revocation_MarksSubscriptionRevoked confirms the soft-delete
// path: receiving a revocation updates revoked_at on the matching subscription
// row. Without this, "active subs" listings stay stale and the dashboard
// keeps showing subscriptions Twitch has actually stopped delivering to.
func TestWebhook_Revocation_MarksSubscriptionRevoked(t *testing.T) {
	proc := &fakeProcessor{}
	srv, repo := newTestServer(t, proc)
	ctx := context.Background()

	// Seed: the subscription needs to exist for the revocation FK to bite.
	// Transport JSON is valid; mocks the Twitch create-response we'd have
	// stored when we subscribed in the first place.
	// Also seed a channel row because subscriptions.broadcaster_id FKs it.
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID:    "12345",
		BroadcasterLogin: "coolstreamer",
		BroadcasterName:  "CoolStreamer",
		ViewCount:        0,
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	bid := "12345"
	if _, err := repo.CreateSubscription(ctx, &repository.SubscriptionInput{
		ID:                "sub-rev-1",
		Status:            "enabled",
		Type:              "stream.online",
		Version:           "1",
		Cost:              1,
		Condition:         []byte(`{"broadcaster_user_id":"12345"}`),
		BroadcasterID:     &bid,
		TransportMethod:   "webhook",
		TransportCallback: "https://example/cb",
		TwitchCreatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	body := []byte(revocationBody("12345", "sub-rev-1", "authorization_revoked"))
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/webhook/callback", strings.NewReader(string(body)))
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	req.Header.Set(twitch.EventSubHeaderMessageType, string(twitch.MsgTypeRevocation))
	signRequest(req, "rev-msg-1", ts, body, testSecret)

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}

	got, err := repo.GetSubscription(ctx, "sub-rev-1")
	if err != nil {
		t.Fatalf("get sub: %v", err)
	}
	if got.RevokedAt == nil {
		t.Error("RevokedAt must be set after revocation")
	}
	if got.RevokedReason == nil || *got.RevokedReason != "authorization_revoked" {
		t.Errorf("RevokedReason = %v, want authorization_revoked", got.RevokedReason)
	}
}

// TestWebhook_Notification_ProcessorFailure_StillReturns204 guards the
// retry-storm prevention: if our processor (schedule matcher, downloader)
// errors, we must still return 2xx or Twitch retries forever and floods the
// audit log. The failure is instead recorded via MarkWebhookEventFailed so
// the dashboard surfaces it.
func TestWebhook_Notification_ProcessorFailure_StillReturns204(t *testing.T) {
	proc := &fakeProcessor{
		fn: func(context.Context, *twitch.EventSubNotification) error {
			return fmt.Errorf("processor exploded")
		},
	}
	srv, repo := newTestServer(t, proc)
	body := []byte(notificationBody("12345", "sub-f1", "event-f1"))

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/webhook/callback", strings.NewReader(string(body)))
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	req.Header.Set(twitch.EventSubHeaderMessageType, string(twitch.MsgTypeNotification))
	signRequest(req, "fail-msg-1", ts, body, testSecret)

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204 (retries must not be triggered on processor failure)", resp.StatusCode)
	}

	stored, err := repo.GetWebhookEventByEventID(context.Background(), "fail-msg-1")
	if err != nil {
		t.Fatalf("audit lookup: %v", err)
	}
	if stored.Status != repository.WebhookStatusFailed {
		t.Errorf("Status = %q, want failed", stored.Status)
	}
	if stored.Error == nil || !strings.Contains(*stored.Error, "processor exploded") {
		t.Errorf("Error = %v, want to contain processor error text", stored.Error)
	}
}

// TestWebhook_Notification_ProcessorSuccess_MarksProcessed confirms the
// success-path audit update. Without this assertion a future change that
// returns early after Process() would silently drop the processed-at
// marker; the dashboard's "stuck received" query would then falsely page
// operators for events that actually succeeded.
func TestWebhook_Notification_ProcessorSuccess_MarksProcessed(t *testing.T) {
	srv, repo := newTestServer(t, &fakeProcessor{})
	body := []byte(notificationBody("12345", "sub-ok1", "event-ok1"))

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/webhook/callback", strings.NewReader(string(body)))
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	req.Header.Set(twitch.EventSubHeaderMessageType, string(twitch.MsgTypeNotification))
	signRequest(req, "ok-msg-1", ts, body, testSecret)

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	stored, err := repo.GetWebhookEventByEventID(context.Background(), "ok-msg-1")
	if err != nil {
		t.Fatalf("audit lookup: %v", err)
	}
	if stored.Status != repository.WebhookStatusProcessed {
		t.Errorf("Status = %q, want processed", stored.Status)
	}
	if stored.ProcessedAt == nil {
		t.Error("ProcessedAt must be set on success")
	}
}
