package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/go-chi/chi/v5"
)

const testSecret = "test-webhook-secret"

// newTestHandler spins up the webhook handler backed by an in-memory-ish
// SQLite adapter. Callers mutate the returned processor's behavior via the
// processorFn. The adapter is fully migrated.
type fakeProcessor struct {
	calls  atomic.Int32
	sentAt atomic.Pointer[time.Time]
	fn     func(context.Context, *twitch.EventSubNotification) error
}

func (f *fakeProcessor) Process(ctx context.Context, n *twitch.EventSubNotification, sentAt time.Time) error {
	f.calls.Add(1)
	f.sentAt.Store(&sentAt)
	if f.fn != nil {
		return f.fn(ctx, n)
	}
	return nil
}

func newTestServer(t *testing.T, proc EventProcessor) (*httptest.Server, repository.Repository) {
	t.Helper()
	return newTestServerWithRepo(t, proc, func(r repository.Repository) repository.Repository { return r })
}

// newTestServerWithRepo lets a test wrap the repository the handler uses; the
// returned repository is the unwrapped one, for assertions.
func newTestServerWithRepo(t *testing.T, proc EventProcessor, wrap func(repository.Repository) repository.Repository) (*httptest.Server, repository.Repository) {
	t.Helper()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewHandler(wrap(repo), testSecret, proc, log)
	r := chi.NewRouter()
	// Keep the production recoverer in place to catch panics outside the
	// processor's terminal-failure handling.
	r.Use(middleware.Recoverer(log))
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

// TestWebhook_Verification_ChallengeXSS checks that HTML challenges stay plain
// text and require authentication.
func TestWebhook_Verification_ChallengeXSS(t *testing.T) {
	const challenge = `<script>alert("é & XSS")</script>`
	signedBody := verificationBody("12345", challenge, "sub-xss")
	for _, tc := range []struct {
		name       string
		body       string
		secret     string
		wantStatus int
		wantBody   string
	}{
		{
			name: "unsigned browser form",
			// A text/plain form joins its input name and value with '='.
			body:       `{"challenge":"<script>alert()</script>","x":"="}` + "\r\n",
			wantStatus: http.StatusBadRequest,
			wantBody:   "invalid webhook\n",
		},
		{
			name:       "forged signature",
			body:       signedBody,
			secret:     "attacker-secret",
			wantStatus: http.StatusForbidden,
			wantBody:   "invalid webhook\n",
		},
		{
			name:       "signed HTML challenge",
			body:       signedBody,
			secret:     testSecret,
			wantStatus: http.StatusOK,
			wantBody:   challenge,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !json.Valid([]byte(tc.body)) {
				t.Fatal("fixture must be valid JSON so parsing cannot mask an authentication regression")
			}
			srv, _ := newTestServer(t, &fakeProcessor{})
			req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/webhook/callback", strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "text/plain")
			if tc.secret != "" {
				req.Header.Set(twitch.EventSubHeaderMessageType, string(twitch.MsgTypeVerification))
				signRequest(req, "verify-xss", time.Now().UTC().Format(time.RFC3339Nano), []byte(tc.body), tc.secret)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			got, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if string(got) != tc.wantBody {
				t.Errorf("body = %q, want %q", got, tc.wantBody)
			}
			if got := resp.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
				t.Errorf("Content-Type = %q, want text/plain; charset=utf-8", got)
			}
			if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
			if resp.ContentLength != int64(len(tc.wantBody)) {
				t.Errorf("Content-Length = %d, want %d bytes", resp.ContentLength, len(tc.wantBody))
			}
		})
	}
}

