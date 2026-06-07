package igdb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL       = "https://api.igdb.com/v4"
	defaultRetryBaseWait = 500 * time.Millisecond
	maxRetries           = 2
)

// TokenProvider supplies the Twitch app access token IGDB expects in the
// Authorization header. twitch.Client satisfies this through its cached token
// accessor, keeping IGDB off the Twitch package's import graph.
type TokenProvider interface {
	AppAccessToken(ctx context.Context) (string, error)
}

type Client struct {
	clientID       string
	tokenProvider  TokenProvider
	baseURL        string
	httpClient     *http.Client
	log            *slog.Logger
	retryBaseDelay time.Duration
}

type Game struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Summary   string `json:"summary"`
	Storyline string `json:"storyline"`
	URL       string `json:"url"`
}

type Error struct {
	Status     int
	Body       string
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	return fmt.Sprintf("igdb: %d: %s", e.Status, e.Body)
}

type nonRetryableError struct {
	err error
}

func (e *nonRetryableError) Error() string {
	return e.err.Error()
}

func (e *nonRetryableError) Unwrap() error {
	return e.err
}

func NewClient(clientID string, tokenProvider TokenProvider, log *slog.Logger) *Client {
	if log == nil {
		log = slog.Default()
	}
	return &Client{
		clientID:       clientID,
		tokenProvider:  tokenProvider,
		baseURL:        defaultBaseURL,
		httpClient:     &http.Client{Timeout: 10 * time.Second},
		log:            log.With("domain", "igdb"),
		retryBaseDelay: defaultRetryBaseWait,
	}
}

func (c *Client) SetHTTPClient(httpClient *http.Client) {
	if httpClient != nil {
		c.httpClient = httpClient
	}
}

func (c *Client) SetBaseURL(baseURL string) {
	if baseURL != "" {
		c.baseURL = strings.TrimRight(baseURL, "/")
	}
}

func (c *Client) SetRetryBaseDelay(delay time.Duration) {
	if delay >= 0 {
		c.retryBaseDelay = delay
	}
}

// GetGames fetches games by IGDB id. IDs must already be numeric; the service
// layer parses Twitch's string igdb_id before calling this method so query
// construction never depends on untrusted text.
func (c *Client) GetGames(ctx context.Context, ids []int64) ([]Game, error) {
	ids = normalizeIDs(ids)
	if len(ids) == 0 {
		return []Game{}, nil
	}
	if c == nil || c.tokenProvider == nil {
		return nil, fmt.Errorf("igdb client not configured")
	}
	if c.clientID == "" {
		return nil, fmt.Errorf("igdb client id not configured")
	}

	body := buildGamesQuery(ids)
	var out []Game
	if err := c.post(ctx, "/games", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func normalizeIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func buildGamesQuery(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return fmt.Sprintf(
		"fields id,name,summary,storyline,url;\nwhere id = (%s);\nlimit %d;",
		strings.Join(parts, ","),
		len(parts),
	)
}

func (c *Client) post(ctx context.Context, path, body string, out any) error {
	token, err := c.tokenProvider.AppAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("igdb app access token: %w", err)
	}
	if token == "" {
		return fmt.Errorf("igdb app access token empty")
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := c.postOnce(ctx, path, body, token, out)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRetryable(err) || attempt == maxRetries {
			return err
		}
		delay := retryDelay(err, attempt, c.retryBaseDelay)
		if delay > 0 {
			c.log.Debug("retrying IGDB request", "path", path, "attempt", attempt+1, "delay", delay, "error", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return lastErr
}

func (c *Client) postOnce(ctx context.Context, path, body, token string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewBufferString(body))
	if err != nil {
		return &nonRetryableError{err: err}
	}
	req.Header.Set("Client-ID", c.clientID)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "text/plain")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read IGDB response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Error{
			Status:     resp.StatusCode,
			Body:       strings.TrimSpace(string(respBody)),
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return &nonRetryableError{err: fmt.Errorf("decode IGDB response: %w", err)}
	}
	return nil
}

func isRetryable(err error) bool {
	var nonRetryable *nonRetryableError
	if errors.As(err, &nonRetryable) {
		return false
	}
	var igdbErr *Error
	if ok := errors.As(err, &igdbErr); ok {
		return igdbErr.Status == http.StatusTooManyRequests || igdbErr.Status >= 500
	}
	return true
}

func retryDelay(err error, attempt int, fallback time.Duration) time.Duration {
	var igdbErr *Error
	if ok := errors.As(err, &igdbErr); ok && igdbErr.RetryAfter > 0 {
		return igdbErr.RetryAfter
	}
	if fallback <= 0 {
		return 0
	}
	return fallback * time.Duration(1<<attempt)
}

func parseRetryAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(raw); err == nil {
		if d := time.Until(at); d > 0 {
			return d
		}
	}
	return 0
}
