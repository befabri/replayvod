package downloader

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// TokenRefresher exchanges a long-lived refresh token for a
// short-lived access token via Twitch's OAuth endpoint. The
// downloader holds a reference to one (via Service.SetOAuthRefresher)
// so the Phase 6c authenticated-playback path can use the
// service account without taking a hard dependency on the Helix
// client package.
//
// Returns the access token and its absolute expiry. An empty
// access token + nil error is treated the same as an error —
// the serviceAccount falls back to anonymous playback.
type TokenRefresher func(ctx context.Context, refreshToken string) (accessToken string, expiresAt time.Time, err error)

// serviceAccount caches a service-account access token derived
// from the configured refresh token. One instance per Service.
//
// Cache policy:
//   - access token valid + not within 60s of expiry → return cached
//   - otherwise → single-flight refresh via TokenRefresher
//   - refresh failures fall back to "" (anonymous playback) so a
//     transient OAuth outage doesn't kill every download
type serviceAccount struct {
	refreshToken string
	log          *slog.Logger

	mu        sync.Mutex
	refresher TokenRefresher
	access    string
	expires   time.Time
	inflight  *inflightRefresh
}

type inflightRefresh struct {
	done  chan struct{}
	token string
	err   error
}

// newServiceAccount constructs a cache. refreshToken empty means
// "no service account configured" — Token always returns "".
func newServiceAccount(refreshToken string, log *slog.Logger) *serviceAccount {
	return &serviceAccount{
		refreshToken: refreshToken,
		log:          log.With("domain", "downloader.svcacct"),
	}
}

// setRefresher wires in the token-exchange callback. Safe to
// call at any time.
//
// Mid-refresh semantics: a concurrent Token call that's already
// past its single-flight "take ownership of inflight" point
// snapshots the OLD refresher under lock and completes with it.
// Only subsequent Token calls see the new refresher. In practice
// setRefresher is called once at server startup before any
// Token call fires, so the concurrent path is only exercised
// under test — but the snapshot ensures correctness either way.
func (sa *serviceAccount) setRefresher(r TokenRefresher) {
	sa.mu.Lock()
	sa.refresher = r
	sa.mu.Unlock()
}

// configured reports whether service-account playback is
// enabled. resolveVariantURL uses this to skip the access-token
// plumbing when the operator hasn't set TWITCH_SERVICE_ACCOUNT_REFRESH_TOKEN.
func (sa *serviceAccount) configured() bool {
	return sa != nil && sa.refreshToken != ""
}

// Token returns an access token suitable for the Authorization
// OAuth header on GQL requests. Empty string means "fall back to
// anonymous" — either the service account isn't configured, or
// the refresh attempt failed and the caller should proceed
// without authentication rather than fail the job.
//
// Refresh failures are logged at Warn so operators notice
// misconfigured refresh tokens but the pipeline keeps running.
func (sa *serviceAccount) Token(ctx context.Context) string {
	if !sa.configured() {
		return ""
	}

	sa.mu.Lock()
	// 60-second slack so a token that's about to expire mid-
	// request doesn't force the orchestrator's auth-refresh
	// path to invalidate it immediately.
	if sa.access != "" && time.Now().Add(60*time.Second).Before(sa.expires) {
		tok := sa.access
		sa.mu.Unlock()
		return tok
	}
	// Single-flight: if a refresh is in progress, wait on it.
	if sa.inflight != nil {
		flight := sa.inflight
		sa.mu.Unlock()
		select {
		case <-flight.done:
			return flight.token
		case <-ctx.Done():
			return ""
		}
	}
	refresher := sa.refresher
	if refresher == nil {
		sa.mu.Unlock()
		sa.log.Warn("service account configured but no refresher wired; falling back to anonymous")
		return ""
	}
	flight := &inflightRefresh{done: make(chan struct{})}
	sa.inflight = flight
	refreshToken := sa.refreshToken
	sa.mu.Unlock()

	access, expires, err := refresher(ctx, refreshToken)

	sa.mu.Lock()
	flight.err = err
	if err != nil || access == "" {
		flight.token = ""
		if err != nil {
			sa.log.Warn("service account token refresh failed; falling back to anonymous",
				"error", err)
		}
	} else {
		sa.access = access
		sa.expires = expires
		flight.token = access
	}
	sa.inflight = nil
	sa.mu.Unlock()
	close(flight.done)

	return flight.token
}

