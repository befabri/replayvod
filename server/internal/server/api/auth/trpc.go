package auth

import (
	"context"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/server/api/apierr"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/trpcgo"
)

type TRPCHandler struct {
	svc        *Service
	sessionMgr *session.Manager
	log        *slog.Logger
}

func NewTRPCHandler(svc *Service, sm *session.Manager, log *slog.Logger) *TRPCHandler {
	return &TRPCHandler{
		svc:        svc,
		sessionMgr: sm,
		log:        log.With("domain", "auth-api"),
	}
}

type SessionResponse struct {
	UserID          string          `json:"user_id"`
	Login           string          `json:"login"`
	DisplayName     string          `json:"display_name"`
	Email           *string         `json:"email,omitempty"`
	ProfileImageURL *string         `json:"profile_image_url,omitempty"`
	Role            middleware.Role `json:"role"`
}

func (h *TRPCHandler) Session(ctx context.Context) (SessionResponse, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return SessionResponse{}, err
	}
	return SessionResponse{
		UserID:          user.ID,
		Login:           user.Login,
		DisplayName:     user.DisplayName,
		Email:           user.Email,
		ProfileImageURL: user.ProfileImageURL,
		Role:            middleware.Role(user.Role),
	}, nil
}

type LogoutResult struct {
	OK bool `json:"ok"`
}

func (h *TRPCHandler) Logout(ctx context.Context) (LogoutResult, error) {
	sess := middleware.GetSession(ctx)
	if sess == nil {
		return LogoutResult{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	if err := h.svc.DeleteSession(ctx, sess.HashedID); err != nil {
		return LogoutResult{}, apierr.Map(h.log, err, "logout")
	}
	trpcgo.SetCookie(ctx, h.sessionMgr.ClearCookie())
	return LogoutResult{OK: true}, nil
}

type SessionInfo struct {
	HashedID     string    `json:"hashed_id"`
	ExpiresAt    time.Time `json:"expires_at"`
	LastActiveAt time.Time `json:"last_active_at"`
	CreatedAt    time.Time `json:"created_at"`
	UserAgent    *string   `json:"user_agent,omitempty"`
	IPAddress    *string   `json:"ip_address,omitempty"`
	Current      bool      `json:"current"`
}

// ListSessions returns every active session for the current user. The
// service layer doesn't know which one is current (it's ctx-bound) —
// we mark it here at the transport layer.
func (h *TRPCHandler) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	user := middleware.GetUser(ctx)
	current := middleware.GetSession(ctx)
	if user == nil || current == nil {
		return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	rows, err := h.svc.ListSessionsForUser(ctx, user.ID)
	if err != nil {
		return nil, apierr.Map(h.log, err, "list sessions")
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

type RevokeSessionInput struct {
	HashedID string `json:"hashed_id" validate:"required"`
}

// RevokeSession deletes a specific session (must belong to the
// current user). ErrSessionNotOwned collapses "exists but not yours"
// and "doesn't exist" to 404 — stops session-enumeration probing.
func (h *TRPCHandler) RevokeSession(ctx context.Context, input RevokeSessionInput) (LogoutResult, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return LogoutResult{}, err
	}
	if err := h.svc.RevokeUserSession(ctx, user.ID, input.HashedID); err != nil {
		return LogoutResult{}, apierr.Map(h.log, err, "revoke session",
			apierr.On(ErrSessionNotOwned, trpcgo.CodeNotFound, "session not found"))
	}

	// If revoking the current session, clear the cookie too so the
	// next request is unambiguously unauthenticated.
	current := middleware.GetSession(ctx)
	if current != nil && current.HashedID == input.HashedID {
		trpcgo.SetCookie(ctx, h.sessionMgr.ClearCookie())
	}
	return LogoutResult{OK: true}, nil
}
