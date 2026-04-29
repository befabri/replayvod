// Package relayclient connects to a hosted webhook relay (Cloudflare
// Workers + Durable Object) over a WebSocket and replays each ingested
// frame as a local HTTP POST to the configured webhook callback URL.
//
// The relay is deliberately a dumb pipe: it forwards Twitch's signed
// EventSub POSTs verbatim. HMAC verification still happens locally inside
// the existing webhook handler — the relay client never inspects payload
// contents and never holds the HMAC secret.
package relayclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

const responseBodyLimit = 64 << 10

// Config describes a relay client.
//
// SubscribeURL is the relay's WebSocket endpoint, e.g.
// "wss://relay.replayvod.com/u/<token>/subscribe". CallbackURL is the
// local webhook handler that the existing on-machine HTTP server already
// serves (Environment.RelayLocalCallbackURL, or its default loopback URL).
//
// HTTPClient is optional and is used for local webhook dispatch. If set, its
// transport settings are also reused for the WebSocket handshake, but its
// Timeout is not applied to the long-lived relay connection.
type Config struct {
	SubscribeURL string
	CallbackURL  string
	// AllowUnsafeCallbackURL is for tests or unusual embedded callers only. The
	// production Connect agent should replay exclusively to the loopback webhook.
	AllowUnsafeCallbackURL bool
	HTTPClient             *http.Client
	Logger                 *slog.Logger
}

// Client streams events from the relay and replays them locally.
type Client struct {
	cfg        Config
	log        *slog.Logger
	http       *http.Client
	dialHTTP   *http.Client
	lastCursor atomic.Int64
	ready      chan struct{}
	readyOnce  sync.Once
}

// New validates the config and returns a Client.
func New(cfg Config) (*Client, error) {
	if cfg.SubscribeURL == "" {
		return nil, errors.New("relayclient: SubscribeURL required")
	}
	u, err := url.Parse(cfg.SubscribeURL)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") {
		return nil, fmt.Errorf("relayclient: SubscribeURL must be ws:// or wss://: %q", cfg.SubscribeURL)
	}
	if cfg.CallbackURL == "" {
		return nil, errors.New("relayclient: CallbackURL required")
	}
	cb, err := url.Parse(cfg.CallbackURL)
	if err != nil || (cb.Scheme != "http" && cb.Scheme != "https") || cb.Host == "" {
		return nil, fmt.Errorf("relayclient: CallbackURL must be http:// or https://: %q", cfg.CallbackURL)
	}
	if !cfg.AllowUnsafeCallbackURL && !isSafeLocalCallbackURL(cb) {
		return nil, fmt.Errorf("relayclient: CallbackURL must be a loopback /api/v1/webhook/callback URL: %q", cfg.CallbackURL)
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		// Bounded by the relay DO's 8s dispatch budget for verification
		// challenges. A hung local handler shouldn't outlive that window.
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	dialHTTPClient := websocketHTTPClient(cfg.HTTPClient)
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Client{
		cfg:      cfg,
		log:      log.With("domain", "relayclient"),
		http:     httpClient,
		dialHTTP: dialHTTPClient,
		ready:    make(chan struct{}),
	}, nil
}

func websocketHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		return nil
	}
	dialClient := *client
	dialClient.Timeout = 0
	return &dialClient
}

// Ready is closed after the first successful relay WebSocket connection.
func (c *Client) Ready() <-chan struct{} {
	return c.ready
}

// Run blocks until ctx is cancelled, reconnecting with exponential
// backoff on transport errors. Backoff resets on a clean session.
func (c *Client) Run(ctx context.Context) {
	const (
		minBackoff = 1 * time.Second
		maxBackoff = 60 * time.Second
	)
	backoff := minBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		err := c.session(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			c.log.Warn("relay session ended", "error", err, "backoff", backoff.String())
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
		// Reset to minimum after roughly the buffer-replay window so a
		// transient blip doesn't compound into long stalls when the
		// relay comes back.
		if err == nil {
			backoff = minBackoff
		}
	}
}

func (c *Client) session(ctx context.Context) error {
	dialURL := c.cfg.SubscribeURL
	if cursor := c.lastCursor.Load(); cursor > 0 {
		u, err := url.Parse(dialURL)
		if err != nil {
			return fmt.Errorf("parse subscribe url: %w", err)
		}
		q := u.Query()
		q.Set("cursor", strconv.FormatInt(cursor, 10))
		u.RawQuery = q.Encode()
		dialURL = u.String()
	}

	conn, _, err := websocket.Dial(ctx, dialURL, &websocket.DialOptions{
		HTTPClient: c.dialHTTP,
	})
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "shutdown")
	conn.SetReadLimit(1 << 20) // 1 MiB; EventSub payloads are kilobytes

	c.readyOnce.Do(func() { close(c.ready) })
	c.log.Info("relay connected", "subscribe_host", urlHost(c.cfg.SubscribeURL))
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("read: %w", err)
		}
		if err := c.handleFrame(ctx, conn, data); err != nil {
			return fmt.Errorf("frame dispatch: %w", err)
		}
	}
}

