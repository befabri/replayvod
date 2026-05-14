package twitch

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

const testSecret = "super-secret-webhook-key"

// signedHeaders returns request headers that would pass VerifyEventSubSignature
// for the given body using testSecret. Callers can tweak individual fields
// before asserting.
func signedHeaders(id, timestamp string, body []byte, secret string) http.Header {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(id))
	mac.Write([]byte(timestamp))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	h := http.Header{}
	h.Set(EventSubHeaderMessageID, id)
	h.Set(EventSubHeaderMessageTimestamp, timestamp)
	h.Set(EventSubHeaderMessageSignature, sig)
	return h
}

func captureDefaultLogger(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	slog.SetDefault(logger)
	return &buf, func() {
		slog.SetDefault(prev)
	}
}

// --- VerifyEventSubSignature ---

func TestVerifyEventSubSignature_validSignature(t *testing.T) {
	body := []byte(`{"subscription":{"type":"stream.online"}}`)
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	h := signedHeaders("msg-1", ts, body, testSecret)
	if err := VerifyEventSubSignature(h, body, testSecret, 0); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestVerifyEventSubSignature_tamperedBodyReturnsMismatch(t *testing.T) {
	body := []byte(`{"subscription":{"type":"stream.online"}}`)
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	h := signedHeaders("msg-1", ts, body, testSecret)
	tampered := []byte(`{"subscription":{"type":"stream.offline"}}`)
	err := VerifyEventSubSignature(h, tampered, testSecret, 0)
	if !errors.Is(err, ErrEventSubSignatureMismatch) {
		t.Fatalf("expected ErrEventSubSignatureMismatch, got %v", err)
	}
}

func TestVerifyEventSubSignature_wrongSecretReturnsMismatch(t *testing.T) {
	body := []byte(`{}`)
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	h := signedHeaders("msg-1", ts, body, testSecret)
	err := VerifyEventSubSignature(h, body, "different-secret", 0)
	if !errors.Is(err, ErrEventSubSignatureMismatch) {
		t.Fatalf("expected ErrEventSubSignatureMismatch, got %v", err)
	}
}

func TestVerifyEventSubSignature_replayReturnsReplayError(t *testing.T) {
	body := []byte(`{}`)
	oldTS := time.Now().Add(-15 * time.Minute).UTC().Format(time.RFC3339Nano)
	h := signedHeaders("msg-1", oldTS, body, testSecret)
	err := VerifyEventSubSignature(h, body, testSecret, 10*time.Minute)
	if !errors.Is(err, ErrEventSubMessageReplay) {
		t.Fatalf("expected ErrEventSubMessageReplay, got %v", err)
	}
}

func TestVerifyEventSubSignature_futureSkewReturnsReplayError(t *testing.T) {
	body := []byte(`{}`)
	futureTS := time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339Nano)
	h := signedHeaders("msg-1", futureTS, body, testSecret)
	err := VerifyEventSubSignature(h, body, testSecret, 10*time.Minute)
	if !errors.Is(err, ErrEventSubMessageReplay) {
		t.Fatalf("expected ErrEventSubMessageReplay, got %v", err)
	}
}

func TestVerifyEventSubSignature_rfc3339TimestampWithoutNanos(t *testing.T) {
	body := []byte(`{}`)
	ts := time.Now().UTC().Format(time.RFC3339) // no nanoseconds
	h := signedHeaders("msg-1", ts, body, testSecret)
	if err := VerifyEventSubSignature(h, body, testSecret, 0); err != nil {
		t.Fatalf("expected nil for RFC3339 timestamp, got %v", err)
	}
}

func TestVerifyEventSubSignature_malformedTimestampReturnsError(t *testing.T) {
	body := []byte(`{}`)
	h := signedHeaders("msg-1", "not-a-timestamp", body, testSecret)
	err := VerifyEventSubSignature(h, body, testSecret, 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, ErrEventSubSignatureMismatch) || errors.Is(err, ErrEventSubMessageReplay) {
		t.Fatalf("expected parse error, got sentinel: %v", err)
	}
}

