package auth

import (
	"context"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/trpcgo"
)

// Service handles tRPC auth procedures (session info, logout, session management).
type Service struct {
	repo       repository.Repository
	sessionMgr *session.Manager
	log        *slog.Logger
}

// NewService creates a new auth tRPC service.
func NewService(repo repository.Repository, sm *session.Manager, log *slog.Logger) *Service {
	return &Service{
		repo:       repo,
		sessionMgr: sm,
		log:        log.With("domain", "auth"),
	}
}

// SessionResponse is the shape returned by auth.session — never includes tokens.
type SessionResponse struct {
	UserID          string  `json:"user_id"`
	Login           string  `json:"login"`
	DisplayName     string  `json:"display_name"`
	Email           *string `json:"email,omitempty"`
	ProfileImageURL *string `json:"profile_image_url,omitempty"`
	Role            string  `json:"role" validate:"oneof=viewer admin owner"`
}

// Session returns the current authenticated user.
func (s *Service) Session(ctx context.Context) (SessionResponse, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return SessionResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}

	return SessionResponse{
		UserID:          user.ID,
		Login:           user.Login,
		DisplayName:     user.DisplayName,
		Email:           user.Email,
		ProfileImageURL: user.ProfileImageURL,
		Role:            user.Role,
	}, nil
}

// LogoutResult signals logout success.
type LogoutResult struct {
	OK bool `json:"ok"`
}

// Logout deletes the current session and clears the cookie.
func (s *Service) Logout(ctx context.Context) (LogoutResult, error) {
	sess := middleware.GetSession(ctx)
	if sess == nil {
		return LogoutResult{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}

	if err := s.sessionMgr.DeleteByHash(ctx, sess.HashedID); err != nil {
		s.log.Error("failed to delete session", "error", err)
		return LogoutResult{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to logout")
	}

	trpcgo.SetCookie(ctx, s.sessionMgr.ClearCookie())
	return LogoutResult{OK: true}, nil
}

// SessionInfo is a single active session for the list endpoint.
type SessionInfo struct {
	HashedID     string    `json:"hashed_id"`
	ExpiresAt    time.Time `json:"expires_at"`
	LastActiveAt time.Time `json:"last_active_at"`
	CreatedAt    time.Time `json:"created_at"`
	UserAgent    *string   `json:"user_agent,omitempty"`
	IPAddress    *string   `json:"ip_address,omitempty"`
	Current      bool      `json:"current"`
}

// ListSessions returns all active sessions for the current user.
func (s *Service) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	user := middleware.GetUser(ctx)
	current := middleware.GetSession(ctx)
	if user == nil || current == nil {
		return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}

	rows, err := s.repo.ListUserSessions(ctx, user.ID)
	if err != nil {
		s.log.Error("failed to list sessions", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list sessions")
	}

	out := make([]SessionInfo, len(rows))
	for i, row := range rows {
		out[i] = SessionInfo{
			HashedID:     row.HashedID,
			ExpiresAt:    row.ExpiresAt,
			LastActiveAt: row.LastActiveAt,
			CreatedAt:    row.CreatedAt,
			UserAgent:    row.UserAgent,
			IPAddress:    row.IPAddress,
			Current:      row.HashedID == current.HashedID,
		}
	}
	return out, nil
}

// RevokeSessionInput specifies which session to revoke.
type RevokeSessionInput struct {
	HashedID string `json:"hashed_id" validate:"required"`
}

// RevokeSession deletes a specific session (must belong to the current user).
func (s *Service) RevokeSession(ctx context.Context, input RevokeSessionInput) (LogoutResult, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return LogoutResult{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}

	// Verify the session belongs to this user
	rows, err := s.repo.ListUserSessions(ctx, user.ID)
	if err != nil {
		return LogoutResult{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to verify session")
	}

	owns := false
	for _, row := range rows {
		if row.HashedID == input.HashedID {
			owns = true
			break
		}
	}
	if !owns {
		return LogoutResult{}, trpcgo.NewError(trpcgo.CodeNotFound, "session not found")
	}

	if err := s.sessionMgr.DeleteByHash(ctx, input.HashedID); err != nil {
		s.log.Error("failed to delete session", "error", err)
		return LogoutResult{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to revoke session")
	}

	// If revoking current session, clear cookie too
	current := middleware.GetSession(ctx)
	if current != nil && current.HashedID == input.HashedID {
		trpcgo.SetCookie(ctx, s.sessionMgr.ClearCookie())
	}

	return LogoutResult{OK: true}, nil
}
