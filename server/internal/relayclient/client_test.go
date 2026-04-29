package relayclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestNewRejectsInvalidCallbackURL(t *testing.T) {
	_, err := New(Config{
		SubscribeURL: "wss://relay.example/u/token-token-token/subscribe",
		CallbackURL:  "not-a-url",
	})
	if err == nil {
		t.Fatal("expected invalid CallbackURL error")
	}
}

func TestNewRejectsUnsafeCallbackURL(t *testing.T) {
	tests := []string{
		"http://example.com/api/v1/webhook/callback",
		"http://127.0.0.1:8080/internal",
		"http://127.0.0.1:8080/api/v1/webhook/callback?next=/internal",
	}
	for _, callbackURL := range tests {
		t.Run(callbackURL, func(t *testing.T) {
			_, err := New(Config{
				SubscribeURL: "wss://relay.example/u/token-token-token/subscribe",
				CallbackURL:  callbackURL,
			})
			if err == nil {
				t.Fatal("expected unsafe CallbackURL error")
			}
		})
	}
}

func TestNewAcceptsLoopbackWebhookCallbackURL(t *testing.T) {
	_, err := New(Config{
		SubscribeURL: "wss://relay.example/u/token-token-token/subscribe",
		CallbackURL:  "http://127.0.0.1:8080/api/v1/webhook/callback",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
}

func TestHandleFrameForwardsBodyAndSafeHeaders(t *testing.T) {
	const body = `{"subscription":{"id":"sub-1"}}`

	var gotBody string
	var gotEventID string
	var gotHost string
	var gotContentLength int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		gotBody = string(data)
		gotEventID = r.Header.Get("Twitch-Eventsub-Message-Id")
		gotHost = r.Host
		gotContentLength = r.ContentLength
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c, err := New(Config{
		SubscribeURL:           "wss://relay.example/u/token-token-token/subscribe",
		CallbackURL:            srv.URL,
		AllowUnsafeCallbackURL: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	f := frame{
		ID:     "frame-1",
		Cursor: 42,
		TS:     1234,
		Headers: map[string]string{
			"host":                       "attacker.example",
			"content-length":             "999",
			"twitch-eventsub-message-id": "event-1",
		},
		Body: base64.StdEncoding.EncodeToString([]byte(body)),
	}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}

	if err := c.handleFrame(context.Background(), nil, data); err != nil {
		t.Fatalf("handleFrame: %v", err)
	}

	if gotBody != body {
		t.Fatalf("body = %q, want %q", gotBody, body)
	}
	if gotEventID != "event-1" {
		t.Fatalf("event header = %q, want event-1", gotEventID)
	}
	if gotHost == "attacker.example" {
		t.Fatal("forwarded untrusted Host header")
	}
	if gotContentLength != int64(len(body)) {
		t.Fatalf("content length = %d, want %d", gotContentLength, len(body))
	}
	if got := c.lastCursor.Load(); got != 42 {
		t.Fatalf("lastCursor = %d, want 42", got)
	}
}

func TestHandleFrameDoesNotAdvanceCursorOnServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, err := New(Config{
		SubscribeURL:           "wss://relay.example/u/token-token-token/subscribe",
		CallbackURL:            srv.URL,
		AllowUnsafeCallbackURL: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	f := frame{
		ID:     "frame-1",
		Cursor: 42,
		TS:     1234,
		Body:   base64.StdEncoding.EncodeToString([]byte("{}")),
	}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}

	if err := c.handleFrame(context.Background(), nil, data); err == nil {
		t.Fatal("expected server error")
	}
	if got := c.lastCursor.Load(); got != 0 {
		t.Fatalf("lastCursor = %d, want 0", got)
	}
}

func TestHandleFrameSendsDispatchResult(t *testing.T) {
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Relay-Test", "ok")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("challenge-response"))
	}))
	defer callback.Close()

	conn, resultCh, cleanup := websocketPair(t)
	defer cleanup()

	c, err := New(Config{
		SubscribeURL:           "wss://relay.example/u/token-token-token/subscribe",
		CallbackURL:            callback.URL,
		AllowUnsafeCallbackURL: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	f := frame{
		ID:               "verify-1",
		TS:               1234,
		Body:             base64.StdEncoding.EncodeToString([]byte("{}")),
		RequiresResponse: true,
	}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.handleFrame(ctx, conn, data); err != nil {
		t.Fatalf("handleFrame: %v", err)
	}

	result := readDispatchResult(t, resultCh)
	if result.Type != "dispatch_result" || result.ID != "verify-1" {
		t.Fatalf("dispatch result = %#v", result)
	}
	if result.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.Status)
	}
	body, err := base64.StdEncoding.DecodeString(result.Body)
	if err != nil {
		t.Fatalf("decode result body: %v", err)
	}
	if string(body) != "challenge-response" {
		t.Fatalf("body = %q, want challenge-response", body)
	}
	if result.Headers["x-relay-test"] != "ok" {
		t.Fatalf("x-relay-test header = %q, want ok", result.Headers["x-relay-test"])
	}
}

