package subscriptions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/trpcgo"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type requestKey struct{}

func testServer(t *testing.T) (*httptest.Server, *Handler, *atomic.Int32) {
	t.Helper()
	router := trpcgo.NewRouter(trpcgo.WithContextCreator(func(ctx context.Context, r *http.Request) context.Context {
		return context.WithValue(ctx, requestKey{}, r)
	}))
	running := &atomic.Int32{}
	auth := trpcgo.Use(func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			r := ctx.Value(requestKey{}).(*http.Request)
			if r.Header.Get("Cookie") != "session=test" {
				return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
			}
			meta, ok := trpcgo.GetProcedureMeta(ctx)
			if !ok || meta.Type != trpcgo.ProcedureSubscription {
				return nil, errors.New("procedure metadata missing")
			}
			return next(ctx, input)
		}
	})
	trpcgo.MustSubscribe(router, "feed", func(ctx context.Context, input struct {
		Value string `json:"value"`
	}) (<-chan string, error) {
		ch := make(chan string, 1)
		ch <- input.Value
		running.Add(1)
		go func() { <-ctx.Done(); running.Add(-1); close(ch) }()
		return ch, nil
	}, auth)
	trpcgo.MustVoidSubscribe(router, "owner", func(ctx context.Context) (<-chan string, error) {
		return nil, trpcgo.NewError(trpcgo.CodeForbidden, "insufficient permissions")
	}, auth)
	trpcgo.MustVoidSubscribe(router, "broken", func(ctx context.Context) (<-chan string, error) { return nil, errors.New("private database password") }, auth)
	trpcgo.MustVoidSubscribe(router, "completed", func(ctx context.Context) (<-chan string, error) { ch := make(chan string); close(ch); return ch, nil }, auth)
	trpcgo.MustVoidMutation(router, "delete", func(context.Context) (bool, error) { t.Error("mutation executed over WebSocket"); return true, nil })
	handler := NewHandler(router, []string{"https://dashboard.example"})
	server := httptest.NewServer(handler)
	t.Cleanup(func() { _ = handler.Close(); server.Close(); _ = router.Close() })
	return server, handler, running
}

func dial(t *testing.T, server *httptest.Server, cookie bool) *websocket.Conn {
	t.Helper()
	headers := http.Header{"Origin": {"https://dashboard.example"}}
	if cookie {
		headers.Set("Cookie", "session=test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func send(t *testing.T, conn *websocket.Conn, message string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(message)); err != nil {
		t.Fatal(err)
	}
}

func receive(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var value map[string]any
	if err := wsjson.Read(ctx, conn, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func waitCount(t *testing.T, count *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if count.Load() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("active subscriptions = %d, want %d", count.Load(), want)
}

func TestMultiplexAndCancelIndependently(t *testing.T) {
	server, _, running := testServer(t)
	conn := dial(t, server, true)
	send(t, conn, `[{"id":1,"method":"subscription","params":{"path":"feed","input":{"value":"one"}}},{"id":2,"method":"subscription","params":{"path":"feed","input":{"value":"two"}}}]`)
	values := map[float64]string{}
	for range 4 {
		msg := receive(t, conn)
		result := msg["result"].(map[string]any)
		if result["type"] == "data" {
			values[msg["id"].(float64)] = result["data"].(string)
		}
	}
	if values[1] != "one" || values[2] != "two" {
		t.Fatalf("misrouted data: %v", values)
	}
	send(t, conn, `{"id":1,"method":"subscription.stop"}`)
	msg := receive(t, conn)
	if msg["id"] != float64(1) || msg["result"].(map[string]any)["type"] != "stopped" {
		t.Fatalf("stop: %v", msg)
	}
	waitCount(t, running, 1)
	_ = conn.CloseNow()
	waitCount(t, running, 0)
}

func TestAuthorizationAndErrorsUseTRPCShape(t *testing.T) {
	for _, tc := range []struct {
		name, message, code string
		cookie              bool
	}{
		{"anonymous", `{"id":1,"method":"subscription","params":{"path":"feed"}}`, "UNAUTHORIZED", false},
		{"owner", `{"id":1,"method":"subscription","params":{"path":"owner"}}`, "FORBIDDEN", true},
		{"internal", `{"id":1,"method":"subscription","params":{"path":"broken"}}`, "INTERNAL_SERVER_ERROR", true},
		{"mutation method", `{"id":1,"method":"mutation","params":{"path":"delete"}}`, "METHOD_NOT_SUPPORTED", true},
		{"mutation path", `{"id":1,"method":"subscription","params":{"path":"delete"}}`, "NOT_FOUND", true},
		{"bad input", `{"id":1,"method":"subscription","params":{"path":"feed","input":{"value":4}}}`, "BAD_REQUEST", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, _, _ := testServer(t)
			conn := dial(t, server, tc.cookie)
			send(t, conn, tc.message)
			msg := receive(t, conn)
			raw, _ := json.Marshal(msg)
			if strings.Contains(string(raw), "password") {
				t.Fatalf("leaked internal error: %s", raw)
			}
			err := msg["error"].(map[string]any)
			if err["data"].(map[string]any)["code"] != tc.code || msg["id"] != float64(1) {
				t.Fatalf("error: %s", raw)
			}
		})
	}
}

func TestRejectsUntrustedOrigins(t *testing.T) {
	server, _, _ := testServer(t)
	for _, origin := range []string{"https://evil.example", "http://dashboard.example", "https://dashboard.example.evil", "null"} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		conn, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}}})
		cancel()
		if conn != nil {
			_ = conn.CloseNow()
		}
		if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
			t.Fatalf("origin %q: response=%v error=%v", origin, response, err)
		}
	}
}