func TestWebhookRejectsOversizedBody(t *testing.T) {
	proc := &fakeProcessor{}
	srv, _ := newTestServer(t, proc)

	body := []byte(strings.Replace(
		notificationBody("12345", "sub-big", "event-big"),
		`"type": "live",`,
		`"type": "live", "padding": "`+strings.Repeat("x", maxWebhookBodyBytes)+`",`,
		1,
	))
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/webhook/callback", strings.NewReader(string(body)))
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	req.Header.Set(twitch.EventSubHeaderMessageType, string(twitch.MsgTypeNotification))
	signRequest(req, "big-body", ts, body, testSecret)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if proc.calls.Load() != 0 {
		t.Fatalf("processor calls = %d, want 0", proc.calls.Load())
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

func TestWebhook_NotificationWithoutProcessorAuditsAndMarksProcessed(t *testing.T) {
	srv, repo := newTestServer(t, nil)
	body := []byte(notificationBody("12345", "sub-disabled", "event-disabled"))

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/webhook/callback", strings.NewReader(string(body)))
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	req.Header.Set(twitch.EventSubHeaderMessageType, string(twitch.MsgTypeNotification))
	signRequest(req, "disabled-msg", ts, body, testSecret)

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}

	stored, err := repo.GetWebhookEventByEventID(context.Background(), "disabled-msg")
	if err != nil {
		t.Fatalf("audit lookup: %v", err)
	}
	if stored.Status != repository.WebhookStatusProcessed {
		t.Fatalf("Status = %q, want %q", stored.Status, repository.WebhookStatusProcessed)
	}
	if stored.ProcessedAt == nil {
		t.Fatal("ProcessedAt must be set for audit-only notifications")
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
// audit log. The failure is instead recorded on the audit row, and a
// redelivery of a failed event is not run again.
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

	if s := mustDeliverNotification(t, srv, "fail-msg-1", body); s != http.StatusNoContent {
		t.Errorf("redelivery status = %d, want 204", s)
	}
	if got := proc.calls.Load(); got != 1 {
		t.Errorf("processor calls = %d, want 1 (a failed event is not retried)", got)
	}
}

// TestWebhook_Notification_ProcessorSuccess_MarksProcessed confirms the
// success-path audit update. Without this assertion a future change that
// returns early after Process() would silently drop the processed-at
// marker, and a redelivery would then run an event that already succeeded.
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

func deliverNotification(srv *httptest.Server, messageID string, body []byte) (int, error) {
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/webhook/callback", strings.NewReader(string(body)))
	if err != nil {
		return 0, err
	}
	req.Header.Set(twitch.EventSubHeaderMessageType, string(twitch.MsgTypeNotification))
	signRequest(req, messageID, time.Now().UTC().Format(time.RFC3339Nano), body, testSecret)
	resp, err := srv.Client().Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

func mustDeliverNotification(t *testing.T, srv *httptest.Server, messageID string, body []byte) int {
	t.Helper()
	status, err := deliverNotification(srv, messageID, body)
	if err != nil {
		t.Fatalf("deliver %s: %v", messageID, err)
	}
	return status
}

func requireWebhookStatus(t *testing.T, repo repository.Repository, messageID, want string) {
	t.Helper()
	stored, err := repo.GetWebhookEventByEventID(context.Background(), messageID)
	if err != nil {
		t.Fatalf("audit lookup: %v", err)
	}
	if stored.Status != want {
		t.Fatalf("Status = %q, want %q", stored.Status, want)
	}
}

// A process killed mid-event leaves its row received. The redelivery that
// reaches the restarted server must run the event, not dedupe it away, or a
// lost stream.online means the broadcast is never recorded. It runs with the
// time of the first delivery, which tells a replayed stream.offline apart from
// a broadcast that started since.
func TestWebhook_Notification_RedeliveryRunsEventLeftReceived(t *testing.T) {
	proc := &fakeProcessor{}
	srv, repo := newTestServer(t, proc)
	body := []byte(notificationBody("12345", "sub-crash", "event-crash"))
	firstSent := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Second)
	if _, err := repo.CreateWebhookEvent(context.Background(), &repository.WebhookEventInput{
		EventID:          "crash-msg",
		MessageType:      repository.WebhookMessageNotification,
		MessageTimestamp: firstSent,
		Payload:          body,
	}); err != nil {
		t.Fatal(err)
	}

	if s := mustDeliverNotification(t, srv, "crash-msg", body); s != http.StatusNoContent {
		t.Fatalf("redelivery status = %d, want 204", s)
	}
	if got := proc.calls.Load(); got != 1 {
		t.Fatalf("processor calls = %d, want 1", got)
	}
	if got := proc.sentAt.Load(); got == nil || !got.Equal(firstSent) {
		t.Fatalf("processor sentAt = %v, want the first delivery's %v", got, firstSent)
	}
	requireWebhookStatus(t, repo, "crash-msg", repository.WebhookStatusProcessed)
}

func TestWebhook_Notification_ProcessorPanicIsTerminal(t *testing.T) {
	proc := &fakeProcessor{fn: func(_ context.Context, n *twitch.EventSubNotification) error {
		if n.Subscription.ID == "sub-panic" {
			panic("processor blew up")
		}
		return nil
	}}
	srv, repo := newTestServer(t, proc)
	body := []byte(notificationBody("12345", "sub-panic", "event-panic"))

	if s := mustDeliverNotification(t, srv, "panic-msg", body); s != http.StatusNoContent {
		t.Fatalf("first delivery status = %d, want 204", s)
	}
	requireWebhookStatus(t, repo, "panic-msg", repository.WebhookStatusFailed)
	stored, err := repo.GetWebhookEventByEventID(context.Background(), "panic-msg")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Error == nil || !strings.Contains(*stored.Error, "processor blew up") {
		t.Errorf("Error = %v, want the panic recorded", stored.Error)
	}
	if stored.ProcessedAt == nil {
		t.Error("ProcessedAt must be set on terminal failure")
	}
	if s := mustDeliverNotification(t, srv, "panic-msg", body); s != http.StatusNoContent {
		t.Fatalf("redelivery status = %d, want 204", s)
	}
	if got := proc.calls.Load(); got != 1 {
		t.Fatalf("processor calls = %d, want 1 (a panicked event is not retried)", got)
	}

	// The failed event must not stop another channel's stream.online from
	// reaching the processor, including when it follows in a relay replay.
	next := []byte(notificationBody("67890", "sub-next", "event-next"))
	if s := mustDeliverNotification(t, srv, "next-msg", next); s != http.StatusNoContent {
		t.Fatalf("next event status = %d, want 204", s)
	}
	if got := proc.calls.Load(); got != 2 {
		t.Fatalf("processor calls = %d, want 2", got)
	}
	requireWebhookStatus(t, repo, "next-msg", repository.WebhookStatusProcessed)
}

func deliverNotificationAsync(srv *httptest.Server, messageID string, body []byte) <-chan int {
	status := make(chan int, 1)
	go func() {
		s, err := deliverNotification(srv, messageID, body)
		if err != nil {
			s = -1
		}
		status <- s
	}()
	return status
}

func awaitSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal(failure)
	}
}

