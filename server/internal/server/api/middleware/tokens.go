package middleware

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/twitch"
)

const sessionTokenRefreshSkew = time.Minute
const latestTokenReuseWindow = 2 * time.Minute

// userTokenRefresher is the narrow Twitch client surface auth middleware needs.
type userTokenRefresher interface {
	RefreshUserToken(ctx context.Context, refreshToken string) (*twitch.TokenResponse, error)
}

type sessionTokenUpdater interface {
	UpdateTokens(ctx context.Context, hashedID string, tokens *session.TwitchTokens) error
}

type refreshCall struct {
	done   chan struct{}
	tokens *session.TwitchTokens
	err    error
}

type latestTokenTimer interface {
	Stop() bool
}

type latestTokensEntry struct {
	tokens   *session.TwitchTokens
	storedAt time.Time
	timer    latestTokenTimer
}

// SessionTokenProvider coordinates request-scoped Twitch user-token refreshes
// and collapses concurrent refreshes for the same session into a single Twitch
// token-exchange call.
type SessionTokenProvider struct {
	updater   sessionTokenUpdater
	refresher userTokenRefresher
	log       *slog.Logger

	mu        sync.Mutex
	inflight  map[string]*refreshCall
	latest    map[string]*latestTokensEntry
	afterFunc func(time.Duration, func()) latestTokenTimer
}

func NewSessionTokenProvider(updater sessionTokenUpdater, refresher userTokenRefresher, log *slog.Logger) *SessionTokenProvider {
	return &SessionTokenProvider{
		updater:   updater,
		refresher: refresher,
		log:       log,
		inflight:  make(map[string]*refreshCall),
		latest:    make(map[string]*latestTokensEntry),
		afterFunc: func(d time.Duration, f func()) latestTokenTimer { return time.AfterFunc(d, f) },
	}
}

func (p *SessionTokenProvider) Bind(ctx context.Context, sessionID string, tokens *session.TwitchTokens) context.Context {
	if p == nil || tokens == nil || sessionID == "" {
		return ctx
	}
	return twitch.WithUserTokenProvider(ctx, &boundSessionTokenProvider{
		provider:  p,
		sessionID: sessionID,
		tokens:    tokens,
	})
}

type boundSessionTokenProvider struct {
	provider  *SessionTokenProvider
	sessionID string
	tokens    *session.TwitchTokens
}

func (b *boundSessionTokenProvider) AccessToken(ctx context.Context, force bool) (string, error) {
	tokens, err := b.provider.validTokens(ctx, b.sessionID, b.tokens, force)
	if err != nil {
		return "", err
	}
	if tokens == nil || tokens.AccessToken == "" {
		return "", &twitch.UserAuthError{Cause: fmt.Errorf("missing session access token")}
	}
	*b.tokens = *tokens
	return tokens.AccessToken, nil
}

func (p *SessionTokenProvider) validTokens(ctx context.Context, sessionID string, tokens *session.TwitchTokens, force bool) (*session.TwitchTokens, error) {
	if tokens == nil {
		return nil, &twitch.UserAuthError{Cause: fmt.Errorf("missing session tokens")}
	}
	if !force && tokens.AccessToken != "" && !tokensExpiredSoon(tokens.ExpiresAt) {
		return tokens, nil
	}
	// force=true means a Helix call just failed with 401/403 on the
	// token we handed out. The cache is populated by past refreshes
	// and stays valid for latestTokenReuseWindow, so a reuse here
	// could serve the exact token Twitch just rejected (if the cache
	// entry has been invalidated server-side). Skip the cache on
	// force and always drive a real refresh; startRefresh coalesces
	// concurrent force callers into a single Twitch round-trip, so
	// the extra cost is one refresh per 401 event.
	if !force {
		if refreshed := p.latestTokens(sessionID, tokens); refreshed != nil {
			return refreshed, nil
		}
	}
	if p == nil || p.refresher == nil || p.updater == nil {
		if force {
			return nil, &twitch.UserAuthError{Cause: fmt.Errorf("session token refresh unavailable")}
		}
		return tokens, nil
	}
	if tokens.RefreshToken == "" {
		if force {
			return nil, &twitch.UserAuthError{Cause: fmt.Errorf("missing session refresh token")}
		}
		return tokens, nil
	}

	call, owner := p.startRefresh(sessionID)
	if owner {
		call.tokens, call.err = p.refreshTokens(ctx, sessionID, tokens)
		close(call.done)
		p.finishRefresh(sessionID)
	} else {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-call.done:
		}
	}
	if call.err != nil {
		return nil, call.err
	}
	if call.tokens == nil {
		return nil, &twitch.UserAuthError{Cause: fmt.Errorf("empty refreshed session tokens")}
	}
	return call.tokens, nil
}