// TestReadyOnlyFiresAfterWebSocketHandshake guards the contract main.go
// relies on when it gates EventSub reconcile behind <-rc.Ready(): the
// channel must not close until the relay subscriber socket is actually
// accepted by the relay. Otherwise reconcile could create Twitch
// subscriptions while no subscriber is connected and the verification
// challenge would 503 at the relay.
func TestReadyOnlyFiresAfterWebSocketHandshake(t *testing.T) {
	gateAccept := make(chan struct{})
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-gateAccept:
		case <-r.Context().Done():
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		<-r.Context().Done()
	}))
	defer wsServer.Close()

	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer callback.Close()

	c, err := New(Config{
		SubscribeURL:           websocketURL(wsServer.URL),
		CallbackURL:            callback.URL,
		AllowUnsafeCallbackURL: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	select {
	case <-c.Ready():
		t.Fatal("Ready closed before websocket handshake completed")
	case <-time.After(150 * time.Millisecond):
	}

	close(gateAccept)

	select {
	case <-c.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("Ready did not close after handshake")
	}
}

func TestSessionWebSocketOutlivesCallbackHTTPClientTimeout(t *testing.T) {
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer callback.Close()

	resultCh := make(chan dispatchResult, 1)
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		// This delay is longer than the callback HTTP client timeout below. The
		// WebSocket must not inherit that dispatch timeout after the handshake.
		time.Sleep(75 * time.Millisecond)

		f := frame{
			ID:     "frame-1",
			Cursor: 42,
			TS:     1234,
			Body:   base64.StdEncoding.EncodeToString([]byte("{}")),
		}
		data, err := json.Marshal(f)
		if err != nil {
			t.Errorf("marshal frame: %v", err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
			t.Errorf("write frame: %v", err)
			return
		}
		_, result, err := conn.Read(ctx)
		if err != nil {
			t.Errorf("read dispatch result: %v", err)
			return
		}

		var parsed dispatchResult
		if err := json.Unmarshal(result, &parsed); err != nil {
			t.Errorf("unmarshal dispatch result: %v", err)
			return
		}
		resultCh <- parsed
	}))
	defer wsServer.Close()

	c, err := New(Config{
		SubscribeURL:           websocketURL(wsServer.URL),
		CallbackURL:            callback.URL,
		AllowUnsafeCallbackURL: true,
		HTTPClient:             &http.Client{Timeout: 25 * time.Millisecond},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { _ = c.session(ctx) }()

	select {
	case result := <-resultCh:
		if result.Status != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", result.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for dispatch result")
	}
}

func TestSessionStopsAfterDispatchFailure(t *testing.T) {
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer callback.Close()

	resultCh := make(chan []byte, 1)
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		f := frame{
			ID:     "frame-1",
			Cursor: 42,
			TS:     1234,
			Body:   base64.StdEncoding.EncodeToString([]byte("{}")),
		}
		data, err := json.Marshal(f)
		if err != nil {
			t.Errorf("marshal frame: %v", err)
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
			t.Errorf("write frame: %v", err)
			return
		}
		_, result, err := conn.Read(ctx)
		if err != nil {
			t.Errorf("read dispatch result: %v", err)
			return
		}
		resultCh <- result
	}))
	defer wsServer.Close()

	c, err := New(Config{
		SubscribeURL:           websocketURL(wsServer.URL),
		CallbackURL:            callback.URL,
		AllowUnsafeCallbackURL: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.session(ctx); err == nil {
		t.Fatal("expected session error")
	}

	result := readDispatchResult(t, resultCh)
	if result.Status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", result.Status)
	}
	if got := c.lastCursor.Load(); got != 0 {
		t.Fatalf("lastCursor = %d, want 0", got)
	}
}

func websocketPair(t *testing.T) (*websocket.Conn, <-chan []byte, func()) {
	t.Helper()
	resultCh := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Errorf("read dispatch result: %v", err)
			return
		}
		resultCh <- data
	}))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, websocketURL(srv.URL), nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial websocket: %v", err)
	}
	cleanup := func() {
		_ = conn.Close(websocket.StatusNormalClosure, "done")
		srv.Close()
	}
	return conn, resultCh, cleanup
}