// requireUnanswered fails when a delivery gets its response while the attempt
// it waits on is still blocked.
func requireUnanswered(t *testing.T, status <-chan int, delivery string) {
	t.Helper()
	select {
	case s := <-status:
		t.Fatalf("%s answered %d before the first attempt finished", delivery, s)
	case <-time.After(200 * time.Millisecond):
	}
}

// Twitch retries a delivery that has not answered in time, so a redelivery can
// arrive while the first attempt is still running. It must wait for that
// attempt instead of starting a second run of the same event.
func TestWebhook_Notification_RedeliveryDuringFirstAttemptRunsOnce(t *testing.T) {
	for _, outcome := range []string{"success", "panic"} {
		t.Run(outcome, func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			var attempts atomic.Int32
			proc := &fakeProcessor{fn: func(context.Context, *twitch.EventSubNotification) error {
				if attempts.Add(1) == 1 {
					close(started)
					<-release
				}
				if outcome == "panic" {
					panic("processor blew up")
				}
				return nil
			}}
			srv, repo := newTestServer(t, proc)
			body := []byte(notificationBody("12345", "sub-slow", "event-slow"))

			first := deliverNotificationAsync(srv, "slow-msg", body)
			awaitSignal(t, started, "first delivery never reached the processor")
			retry := deliverNotificationAsync(srv, "slow-msg", body)
			requireUnanswered(t, retry, "redelivery")
			close(release)
			if s := <-first; s != http.StatusNoContent {
				t.Errorf("first delivery status = %d, want 204", s)
			}
			if s := <-retry; s != http.StatusNoContent {
				t.Errorf("redelivery status = %d, want 204", s)
			}
			if got := proc.calls.Load(); got != 1 {
				t.Fatalf("processor calls = %d, want 1", got)
			}
			want := repository.WebhookStatusProcessed
			if outcome == "panic" {
				want = repository.WebhookStatusFailed
			}
			requireWebhookStatus(t, repo, "slow-msg", want)
		})
	}
}

