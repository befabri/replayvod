package twitch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// integrityCache holds a single in-flight Client-Integrity token.
// It's shared across all downloader jobs — acquiring an integrity
// token is expensive (Twitch rate-limits aggressively) and the
// token itself isn't tied to a channel or a playback token, so the
// same value works for every job.
//
// Refresh policy: the cache only refreshes when the caller explicitly
// invalidates (via MarkBad). We do NOT refresh eagerly on expiry
// because integrity tokens are rate-limited; the spec's policy is
// "happy path runs without integrity; acquire only when GQL starts
// returning errors that integrity would fix."
type integrityCache struct {
	mu       sync.Mutex
	token    string
	expires  time.Time
	inflight *inflightIntegrity
}

// inflightIntegrity coordinates a single-flight acquisition so
// concurrent jobs all wait on one HTTP call rather than hammering
// the integrity endpoint (which Twitch rate-limits in the low
// hundreds per hour per IP).
type inflightIntegrity struct {
	done  chan struct{}
	token string
	err   error
}

func newIntegrityCache() *integrityCache {
	return &integrityCache{}
}

// integrityResponse matches the body Twitch returns from
// /integrity. Only the two fields we actually consume are decoded;
// the real body carries more metadata that we don't need.
type integrityResponse struct {
	Token      string `json:"token"`
	Expiration int64  `json:"expiration"` // ms since epoch
}

// Acquire returns a cached integrity token or fetches a new one.
// All callers for the same cache state share the same acquisition —
// see inflightIntegrity above.
//
// Returns ("", nil) only when the network request succeeds but the
// response body is empty or malformed; callers treat that the same
// as a transient error and retry.
func (c *integrityCache) Acquire(ctx context.Context, client *Client) (string, error) {
	c.mu.Lock()
	// Cached + not expired → return immediately. 60s slack so a
	// token that's about to expire mid-request doesn't make the
	// segment fetch's retry path immediately invalidate it.
	if c.token != "" && time.Now().Add(60*time.Second).Before(c.expires) {
		token := c.token
		c.mu.Unlock()
		return token, nil
	}
	// In-flight acquisition: wait on it rather than starting a
	// second one. First-caller owns inflight; others block on
	// the done channel.
	if c.inflight != nil {
		flight := c.inflight
		c.mu.Unlock()
		select {
		case <-flight.done:
			return flight.token, flight.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	flight := &inflightIntegrity{done: make(chan struct{})}
	c.inflight = flight
	c.mu.Unlock()

	token, expires, err := fetchIntegrity(ctx, client)

	c.mu.Lock()
	flight.token = token
	flight.err = err
	if err == nil && token != "" {
		c.token = token
		c.expires = expires
	}
	c.inflight = nil
	c.mu.Unlock()
	close(flight.done)
	return token, err
}

// MarkBad invalidates the cached token. Called by callers that
// observe a request failure the integrity token was supposed to
// fix — forces the next Acquire to fetch a fresh one.
func (c *integrityCache) MarkBad() {
	c.mu.Lock()
	c.token = ""
	c.expires = time.Time{}
	c.mu.Unlock()
}

// fetchIntegrity POSTs to gql.twitch.tv/integrity with the headers
// Twitch expects: Client-ID, Device-Id, User-Agent. Returns the
// token and its expiry; expiration missing or in the past is
// treated as "no cache" (token is still returned so a one-shot
// caller can use it).
func fetchIntegrity(ctx context.Context, client *Client) (string, time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, integrityURL, strings.NewReader(""))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("build integrity request: %w", err)
	}
	req.Header.Set("Client-ID", client.clientID)
	req.Header.Set("Device-Id", client.deviceID)
	req.Header.Set("User-Agent", client.userAgent)

	resp, err := client.http.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("integrity request: %w", err)
	}
	defer drainAndClose(resp)

	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("integrity: status %d", resp.StatusCode)
	}

	var body integrityResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", time.Time{}, fmt.Errorf("integrity decode: %w", err)
	}
	var expires time.Time
	if body.Expiration > 0 {
		expires = time.UnixMilli(body.Expiration)
	}
	return body.Token, expires, nil
}
