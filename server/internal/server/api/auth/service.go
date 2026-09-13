// Package auth authenticates Twitch users and manages local sessions and access.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/ptr"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// ErrSessionNotOwned covers missing and other users' sessions, preventing ownership enumeration.
var ErrSessionNotOwned = errors.New("auth: session not owned by user")

// ErrLoginDenied rejects an OAuth login; Reason is safe for the redirect query string.
type ErrLoginDenied struct {
	Reason string
}

func (e *ErrLoginDenied) Error() string { return "auth: login denied: " + e.Reason }

type Config struct {
	WhitelistEnabled bool
	// OwnerTwitchID grants the owner role on login; the first user also becomes owner.
	OwnerTwitchID string
}

type Service struct {
	repo       repository.Repository
	sessionMgr *session.Manager
	twitch     *twitch.Client
	cfg        Config
	log        *slog.Logger
}

func New(repo repository.Repository, sm *session.Manager, tc *twitch.Client, cfg Config, log *slog.Logger) *Service {
	return &Service{
		repo:       repo,
		sessionMgr: sm,
		twitch:     tc,
		cfg:        cfg,
		log:        log.With("domain", "auth"),
	}
}

// DeleteSession deletes a session without checking ownership; callers must already hold it.
func (s *Service) DeleteSession(ctx context.Context, hashedID string) error {
	return s.sessionMgr.DeleteByHash(ctx, hashedID)
}

// ListSessionsForUser returns the user's active sessions.
func (s *Service) ListSessionsForUser(ctx context.Context, userID string) ([]repository.SessionInfo, error) {
	return s.repo.ListUserSessions(ctx, userID)
}

// RevokeUserSession deletes a user's session or returns ErrSessionNotOwned.
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

type LoginResult struct {
	User   *repository.User
	Tokens *session.TwitchTokens
}

// HandleOAuthCallback authenticates a Twitch user and returns their account and
// tokens. An empty inviteToken uses ordinary whitelist checks; a valid invite
// grants access and may promote the user, while an invalid invite returns
// ErrLoginDenied. Twitch requires redirectURI to match the authorization
// request exactly.
func (s *Service) HandleOAuthCallback(ctx context.Context, code, redirectURI, codeVerifier, inviteToken string) (*LoginResult, error) {
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

	if s.cfg.WhitelistEnabled && inviteToken == "" {
		ok, err := s.repo.IsWhitelisted(ctx, tu.ID)
		if err != nil {
			return nil, fmt.Errorf("whitelist check: %w", err)
		}
		if !ok {
			s.log.Info("user not whitelisted", "twitch_id", tu.ID, "login", tu.Login)
			return nil, &ErrLoginDenied{Reason: "not_whitelisted"}
		}
	}

	profile := &repository.User{
		ID:              tu.ID,
		Login:           tu.Login,
		DisplayName:     tu.DisplayName,
		Email:           ptr.StringOrNil(tu.Email),
		ProfileImageURL: ptr.StringOrNil(tu.ProfileImageURL),
	}
	var upserted *repository.User
	if inviteToken != "" {
		upserted, err = s.redeemInvite(ctx, inviteToken, profile)
	} else {
		upserted, err = s.upsertOAuthUser(ctx, s.repo, profile)
	}
	if err != nil {
		return nil, err
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

// upsertOAuthUser refreshes the profile without overwriting concurrent role
// changes.
func (s *Service) upsertOAuthUser(ctx context.Context, repo repository.Repository, profile *repository.User) (*repository.User, error) {
	u := *profile
	existing, err := repo.GetUser(ctx, u.ID)
	switch {
	case err == nil:
		u.Role = existing.Role
	case errors.Is(err, repository.ErrNotFound):
		u.Role, err = s.resolveRole(ctx, repo, u.ID)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	upserted, err := repo.UpsertUser(ctx, &u)
	if err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}
	return upserted, nil
}

func (s *Service) resolveRole(ctx context.Context, repo repository.Repository, twitchID string) (string, error) {
	if s.cfg.OwnerTwitchID != "" && twitchID == s.cfg.OwnerTwitchID {
		return "owner", nil
	}
	users, err := repo.ListUsers(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve initial user role: %w", err)
	}
	if len(users) == 0 {
		return "owner", nil
	}
	return "viewer", nil
}