func TestHeartbeatCompletionAndShutdown(t *testing.T) {
	server, handler, running := testServer(t)
	conn := dial(t, server, true)
	send(t, conn, "PING")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil || string(data) != "PONG" {
		t.Fatalf("heartbeat: %q %v", data, err)
	}
	send(t, conn, `{"id":1,"method":"subscription","params":{"path":"completed"}}`)
	for _, kind := range []string{"started", "stopped"} {
		if receive(t, conn)["result"].(map[string]any)["type"] != kind {
			t.Fatalf("expected %s", kind)
		}
	}
	send(t, conn, `{"id":2,"method":"subscription","params":{"path":"feed"}}`)
	receive(t, conn)
	receive(t, conn)
	waitCount(t, running, 1)
	_ = handler.Close()
	waitCount(t, running, 0)
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("shutdown left socket open")
	}
}

func TestSubscriptionLimitKeepsExistingFeedsAlive(t *testing.T) {
	server, _, running := testServer(t)
	conn := dial(t, server, true)
	for id := 1; id <= maxSubscriptions; id++ {
		send(t, conn, fmt.Sprintf(`{"id":%d,"method":"subscription","params":{"path":"feed"}}`, id))
		receive(t, conn)
		receive(t, conn)
	}
	waitCount(t, running, maxSubscriptions)
	send(t, conn, `{"id":99,"method":"subscription","params":{"path":"feed"}}`)
	msg := receive(t, conn)
	if msg["error"].(map[string]any)["data"].(map[string]any)["code"] != "TOO_MANY_REQUESTS" {
		t.Fatalf("limit error: %v", msg)
	}
	waitCount(t, running, maxSubscriptions)
	// A terminal response permits immediate reuse of the operation's slot and ID.
	for range 100 {
		send(t, conn, `{"id":1,"method":"subscription.stop"}`)
		if msg := receive(t, conn); msg["result"].(map[string]any)["type"] != "stopped" {
			t.Fatalf("stop response: %v", msg)
		}
		send(t, conn, `{"id":1,"method":"subscription","params":{"path":"feed"}}`)
		for _, kind := range []string{"started", "data"} {
			msg := receive(t, conn)
			result, ok := msg["result"].(map[string]any)
			if !ok || result["type"] != kind {
				t.Fatalf("replacement %s response: %v", kind, msg)
			}
		}
	}
	waitCount(t, running, maxSubscriptions)
}

func TestTerminalResponseReleasesRequestID(t *testing.T) {
	for _, path := range []string{"completed", "broken"} {
		t.Run(path, func(t *testing.T) {
			server, _, running := testServer(t)
			conn := dial(t, server, true)
			for range 100 {
				send(t, conn, fmt.Sprintf(`{"id":1,"method":"subscription","params":{"path":%q}}`, path))
				if path == "completed" {
					for _, kind := range []string{"started", "stopped"} {
						if msg := receive(t, conn); msg["result"].(map[string]any)["type"] != kind {
							t.Fatalf("completion response: %v", msg)
						}
					}
				} else if msg := receive(t, conn); msg["error"] == nil {
					t.Fatalf("missing execution error: %v", msg)
				}
				send(t, conn, `{"id":1,"method":"subscription","params":{"path":"feed"}}`)
				for _, kind := range []string{"started", "data"} {
					if msg := receive(t, conn); msg["result"].(map[string]any)["type"] != kind {
						t.Fatalf("replacement response: %v", msg)
					}
				}
				send(t, conn, `{"id":1,"method":"subscription.stop"}`)
				if msg := receive(t, conn); msg["result"].(map[string]any)["type"] != "stopped" {
					t.Fatalf("stop response: %v", msg)
				}
			}
			waitCount(t, running, 0)
		})
	}
}

