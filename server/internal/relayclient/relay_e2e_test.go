//go:build integration

// End-to-end coverage of the .env-driven relay path. Wires the same
// `relayclient.Client` the production server boots, in front of the same
// `webhook.Handler` Twitch posts to, then pushes a synthetic HMAC-signed
// Twitch EventSub notification through a real running relay (default
// `ws://localhost:8788`, override with RELAY_BASE_URL). Asserts the
// handler decoded and dispatched it.
//
// Run with:
//
//	RELAY_TEST_TOKEN=<token issued by the cloud worker> \
//	go test -tags=integration -run TestRelayEndToEnd \
//	    ./internal/relayclient/...
//
// The token must already be active in whichever cloud the relay validates
// against. Locally that means inserting a row into the cloud worker's D1.

package relayclient_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/befabri/replayvod/server/internal/relayclient"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/server/api/webhook"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

type capturingProcessor struct {
	mu     sync.Mutex
	events []*twitch.EventSubNotification
}

func (p *capturingProcessor) Process(_ context.Context, n *twitch.EventSubNotification) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, n)
	return nil
}

func (p *capturingProcessor) seen(messageID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.events {
		if e.Subscription.ID == messageID {
			return true
		}
	}
	return false
}

func TestRelayEndToEnd(t *testing.T) {
	token := os.Getenv("RELAY_TEST_TOKEN")
	if token == "" {
		t.Skip("RELAY_TEST_TOKEN not set; issue one against the running cloud worker first")
	}
	relayBase := strings.TrimRight(getenv("RELAY_BASE_URL", "ws://localhost:8788"), "/")
	const hmacSecret = "smoke-test-hmac-secret-32-chars!!"

	// Build a real webhook handler over a real (in-memory) sqlite repository
	// and mount it under chi at /api/v1, exactly like the production server.
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	proc := &capturingProcessor{}
	handler := webhook.NewHandler(
		repo,
		hmacSecret,
		proc,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { handler.SetupRoutes(r) })
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	callbackURL := srv.URL + "/api/v1/webhook/callback"

	client, err := relayclient.New(relayclient.Config{
		SubscribeURL: relayBase + "/u/" + token + "/subscribe",
		CallbackURL:  callbackURL,
		Logger:       slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
	})
	if err != nil {
		t.Fatalf("relayclient.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go client.Run(ctx)

	select {
	case <-client.Ready():
	case <-time.After(15 * time.Second):
		t.Fatal("relay client did not connect in time; is the relay running on " + relayBase + "?")
	}

	const messageID = "smoke-msg-relayclient"
	const subscriptionID = "smoke-sub-relayclient"
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)

	body, err := json.Marshal(map[string]any{
		"subscription": map[string]any{
			"id":         subscriptionID,
			"type":       "stream.online",
			"version":    "1",
			"status":     "enabled",
			"created_at": timestamp,
			"condition":  map[string]any{"broadcaster_user_id": "12345"},
			"transport":  map[string]any{"method": "webhook", "callback": callbackURL},
			"cost":       1,
		},
		"event": map[string]any{
			"id":                     "evt-1",
			"broadcaster_user_id":    "12345",
			"broadcaster_user_login": "testchan",
			"broadcaster_user_name":  "Test Channel",
			"type":                   "live",
			"started_at":             timestamp,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	mac := hmac.New(sha256.New, []byte(hmacSecret))
	mac.Write([]byte(messageID))
	mac.Write([]byte(timestamp))
	mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	ingestURL := strings.Replace(relayBase, "ws://", "http://", 1)
	ingestURL = strings.Replace(ingestURL, "wss://", "https://", 1)
	ingestURL += "/u/" + token

	req, err := http.NewRequest(http.MethodPost, ingestURL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Twitch-Eventsub-Message-Id", messageID)
	req.Header.Set("Twitch-Eventsub-Message-Timestamp", timestamp)
	req.Header.Set("Twitch-Eventsub-Message-Type", "notification")
	req.Header.Set("Twitch-Eventsub-Subscription-Type", "stream.online")
	req.Header.Set("Twitch-Eventsub-Message-Signature", signature)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ingest POST: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(res.Body)
		t.Fatalf("ingest expected 202, got %d %s", res.StatusCode, respBody)
	}

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if proc.seen(subscriptionID) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !proc.seen(subscriptionID) {
		t.Fatal("webhook handler did not receive a stream.online notification through the relay")
	}
	if got := len(proc.events); got != 1 {
		t.Fatalf("expected exactly 1 event, got %d", got)
	}
	if got := proc.events[0].Subscription.Type; got != "stream.online" {
		t.Fatalf("unexpected subscription type %q", got)
	}
	fmt.Println("relay end-to-end ok: token=", token, " events=", len(proc.events))
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
