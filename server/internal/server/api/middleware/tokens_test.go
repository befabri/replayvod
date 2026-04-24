package middleware

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/twitch"
)

type fakeTokenUpdater struct {
	mu      sync.Mutex
	updates int
	last    *session.TwitchTokens
}

func (f *fakeTokenUpdater) UpdateTokens(_ context.Context, _ string, tokens *session.TwitchTokens) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates++
	copy := *tokens
	f.last = &copy
	return nil
}

type fakeTokenRefresher struct {
	mu      sync.Mutex
	calls   int
	resp    *twitch.TokenResponse
	release chan struct{}
	started chan struct{}
}

func (f *fakeTokenRefresher) RefreshUserToken(_ context.Context, _ string) (*twitch.TokenResponse, error) {
	f.mu.Lock()
	f.calls++
	release := f.release
	started := f.started
	resp := f.resp
	f.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if release != nil {
		<-release
	}
	return resp, nil
}

type fakeLatestTimer struct {
	stopped bool
}

func (f *fakeLatestTimer) Stop() bool {
	f.stopped = true
	return true
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSessionTokenProviderSingleFlight(t *testing.T) {
	updater := &fakeTokenUpdater{}
	refresher := &fakeTokenRefresher{
		resp:    &twitch.TokenResponse{AccessToken: "fresh", RefreshToken: "refresh-2", ExpiresIn: 3600},
		release: make(chan struct{}),
		started: make(chan struct{}, 1),
	}
	provider := NewSessionTokenProvider(updater, refresher, testLogger())
	stale := &session.TwitchTokens{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}

	const waiters = 8
	results := make(chan string, waiters)
	errCh := make(chan error, waiters)
	gate := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		tokens, err := provider.validTokens(context.Background(), "sess-1", stale, true)
		if err != nil {
			errCh <- err
			return
		}
		results <- tokens.AccessToken
	}()
	<-refresher.started
	for range waiters - 1 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			tokens, err := provider.validTokens(context.Background(), "sess-1", stale, true)
			if err != nil {
				errCh <- err
				return
			}
			results <- tokens.AccessToken
		}()
	}
	close(gate)
	time.Sleep(10 * time.Millisecond)
	close(refresher.release)
	wg.Wait()
	close(results)
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("validTokens error: %v", err)
		}
	}
	for token := range results {
		if token != "fresh" {
			t.Fatalf("token = %q, want fresh", token)
		}
	}
	if refresher.calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", refresher.calls)
	}
	if updater.updates != 1 {
		t.Fatalf("updates = %d, want 1", updater.updates)
	}
	if updater.last == nil || updater.last.RefreshToken != "refresh-2" {
		t.Fatalf("persisted refresh token = %#v, want refresh-2", updater.last)
	}
}

func TestSessionTokenProviderReusesLatestRefreshedTokens(t *testing.T) {
	// A force=true caller refreshes once and populates the cache.
	// A subsequent non-force caller with a stale session copy gets
	// the cached tokens instead of driving another refresh — the
	// cache's reason for existing. (Force callers always bypass the
	// cache, covered by TestSessionTokenProviderForceBypassesLatestCache.)
	updater := &fakeTokenUpdater{}
	refresher := &fakeTokenRefresher{
		resp: &twitch.TokenResponse{AccessToken: "fresh", RefreshToken: "refresh-2", ExpiresIn: 3600},
	}
	provider := NewSessionTokenProvider(updater, refresher, testLogger())
	stale := &session.TwitchTokens{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}

	first, err := provider.validTokens(context.Background(), "sess-1", stale, true)
	if err != nil {
		t.Fatalf("first validTokens: %v", err)
	}
	if first.AccessToken != "fresh" {
		t.Fatalf("first access token = %q, want fresh", first.AccessToken)
	}

	second, err := provider.validTokens(context.Background(), "sess-1", stale, false)
	if err != nil {
		t.Fatalf("second validTokens: %v", err)
	}
	if second.AccessToken != "fresh" || second.RefreshToken != "refresh-2" {
		t.Fatalf("second tokens = %#v, want refreshed tokens", second)
	}
	if refresher.calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", refresher.calls)
	}
	if updater.updates != 1 {
		t.Fatalf("updates = %d, want 1", updater.updates)
	}
}