func TestInvalidFramesCancelAllFeeds(t *testing.T) {
	for _, frame := range []string{
		`{"id":1,"method":"subscription","params":{"path":"feed"}}`, // duplicate
		`{"id":1.0,"method":"subscription","params":{"path":"feed"}}`,
		`{"id":1e0,"method":"subscription","params":{"path":"feed"}}`,
		`{"id":null,"method":"subscription","params":{"path":"feed"}}`,
		`{"id":{},"method":"subscription","params":{"path":"feed"}}`,
		`{"id":`,
	} {
		t.Run(frame, func(t *testing.T) {
			server, _, running := testServer(t)
			conn := dial(t, server, true)
			send(t, conn, `{"id":1,"method":"subscription","params":{"path":"feed"}}`)
			receive(t, conn)
			receive(t, conn)
			send(t, conn, frame)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			// A terminal frame can arrive before cancellation closes the socket.
			for {
				if _, _, err := conn.Read(ctx); err != nil {
					if ctx.Err() != nil {
						t.Fatal("invalid request left socket open")
					}
					break
				}
			}
			waitCount(t, running, 0)
		})
	}
}

func TestStopMatchesEquivalentRequestIDs(t *testing.T) {
	for _, tc := range []struct{ start, stop string }{
		{`1.0`, `1`},
		{`"\u0061"`, `"a"`},
		{`-0`, `0`},
	} {
		t.Run(tc.start, func(t *testing.T) {
			server, _, running := testServer(t)
			conn := dial(t, server, true)
			send(t, conn, fmt.Sprintf(`{"id":%s,"method":"subscription","params":{"path":"feed"}}`, tc.start))
			receive(t, conn)
			receive(t, conn)
			send(t, conn, fmt.Sprintf(`{"id":%s,"method":"subscription.stop"}`, tc.stop))
			if msg := receive(t, conn); msg["result"].(map[string]any)["type"] != "stopped" {
				t.Fatalf("stop response: %v", msg)
			}
			waitCount(t, running, 0)
		})
	}
}

// TestServerHeartbeat checks liveness using native pongs without application messages.
func TestServerHeartbeat(t *testing.T) {
	for _, respond := range []bool{true, false} {
		t.Run(fmt.Sprintf("respond=%t", respond), func(t *testing.T) {
			server, handler, running := testServer(t)
			handler.heartbeatInterval = 10 * time.Millisecond
			handler.heartbeatTimeout = 100 * time.Millisecond
			var pings atomic.Int32
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{
				HTTPHeader:     http.Header{"Cookie": {"session=test"}},
				OnPingReceived: func(context.Context, []byte) bool { pings.Add(1); return respond },
			})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			send(t, conn, `{"id":1,"method":"subscription","params":{"path":"feed"}}`)
			receive(t, conn)
			receive(t, conn)
			if respond {
				done := make(chan error, 1)
				go func() { _, _, err := conn.Read(ctx); done <- err }()
				deadline := time.After(time.Second)
				for pings.Load() < 5 {
					select {
					case err := <-done:
						t.Fatalf("healthy connection closed: %v", err)
					case <-deadline:
						t.Fatal("server did not send repeated heartbeats")
					case <-time.After(time.Millisecond):
					}
				}
				waitCount(t, running, 1)
			} else {
				// Cancellation can deliver a stopped frame before closing the socket.
				stopped := false
				for {
					kind, data, err := conn.Read(ctx)
					if err != nil {
						if ctx.Err() != nil {
							t.Fatalf("heartbeat failed to close dead peer: %v", err)
						}
						break
					}
					var message struct {
						ID     int `json:"id"`
						Result struct {
							Type string `json:"type"`
						} `json:"result"`
					}
					if stopped || kind != websocket.MessageText || json.Unmarshal(data, &message) != nil || message.ID != 1 || message.Result.Type != "stopped" {
						t.Fatalf("unexpected frame before heartbeat closure: %s", data)
					}
					stopped = true
				}
				if pings.Load() == 0 {
					t.Fatal("dead peer closed without a heartbeat probe")
				}
				waitCount(t, running, 0)
			}
		})
	}
}