func readDispatchResult(t *testing.T, resultCh <-chan []byte) dispatchResult {
	t.Helper()
	select {
	case data := <-resultCh:
		var result dispatchResult
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("unmarshal dispatch result: %v", err)
		}
		return result
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for dispatch result")
	}
	return dispatchResult{}
}

func websocketURL(raw string) string {
	return "ws" + strings.TrimPrefix(raw, "http")
}

func TestHandleFrameAccumulatesStaleFramesIntoSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	c, err := New(Config{
		SubscribeURL:           "wss://relay.example/u/token-token-token/subscribe",
		CallbackURL:            srv.URL,
		AllowUnsafeCallbackURL: true,
		Logger:                 slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for i := range 2 {
		f := frame{
			ID:     fmt.Sprintf("frame-%d", i),
			Cursor: int64(i + 1),
			TS:     1234, // 1970 — guaranteed stale
			Headers: map[string]string{
				"Twitch-Eventsub-Message-Type":      "notification",
				"Twitch-Eventsub-Subscription-Type": "stream.online",
			},
			Body: base64.StdEncoding.EncodeToString([]byte("{}")),
		}
		data, err := json.Marshal(f)
		if err != nil {
			t.Fatalf("marshal frame: %v", err)
		}
		if err := c.handleFrame(context.Background(), nil, data); err != nil {
			t.Fatalf("handleFrame: %v", err)
		}
	}

	if strings.Contains(buf.String(), "webhook relayed") {
		t.Fatalf("backlog frames should not emit per-frame logs: %s", buf.String())
	}

	c.finalizeFlush()

	out := buf.String()
	if !strings.Contains(out, "relay backlog flushed") {
		t.Fatalf("expected backlog summary in output: %s", out)
	}
	if !strings.Contains(out, `"count":2`) {
		t.Fatalf("expected count=2 in summary: %s", out)
	}
	if !strings.Contains(out, `"stream.online":2`) {
		t.Fatalf("expected by_sub_type stream.online:2 in summary: %s", out)
	}
	if !strings.Contains(out, `"notification":2`) {
		t.Fatalf("expected by_msg_type notification:2 in summary: %s", out)
	}
}

func TestHandleFrameVerificationChallengeBypassesFlush(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("challenge"))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	c, err := New(Config{
		SubscribeURL:           "wss://relay.example/u/token-token-token/subscribe",
		CallbackURL:            srv.URL,
		AllowUnsafeCallbackURL: true,
		Logger:                 slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	f := frame{
		ID:               "verify-1",
		TS:               1234, // stale: would normally be classified as backlog
		RequiresResponse: true,
		Headers: map[string]string{
			"Twitch-Eventsub-Message-Type": "webhook_callback_verification",
		},
		Body: base64.StdEncoding.EncodeToString([]byte("{}")),
	}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	if err := c.handleFrame(context.Background(), nil, data); err != nil {
		t.Fatalf("handleFrame: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "webhook relayed") {
		t.Fatalf("verification frame should log per-frame even when stale: %s", out)
	}
	if strings.Contains(out, "relay backlog flushed") {
		t.Fatalf("verification frame should not enter the flush summary: %s", out)
	}
}

func TestIsBacklogUsesReceiveTime(t *testing.T) {
	now := time.Now()
	fresh := now.Add(-1 * time.Second).UnixMilli()

	if isBacklog(fresh, now) {
		t.Fatal("frame received 1s after relay-side TS should classify as live, not backlog")
	}
	if !isBacklog(fresh, time.UnixMilli(fresh).Add(flushStaleness+time.Second)) {
		t.Fatal("frame received well past flushStaleness should classify as backlog")
	}
	if isBacklog(0, now) {
		t.Fatal("TS=0 must never classify as backlog regardless of receive time")
	}
}

func TestDispatchSetsRelaySentinelHeader(t *testing.T) {
	var gotSentinel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSentinel = r.Header.Get(RelayDispatchHeader)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c, err := New(Config{
		SubscribeURL:           "wss://relay.example/u/token-token-token/subscribe",
		CallbackURL:            srv.URL,
		AllowUnsafeCallbackURL: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	f := frame{
		ID:   "frame-1",
		TS:   time.Now().UnixMilli(),
		Body: base64.StdEncoding.EncodeToString([]byte("{}")),
	}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	if err := c.handleFrame(context.Background(), nil, data); err != nil {
		t.Fatalf("handleFrame: %v", err)
	}
	if gotSentinel == "" {
		t.Fatalf("expected %s header to be set on dispatch", RelayDispatchHeader)
	}
}