type frame struct {
	ID               string            `json:"id"`
	Cursor           int64             `json:"cursor"`
	TS               int64             `json:"ts"`
	Headers          map[string]string `json:"headers"`
	Body             string            `json:"body"`
	RequiresResponse bool              `json:"requires_response"`
}

type localResponse struct {
	status  int
	headers map[string]string
	body    []byte
}

type dispatchResult struct {
	Type    string            `json:"type"`
	ID      string            `json:"id"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

func (c *Client) handleFrame(ctx context.Context, conn *websocket.Conn, data []byte) error {
	var f frame
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("decode frame: %w", err)
	}
	body, err := base64.StdEncoding.DecodeString(f.Body)
	if err != nil {
		return fmt.Errorf("decode body: %w", err)
	}

	resp, dispatchErr := c.dispatch(ctx, &f, body)
	if conn != nil {
		if err := c.sendDispatchResult(ctx, conn, f.ID, resp, dispatchErr); err != nil {
			return err
		}
	}
	if dispatchErr != nil {
		return dispatchErr
	}

	if resp.status >= 400 {
		return fmt.Errorf("local callback returned %d", resp.status)
	}

	// Track the relay's monotonic cursor only for frames the local handler
	// accepted. A failed local callback should replay after reconnect; the
	// webhook handler dedupes on Twitch-Eventsub-Message-Id (see
	// internal/server/api/webhook/handler.go) so a replay is a no-op.
	if f.Cursor > 0 {
		for {
			prev := c.lastCursor.Load()
			if f.Cursor <= prev {
				break
			}
			if c.lastCursor.CompareAndSwap(prev, f.Cursor) {
				break
			}
		}
	}
	c.log.Debug("frame dispatched",
		"id", f.ID, "cursor", f.Cursor, "ts", f.TS, "status", resp.status)
	return nil
}

func (c *Client) dispatch(ctx context.Context, f *frame, body []byte) (localResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.CallbackURL, bytes.NewReader(body))
	if err != nil {
		return errorResponse(http.StatusBadGateway, err), fmt.Errorf("new request: %w", err)
	}
	for k, v := range f.Headers {
		// Skip hop-by-hop and identity headers that don't make sense to
		// replay verbatim against a local endpoint.
		if isSkippedHeader(k) {
			continue
		}
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return errorResponse(http.StatusBadGateway, err), fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyLimit+1))
	if err != nil {
		return errorResponse(http.StatusBadGateway, err), fmt.Errorf("read local response: %w", err)
	}
	if len(bodyBytes) > responseBodyLimit {
		bodyBytes = bodyBytes[:responseBodyLimit]
	}

	return localResponse{
		status:  resp.StatusCode,
		headers: responseHeaders(resp.Header),
		body:    bodyBytes,
	}, nil
}

func (c *Client) sendDispatchResult(ctx context.Context, conn *websocket.Conn, id string, resp localResponse, dispatchErr error) error {
	if dispatchErr != nil && resp.status == 0 {
		resp = errorResponse(http.StatusBadGateway, dispatchErr)
	}
	result := dispatchResult{
		Type:    "dispatch_result",
		ID:      id,
		Status:  resp.status,
		Headers: resp.headers,
		Body:    base64.StdEncoding.EncodeToString(resp.body),
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode dispatch result: %w", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		return fmt.Errorf("send dispatch result: %w", err)
	}
	return nil
}

func errorResponse(status int, err error) localResponse {
	return localResponse{
		status: status,
		headers: map[string]string{
			"content-type": "text/plain; charset=utf-8",
		},
		body: []byte(err.Error() + "\n"),
	}
}

func responseHeaders(h http.Header) map[string]string {
	out := make(map[string]string)
	for name, values := range h {
		if len(values) == 0 || isSkippedHeader(name) {
			continue
		}
		out[strings.ToLower(name)] = values[0]
	}
	return out
}

func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

func isSkippedHeader(name string) bool {
	switch strings.ToLower(name) {
	case "host", "connection", "content-length",
		"transfer-encoding", "upgrade", "keep-alive",
		"cf-connecting-ip", "cf-ipcountry", "cf-ray", "cf-visitor",
		"x-forwarded-for", "x-forwarded-proto", "x-real-ip":
		return true
	}
	return false
}

func isSafeLocalCallbackURL(u *url.URL) bool {
	if u.Path != "/api/v1/webhook/callback" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