// stallingRepo holds the first CreateWebhookEvent until released and then
// fails it, as a database stuck behind a lock would.
type stallingRepo struct {
	repository.Repository
	started, release chan struct{}
	calls            atomic.Int32
}

func (r *stallingRepo) CreateWebhookEvent(ctx context.Context, input *repository.WebhookEventInput) (*repository.WebhookEvent, error) {
	if r.calls.Add(1) == 1 {
		close(r.started)
		<-r.release
		return nil, errors.New("database is locked")
	}
	return r.Repository.CreateWebhookEvent(ctx, input)
}

// A redelivery that arrives while the first attempt is still inserting its
// audit row must not be acknowledged on that attempt's behalf: when the insert
// then fails, Twitch would stop retrying an event nothing stored or ran.
func TestWebhook_Notification_RedeliveryDuringFailedInsertRunsEvent(t *testing.T) {
	stall := &stallingRepo{started: make(chan struct{}), release: make(chan struct{})}
	proc := &fakeProcessor{}
	srv, repo := newTestServerWithRepo(t, proc, func(r repository.Repository) repository.Repository {
		stall.Repository = r
		return stall
	})
	body := []byte(notificationBody("12345", "sub-stall", "event-stall"))

	first := deliverNotificationAsync(srv, "stall-msg", body)
	awaitSignal(t, stall.started, "first delivery never reached the insert")
	retry := deliverNotificationAsync(srv, "stall-msg", body)
	requireUnanswered(t, retry, "redelivery")
	close(stall.release)
	if s := <-first; s != http.StatusInternalServerError {
		t.Errorf("first delivery status = %d, want 500", s)
	}
	if s := <-retry; s != http.StatusNoContent {
		t.Errorf("redelivery status = %d, want 204", s)
	}
	if got := proc.calls.Load(); got != 1 {
		t.Fatalf("processor calls = %d, want 1", got)
	}
	requireWebhookStatus(t, repo, "stall-msg", repository.WebhookStatusProcessed)
}

// Server.Stop shuts the downloader down before the HTTP server, so a
// stream.online can arrive while downloads are refused. It must stay
// retryable instead of being marked failed and acknowledged.
func TestWebhook_Notification_ShutdownDefersEventToRedelivery(t *testing.T) {
	var attempts atomic.Int32
	proc := &fakeProcessor{fn: func(context.Context, *twitch.EventSubNotification) error {
		if attempts.Add(1) == 1 {
			return fmt.Errorf("start download: %w", downloader.ErrShuttingDown)
		}
		return nil
	}}
	srv, repo := newTestServer(t, proc)
	body := []byte(notificationBody("12345", "sub-stop", "event-stop"))

	if s := mustDeliverNotification(t, srv, "stop-msg", body); s != http.StatusServiceUnavailable {
		t.Fatalf("delivery during shutdown status = %d, want 503", s)
	}
	requireWebhookStatus(t, repo, "stop-msg", repository.WebhookStatusReceived)
	if s := mustDeliverNotification(t, srv, "stop-msg", body); s != http.StatusNoContent {
		t.Fatalf("redelivery status = %d, want 204", s)
	}
	if got := proc.calls.Load(); got != 2 {
		t.Fatalf("processor calls = %d, want 2", got)
	}
	requireWebhookStatus(t, repo, "stop-msg", repository.WebhookStatusProcessed)
}