func TestVerifyEventSubSignature_missingHeadersReturnsError(t *testing.T) {
	body := []byte(`{}`)
	// Missing all three eventsub headers
	err := VerifyEventSubSignature(http.Header{}, body, testSecret, 0)
	if err == nil {
		t.Fatal("expected error for missing headers")
	}
}

func TestVerifyEventSubSignature_emptySecretReturnsError(t *testing.T) {
	body := []byte(`{}`)
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	h := signedHeaders("msg-1", ts, body, testSecret)
	err := VerifyEventSubSignature(h, body, "", 0)
	if err == nil {
		t.Fatal("expected error for empty secret")
	}
}

// --- DecodeEventSubWebhook ---

func TestDecodeEventSubWebhook_verification(t *testing.T) {
	body := []byte(`{
		"challenge": "pogchamp-challenge-string",
		"subscription": {
			"id": "sub-abc",
			"status": "webhook_callback_verification_pending",
			"type": "stream.online",
			"version": "1",
			"condition": {"broadcaster_user_id": "12345"},
			"transport": {"method": "webhook", "callback": "https://example/cb"},
			"created_at": "2026-04-12T00:00:00Z",
			"cost": 1
		}
	}`)
	h := http.Header{}
	h.Set(EventSubHeaderMessageType, string(MsgTypeVerification))
	n, err := DecodeEventSubWebhook(h, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.MessageType != MsgTypeVerification {
		t.Errorf("MessageType = %q; want verification", n.MessageType)
	}
	if n.Challenge != "pogchamp-challenge-string" {
		t.Errorf("Challenge = %q; want pogchamp-challenge-string", n.Challenge)
	}
	if n.Event != nil {
		t.Errorf("Event should be nil on verification, got %T", n.Event)
	}
}

func TestDecodeEventSubWebhook_notificationKnownType(t *testing.T) {
	body := []byte(`{
		"subscription": {
			"id": "sub-abc",
			"status": "enabled",
			"type": "stream.online",
			"version": "1",
			"condition": {"broadcaster_user_id": "12345"},
			"transport": {"method": "webhook", "callback": "https://example/cb"},
			"created_at": "2026-04-12T00:00:00Z",
			"cost": 1
		},
		"event": {
			"id": "event-xyz",
			"broadcaster_user_id": "12345",
			"broadcaster_user_login": "coolstreamer",
			"broadcaster_user_name": "CoolStreamer",
			"type": "live",
			"started_at": "2026-04-12T00:05:00Z"
		}
	}`)
	h := http.Header{}
	h.Set(EventSubHeaderMessageType, string(MsgTypeNotification))
	n, err := DecodeEventSubWebhook(h, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.MessageType != MsgTypeNotification {
		t.Errorf("MessageType = %q; want notification", n.MessageType)
	}
	if n.Event == nil {
		t.Fatal("expected typed Event, got nil")
	}
	// Generated type name for stream.online event is StreamonlineEvent or similar.
	// Rather than asserting the concrete name (which depends on naming), assert it's
	// NOT UnknownEvent — i.e. the dispatch resolved to a typed struct.
	if _, isUnknown := n.Event.(UnknownEvent); isUnknown {
		t.Fatalf("expected typed event, got UnknownEvent")
	}
}

func TestDecodeEventSubWebhook_notificationCustomPowerUpRedemptionAddBeta(t *testing.T) {
	body := []byte(`{
		"subscription": {
			"id": "sub-power-up",
			"status": "enabled",
			"type": "channel.custom_power_up_redemption.add",
			"version": "beta",
			"condition": {"broadcaster_user_id": "12345", "reward_id": "reward-1"},
			"transport": {"method": "webhook", "callback": "https://example/cb"},
			"created_at": "2026-04-12T00:00:00Z",
			"cost": 1
		},
		"event": {
			"id": "redemption-1",
			"broadcaster_user_id": "12345",
			"broadcaster_user_login": "coolstreamer",
			"broadcaster_user_name": "CoolStreamer",
			"user_id": "67890",
			"user_login": "viewer",
			"user_name": "Viewer",
			"user_input": "make it sparkle",
			"status": "unfulfilled",
			"custom_power_up": {
				"id": "power-up-1",
				"title": "Sparkle",
				"bits": 100,
				"prompt": "Say something"
			},
			"redeemed_at": "2026-04-12T00:05:00Z"
		}
	}`)
	h := http.Header{}
	h.Set(EventSubHeaderMessageType, string(MsgTypeNotification))
	n, err := DecodeEventSubWebhook(h, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ev, ok := n.Event.(*ChannelCustomPowerUpRedemptionAddEvent)
	if !ok {
		t.Fatalf("Event = %T; want *ChannelCustomPowerUpRedemptionAddEvent", n.Event)
	}
	if ev.CustomPowerUp.ID != "power-up-1" || ev.CustomPowerUp.Title != "Sparkle" || ev.CustomPowerUp.Bits != 100 {
		t.Errorf("CustomPowerUp decoded incorrectly: %+v", ev.CustomPowerUp)
	}
	if ev.UserInput != "make it sparkle" || ev.Status != "unfulfilled" {
		t.Errorf("event fields decoded incorrectly: %+v", ev)
	}
}

func TestDecodeEventSubWebhook_notificationUnknownType(t *testing.T) {
	logBuf, restore := captureDefaultLogger(t)
	defer restore()

	body := []byte(`{
		"subscription": {
			"id": "sub-new",
			"status": "enabled",
			"type": "stream.futuristic_new_type",
			"version": "99",
			"condition": {"foo": "bar"},
			"transport": {"method": "webhook", "callback": "https://example/cb"},
			"created_at": "2026-04-12T00:00:00Z",
			"cost": 1
		},
		"event": {"foo": "bar"}
	}`)
	h := http.Header{}
	h.Set(EventSubHeaderMessageType, string(MsgTypeNotification))
	n, err := DecodeEventSubWebhook(h, body)
	if err != nil {
		t.Fatalf("unexpected error on unknown type: %v", err)
	}
	ue, ok := n.Event.(UnknownEvent)
	if !ok {
		t.Fatalf("expected UnknownEvent for unknown subscription type, got %T", n.Event)
	}
	if ue.Type != "stream.futuristic_new_type" {
		t.Errorf("UnknownEvent.Type = %q; want stream.futuristic_new_type", ue.Type)
	}
	if len(ue.Raw) == 0 {
		t.Error("UnknownEvent.Raw should preserve the raw event payload")
	}
	if !strings.Contains(logBuf.String(), "unknown eventsub event type") {
		t.Fatalf("expected unknown event warning, got %q", logBuf.String())
	}
}

func TestDecodeEventSubWebhook_revocation(t *testing.T) {
	body := []byte(`{
		"subscription": {
			"id": "sub-revoked",
			"status": "authorization_revoked",
			"type": "stream.online",
			"version": "1",
			"condition": {"broadcaster_user_id": "12345"},
			"transport": {"method": "webhook", "callback": "https://example/cb"},
			"created_at": "2026-04-12T00:00:00Z",
			"cost": 1
		}
	}`)
	h := http.Header{}
	h.Set(EventSubHeaderMessageType, string(MsgTypeRevocation))
	n, err := DecodeEventSubWebhook(h, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.MessageType != MsgTypeRevocation {
		t.Errorf("MessageType = %q; want revocation", n.MessageType)
	}
	if n.Event != nil {
		t.Errorf("Event should be nil on revocation, got %T", n.Event)
	}
}

func TestDecodeEventSubWebhook_unknownMessageType(t *testing.T) {
	body := []byte(`{}`)
	h := http.Header{}
	h.Set(EventSubHeaderMessageType, "bogus_type")
	_, err := DecodeEventSubWebhook(h, body)
	if err == nil {
		t.Fatal("expected error for unknown message type")
	}
	if !strings.Contains(err.Error(), "unknown eventsub message type") {
		t.Errorf("error should mention unknown message type, got %q", err.Error())
	}
}

func TestDecodeEventSubWebhook_malformedEnvelope(t *testing.T) {
	body := []byte(`{not valid json`)
	h := http.Header{}
	h.Set(EventSubHeaderMessageType, string(MsgTypeNotification))
	_, err := DecodeEventSubWebhook(h, body)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestDecodeEventSubWebhook_notificationKnownMalformedEventReturnsError(t *testing.T) {
	body := []byte(`{
		"subscription": {
			"id": "sub-abc",
			"status": "enabled",
			"type": "stream.online",
			"version": "1",
			"condition": {"broadcaster_user_id": "12345"},
			"transport": {"method": "webhook", "callback": "https://example/cb"},
			"created_at": "2026-04-12T00:00:00Z",
			"cost": 1
		},
		"event": "not-an-object"
	}`)
	h := http.Header{}
	h.Set(EventSubHeaderMessageType, string(MsgTypeNotification))
	_, err := DecodeEventSubWebhook(h, body)
	if err == nil {
		t.Fatal("expected error for malformed known event payload")
	}
	if !strings.Contains(err.Error(), "decode stream.online v1 event") {
		t.Fatalf("expected known-event decode error, got %v", err)
	}
}

// --- conditionFromType / decodeEventSubTransport fallback paths ---

func TestConditionFromType_unknownReturnsUnknownCondition(t *testing.T) {
	raw := json.RawMessage(`{"some":"condition"}`)
	cond, known, err := conditionFromType("stream.brand_new_type", "1", raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if known {
		t.Fatal("known should be false for unknown type")
	}
	uc, ok := cond.(UnknownCondition)
	if !ok {
		t.Fatalf("expected UnknownCondition, got %T", cond)
	}
	if uc.Type != "stream.brand_new_type" || uc.Version != "1" {
		t.Errorf("UnknownCondition metadata wrong: %+v", uc)
	}
	if string(uc.Raw) != string(raw) {
		t.Errorf("UnknownCondition.Raw not preserved")
	}
}

func TestEventSubSubscription_UnmarshalJSON_unknownTypeLogsWarning(t *testing.T) {
	logBuf, restore := captureDefaultLogger(t)
	defer restore()

	var sub EventSubSubscription
	err := json.Unmarshal([]byte(`{
		"id": "sub-new",
		"status": "enabled",
		"type": "stream.brand_new_type",
		"version": "1",
		"condition": {"some": "condition"},
		"transport": {"method": "webhook", "callback": "https://example/cb"},
		"created_at": "2026-04-12T00:00:00Z",
		"cost": 1
	}`), &sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := sub.Condition.(UnknownCondition); !ok {
		t.Fatalf("expected UnknownCondition, got %T", sub.Condition)
	}
	if !strings.Contains(logBuf.String(), "unknown eventsub subscription type") {
		t.Fatalf("expected unknown subscription warning, got %q", logBuf.String())
	}
}

func TestConditionFromType_knownMalformedReturnsError(t *testing.T) {
	// stream.online exists; feed it invalid JSON for its condition.
	raw := json.RawMessage(`"not-an-object"`)
	_, known, err := conditionFromType("stream.online", "1", raw)
	if !known {
		t.Error("expected known=true for stream.online even when payload is malformed")
	}
	if err == nil {
		t.Fatal("expected decode error for malformed known-type payload")
	}
}

func TestDecodeEventSubTransport_unknownMethodReturnsUnknownTransport(t *testing.T) {
	raw := json.RawMessage(`{"method":"quantum","endpoint":"wormhole://x"}`)
	tr, err := decodeEventSubTransport(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ut, ok := tr.(UnknownTransport)
	if !ok {
		t.Fatalf("expected UnknownTransport, got %T", tr)
	}
	if ut.Method != "quantum" {
		t.Errorf("UnknownTransport.Method = %q; want quantum", ut.Method)
	}
	if len(ut.Raw) == 0 {
		t.Error("UnknownTransport.Raw should preserve raw payload")
	}
}

func TestDecodeEventSubTransport_webhookRoundtrip(t *testing.T) {
	raw := json.RawMessage(`{"method":"webhook","callback":"https://example.com/cb","secret":"s3cr3t"}`)
	tr, err := decodeEventSubTransport(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wt, ok := tr.(WebhookTransport)
	if !ok {
		t.Fatalf("expected WebhookTransport, got %T", tr)
	}
	if wt.Callback != "https://example.com/cb" {
		t.Errorf("Callback = %q", wt.Callback)
	}
}