func (p *SessionTokenProvider) latestTokens(sessionID string, current *session.TwitchTokens) *session.TwitchTokens {
	p.mu.Lock()
	defer p.mu.Unlock()
	latest := p.latest[sessionID]
	if latest == nil || latest.tokens == nil {
		return nil
	}
	if time.Since(latest.storedAt) > latestTokenReuseWindow || tokensExpiredSoon(latest.tokens.ExpiresAt) {
		p.deleteLatestLocked(sessionID)
		return nil
	}
	if !shouldPreferLatestTokens(current, latest.tokens) {
		return nil
	}
	copy := *latest.tokens
	return &copy
}

func shouldPreferLatestTokens(current, latest *session.TwitchTokens) bool {
	if latest == nil {
		return false
	}
	if current == nil {
		return true
	}
	if current.AccessToken == latest.AccessToken && current.RefreshToken == latest.RefreshToken && current.ExpiresAt.Equal(latest.ExpiresAt) {
		return false
	}
	if current.AccessToken == "" || tokensExpiredSoon(current.ExpiresAt) {
		return true
	}
	return latest.ExpiresAt.After(current.ExpiresAt)
}

func (p *SessionTokenProvider) startRefresh(sessionID string) (*refreshCall, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if call, ok := p.inflight[sessionID]; ok {
		return call, false
	}
	call := &refreshCall{done: make(chan struct{})}
	p.inflight[sessionID] = call
	return call, true
}

func (p *SessionTokenProvider) finishRefresh(sessionID string) {
	p.mu.Lock()
	delete(p.inflight, sessionID)
	p.mu.Unlock()
}

func (p *SessionTokenProvider) refreshTokens(ctx context.Context, sessionID string, tokens *session.TwitchTokens) (*session.TwitchTokens, error) {
	resp, err := p.refresher.RefreshUserToken(ctx, tokens.RefreshToken)
	if err != nil {
		p.log.Warn("refresh session twitch token", "session_id", sessionID, "error", err)
		expired := false
		var tokenErr *twitch.TokenRequestError
		if errors.As(err, &tokenErr) && (tokenErr.Status == 400 || tokenErr.Status == 401) {
			expired = true
		}
		return nil, &twitch.UserAuthError{Expired: expired, Cause: err}
	}
	refreshed := &session.TwitchTokens{
		AccessToken:  resp.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second),
	}
	if resp.RefreshToken != "" {
		refreshed.RefreshToken = resp.RefreshToken
	}
	if err := p.updater.UpdateTokens(ctx, sessionID, refreshed); err != nil {
		p.log.Warn("persist refreshed session twitch token", "session_id", sessionID, "error", err)
		return nil, err
	}
	p.mu.Lock()
	p.storeLatestLocked(sessionID, refreshed)
	p.mu.Unlock()
	return refreshed, nil
}

func (p *SessionTokenProvider) storeLatestLocked(sessionID string, tokens *session.TwitchTokens) {
	p.deleteLatestLocked(sessionID)
	copy := *tokens
	entry := &latestTokensEntry{tokens: &copy, storedAt: time.Now()}
	entry.timer = p.afterFunc(latestTokenReuseWindow, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.latest[sessionID] == entry {
			delete(p.latest, sessionID)
		}
	})
	p.latest[sessionID] = entry
}

func (p *SessionTokenProvider) deleteLatestLocked(sessionID string) {
	entry := p.latest[sessionID]
	if entry != nil && entry.timer != nil {
		entry.timer.Stop()
	}
	delete(p.latest, sessionID)
}

func tokensExpiredSoon(expiresAt time.Time) bool {
	if expiresAt.IsZero() {
		return true
	}
	return !time.Now().Before(expiresAt.Add(-sessionTokenRefreshSkew))
}
