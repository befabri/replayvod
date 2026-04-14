// Package auth owns the authentication domain. It co-locates:
//   - Service: domain logic (OAuth code exchange, whitelist + role
//     resolution, session lifecycle).
//   - Handler: Chi OAuth endpoints (state + PKCE + callback → service).
//   - TRPCHandler: tRPC session procedures (session, logout,
//     sessions, revokeSession).
//
// All three live in the same package so the OAuth flow (Chi) and the
// session surface (tRPC) share the same domain service without
// importing across transport boundaries.
package auth

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
var ErrSessionNotOwned = errors.New("auth: session not owned by user")

// ErrLoginDenied is returned when a whitelist check rejects the user
// during OAuth callback. The Reason field is URL-safe and the
// transport layer forwards it in a redirect query string.
type ErrLoginDenied struct {
	Reason string
}

func (e *ErrLoginDenied) Error() string { return "auth: login denied: " + e.Reason }

// Config collects the environment-driven knobs the auth flow needs.
// Kept narrow so Service doesn't depend on the full *config.Config —
// makes unit tests trivial to construct.
type Config struct {
	// WhitelistEnabled gates the IsWhitelisted check during login.
	// When false, any Twitch user who completes OAuth can log in.
	WhitelistEnabled bool
	// OwnerTwitchID bootstraps the owner role on first login. Empty
	// falls back to "first user wins" — the first successful upsert
	// becomes owner.
	OwnerTwitchID string
}

// Service is the auth domain service.
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
// upsert flow for the Twitch OAuth callback. The Chi handler extracts
// code+codeVerifier from cookies + query string, calls this, and
// converts the result or ErrLoginDenied into a redirect.
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

// SyncUserFollows mirrors the user's Twitch follows into the local
// channels + user_followed_channels tables. Uses the user's access
// token to call GET /channels/followed (paginated), then enriches each
// broadcaster via GET /users (batched at 100/call) so the channels row
// carries profile image, description, etc. — not just id/login/name.
//
// Intended as a best-effort background sync triggered from the OAuth
// callback: v1 did this inline on every login. Safe to call repeatedly
// (upserts keep the mirror fresh on each subsequent login).
func (s *Service) SyncUserFollows(ctx context.Context, userID, accessToken string) error {
	authCtx := twitch.WithUserToken(ctx, accessToken)

	follows, _, err := s.twitch.GetFollowedChannelsAll(authCtx, &twitch.GetFollowedChannelsParams{UserID: userID})
	if err != nil {
		return fmt.Errorf("fetch followed channels: %w", err)
	}
	if len(follows) == 0 {
		return nil
	}

	ids := make([]string, len(follows))
	for i, f := range follows {
		ids[i] = f.BroadcasterID
	}
	users := make(map[string]twitch.User, len(follows))
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		batch, err := s.twitch.GetUsers(authCtx, &twitch.GetUsersParams{ID: ids[start:end]})
		if err != nil {
			return fmt.Errorf("enrich users: %w", err)
		}
		for _, u := range batch {
			users[u.ID] = u
		}
	}

	for _, f := range follows {
		ch := &repository.Channel{
			BroadcasterID:    f.BroadcasterID,
			BroadcasterLogin: f.BroadcasterLogin,
			BroadcasterName:  f.BroadcasterName,
		}
		if u, ok := users[f.BroadcasterID]; ok {
			ch.ProfileImageURL = stringOrNil(u.ProfileImageURL)
			ch.OfflineImageURL = stringOrNil(u.OfflineImageURL)
			ch.Description = stringOrNil(u.Description)
			ch.BroadcasterType = stringOrNil(u.BroadcasterType)
		}
		if _, err := s.repo.UpsertChannel(ctx, ch); err != nil {
			return fmt.Errorf("upsert channel %s: %w", f.BroadcasterID, err)
		}
		if err := s.repo.UpsertUserFollow(ctx, &repository.UserFollow{
			UserID:        userID,
			BroadcasterID: f.BroadcasterID,
			FollowedAt:    f.FollowedAt,
			Followed:      true,
		}); err != nil {
			return fmt.Errorf("upsert user follow %s: %w", f.BroadcasterID, err)
		}
	}

	s.log.Info("synced user follows", "user_id", userID, "count", len(follows))
	return nil
}
