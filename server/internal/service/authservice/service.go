// Package authservice owns the authentication business logic:
// OAuth callback flow (code exchange + whitelist + role determination
// + user upsert), session lifecycle (delete, list, revoke), and the
// invariants that gate them.
//
// Transport-agnostic: no tRPC or HTTP types cross this boundary.
// routes/auth/ wraps the service with Chi handlers (OAuth flow) and
// tRPC procedures (session management). A hypothetical public API in
// cmd/public-api/ would import authservice directly.
package authservice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// ErrSessionNotOwned is returned when a user tries to revoke a session
// that isn't theirs. Transport maps this to 404 rather than 403 so
// session-enumeration probes can't distinguish "exists but not yours"
// from "doesn't exist."
var ErrSessionNotOwned = errors.New("authservice: session not owned by user")

// ErrLoginDenied is returned when a whitelist check rejects the user
// during OAuth callback. The Reason field is URL-safe and the
// transport layer forwards it in a redirect query string.
type ErrLoginDenied struct {
	Reason string
}

func (e *ErrLoginDenied) Error() string { return "authservice: login denied: " + e.Reason }

// Config collects the environment-driven knobs the auth flow needs.
// Kept narrow so authservice doesn't depend on the full *config.Config
// — makes unit tests trivial to construct.
type Config struct {
	// WhitelistEnabled gates the IsWhitelisted check during login.
	// When false, any Twitch user who completes OAuth can log in.
	WhitelistEnabled bool
	// OwnerTwitchID bootstraps the owner role on first login. Empty
	// falls back to "first user wins" — the first successful upsert
	// becomes owner.
	OwnerTwitchID string
}

// Service is the auth business-logic handle.
type Service struct {
	repo       repository.Repository
	sessionMgr *session.Manager
	twitch     *twitch.Client
	cfg        Config
	log        *slog.Logger
}

// New builds the service.
func New(repo repository.Repository, sm *session.Manager, tc *twitch.Client, cfg Config, log *slog.Logger) *Service {
	return &Service{
		repo:       repo,
		sessionMgr: sm,
		twitch:     tc,
		cfg:        cfg,
		log:        log.With("domain", "auth"),
	}
}

// DeleteSession deletes a session by its hashed ID. Caller is expected
// to already hold the session (logout path) — no ownership check here,
// see RevokeUserSession for the user-initiated revoke.
func (s *Service) DeleteSession(ctx context.Context, hashedID string) error {
	return s.sessionMgr.DeleteByHash(ctx, hashedID)
}

// ListSessionsForUser returns every active session belonging to the
// given user ID. Caller (transport) marks which row is the current
// one; the service doesn't know about ctx-bound session state.
func (s *Service) ListSessionsForUser(ctx context.Context, userID string) ([]repository.SessionInfo, error) {
	return s.repo.ListUserSessions(ctx, userID)
}

// RevokeUserSession deletes hashedID if it belongs to userID. Returns
// ErrSessionNotOwned when the session isn't in the user's list (either
// it doesn't exist at all or belongs to a different user — collapsed
// to one error for enumeration safety).
func (s *Service) RevokeUserSession(ctx context.Context, userID, hashedID string) error {
	rows, err := s.repo.ListUserSessions(ctx, userID)
	if err != nil {
		return fmt.Errorf("list user sessions: %w", err)
	}
	for _, row := range rows {
		if row.HashedID == hashedID {
			return s.sessionMgr.DeleteByHash(ctx, hashedID)
		}
	}
	return ErrSessionNotOwned
}

// LoginResult is the OAuth callback's success payload. Transport
// takes care of the cookie + redirect; this is the information the
// service commits to.
type LoginResult struct {
	User   *repository.User
	Tokens *session.TwitchTokens
}

// HandleOAuthCallback runs the full code-exchange → whitelist → role →
// upsert flow for the Twitch OAuth callback. Transport layers in
// routes/auth/handler.go extract code+codeVerifier from cookies +
// query string, call this, and convert the result or ErrLoginDenied
// into a redirect.
//
// redirectURI must match exactly what the authorize URL set for the
// code exchange — Twitch rejects mismatches with a cryptic error,
// which is the class of bug this signature makes unambiguous.
func (s *Service) HandleOAuthCallback(ctx context.Context, code, redirectURI, codeVerifier string) (*LoginResult, error) {
	tokenResp, err := s.twitch.ExchangeCode(ctx, code, redirectURI, codeVerifier)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}

	users, err := s.twitch.GetUsers(twitch.WithUserToken(ctx, tokenResp.AccessToken), nil)
	if err != nil {
		return nil, fmt.Errorf("fetch twitch user: %w", err)
	}
	if len(users) == 0 {
		return nil, errors.New("twitch returned no user data")
	}
	tu := users[0]

	if s.cfg.WhitelistEnabled {
		ok, err := s.repo.IsWhitelisted(ctx, tu.ID)
		if err != nil {
			return nil, fmt.Errorf("whitelist check: %w", err)
		}
		if !ok {
			s.log.Info("user not whitelisted", "twitch_id", tu.ID, "login", tu.Login)
			return nil, &ErrLoginDenied{Reason: "not_whitelisted"}
		}
	}

	role := s.resolveRole(ctx, tu.ID)

	// Preserve existing role when the user already exists — Twitch-side
	// role sync would overwrite dashboard-granted promotions otherwise.
	if existing, err := s.repo.GetUser(ctx, tu.ID); err == nil && existing != nil {
		role = existing.Role
	}

	upserted, err := s.repo.UpsertUser(ctx, &repository.User{
		ID:              tu.ID,
		Login:           tu.Login,
		DisplayName:     tu.DisplayName,
		Email:           stringOrNil(tu.Email),
		ProfileImageURL: stringOrNil(tu.ProfileImageURL),
		Role:            role,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}

	s.log.Info("user authenticated", "twitch_id", upserted.ID, "login", upserted.Login, "role", upserted.Role)

	return &LoginResult{
		User: upserted,
		Tokens: &session.TwitchTokens{
			AccessToken:  tokenResp.AccessToken,
			RefreshToken: tokenResp.RefreshToken,
			ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		},
	}, nil
}

// resolveRole decides what role a freshly-logging-in user gets when
// we haven't seen them before: OwnerTwitchID takes precedence, then
// "first user wins" owner bootstrap, else plain viewer. Existing
// users keep their stored role — that check lives in the caller.
func (s *Service) resolveRole(ctx context.Context, twitchID string) string {
	if s.cfg.OwnerTwitchID != "" && twitchID == s.cfg.OwnerTwitchID {
		return "owner"
	}
	users, err := s.repo.ListUsers(ctx)
	if err == nil && len(users) == 0 {
		return "owner"
	}
	return "viewer"
}

func stringOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