func TestSessionTokenProviderExpiresLatestReuseEntry(t *testing.T) {
	provider := NewSessionTokenProvider(&fakeTokenUpdater{}, &fakeTokenRefresher{}, testLogger())
	provider.latest["sess-1"] = &latestTokensEntry{
		tokens: &session.TwitchTokens{
			AccessToken:  "fresh",
			RefreshToken: "refresh-2",
			ExpiresAt:    time.Now().Add(time.Hour),
		},
		storedAt: time.Now().Add(-(latestTokenReuseWindow + time.Second)),
	}
	stale := &session.TwitchTokens{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}

	got := provider.latestTokens("sess-1", stale)
	if got != nil {
		t.Fatalf("latestTokens = %#v, want nil after reuse window", got)
	}
	if _, ok := provider.latest["sess-1"]; ok {
		t.Fatal("latest entry should be evicted after reuse window")
	}
}

func TestSessionTokenProviderTimerDeletesLatestReuseEntry(t *testing.T) {
	provider := NewSessionTokenProvider(&fakeTokenUpdater{}, &fakeTokenRefresher{}, testLogger())
	var expire func()
	provider.afterFunc = func(d time.Duration, f func()) latestTokenTimer {
		if d != latestTokenReuseWindow {
			t.Fatalf("timer duration = %s, want %s", d, latestTokenReuseWindow)
		}
		expire = f
		return &fakeLatestTimer{}
	}
	tokens := &session.TwitchTokens{
		AccessToken:  "fresh",
		RefreshToken: "refresh-2",
		ExpiresAt:    time.Now().Add(time.Hour),
	}

	provider.mu.Lock()
	provider.storeLatestLocked("sess-1", tokens)
	provider.mu.Unlock()
	if expire == nil {
		t.Fatal("latest token timer was not scheduled")
	}

	expire()

	provider.mu.Lock()
	_, ok := provider.latest["sess-1"]
	provider.mu.Unlock()
	if ok {
		t.Fatal("latest entry should be deleted by timer callback")
	}
}

func TestSessionTokenProviderPrefersCurrentValidTokensOverOlderLatestCache(t *testing.T) {
	provider := NewSessionTokenProvider(&fakeTokenUpdater{}, &fakeTokenRefresher{}, testLogger())
	provider.latest["sess-1"] = &latestTokensEntry{
		tokens: &session.TwitchTokens{
			AccessToken:  "cached-token",
			RefreshToken: "cached-refresh",
			ExpiresAt:    time.Now().Add(20 * time.Minute),
		},
		storedAt: time.Now(),
	}
	current := &session.TwitchTokens{
		AccessToken:  "current-token",
		RefreshToken: "current-refresh",
		ExpiresAt:    time.Now().Add(45 * time.Minute),
	}

	got, err := provider.validTokens(context.Background(), "sess-1", current, false)
	if err != nil {
		t.Fatalf("validTokens: %v", err)
	}
	if got != current {
		t.Fatalf("validTokens returned %#v, want current session tokens", got)
	}
}

func TestSessionTokenProviderForceBypassesLatestCache(t *testing.T) {
	// force=true means a Helix call just 401'd, so a cached token
	// may be the exact revoked value Twitch rejected. The provider
	// must drive a real refresh rather than handing back the cache.
	updater := &fakeTokenUpdater{}
	refresher := &fakeTokenRefresher{
		resp: &twitch.TokenResponse{AccessToken: "refreshed-token", RefreshToken: "refreshed-refresh", ExpiresIn: 3600},
	}
	provider := NewSessionTokenProvider(updater, refresher, testLogger())
	provider.latest["sess-1"] = &latestTokensEntry{
		tokens: &session.TwitchTokens{
			AccessToken:  "cached-token",
			RefreshToken: "cached-refresh",
			ExpiresAt:    time.Now().Add(20 * time.Minute),
		},
		storedAt: time.Now(),
	}
	stale := &session.TwitchTokens{
		AccessToken:  "stale-token",
		RefreshToken: "stale-refresh",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}

	got, err := provider.validTokens(context.Background(), "sess-1", stale, true)
	if err != nil {
		t.Fatalf("validTokens: %v", err)
	}
	if got.AccessToken != "refreshed-token" || got.RefreshToken != "refreshed-refresh" {
		t.Fatalf("validTokens returned %#v, want freshly refreshed tokens (not the cached entry)", got)
	}
	if refresher.calls != 1 {
		t.Fatalf("refresher.calls = %d, want 1 (force must bypass the cache and refresh)", refresher.calls)
	}
}
