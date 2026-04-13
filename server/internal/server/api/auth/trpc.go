package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/trpcgo"
)

// TRPCHandler is the tRPC-transport adapter for the auth domain. Kept
// thin: DTO conversion, ctx extraction, error-to-trpc-code
// translation, and cookie side-effects (which the service layer stays
// out of because they're a tRPC-context concern).
type TRPCHandler struct {
	svc        *Service
	sessionMgr *session.Manager
	log        *slog.Logger
}

// NewTRPCHandler wires the tRPC auth procedures onto the domain
// Service.
func NewTRPCHandler(svc *Service, sm *session.Manager, log *slog.Logger) *TRPCHandler {
	return &TRPCHandler{
		svc:        svc,
		sessionMgr: sm,
		log:        log.With("domain", "auth-api"),
	}
}

// SessionResponse is the shape returned by auth.session — never
// includes tokens.
type SessionResponse struct {
	UserID          string  `json:"user_id"`
	Login           string  `json:"login"`
	DisplayName     string  `json:"display_name"`
	Email           *string `json:"email,omitempty"`
	ProfileImageURL *string `json:"profile_image_url,omitempty"`
	Role            string  `json:"role" validate:"oneof=viewer admin owner"`
}

// Session returns the current authenticated user. Pure ctx extraction
// — no service call needed.
func (h *TRPCHandler) Session(ctx context.Context) (SessionResponse, error) {
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
func (h *TRPCHandler) Logout(ctx context.Context) (LogoutResult, error) {
	sess := middleware.GetSession(ctx)
	if sess == nil {
		return LogoutResult{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	if err := h.svc.DeleteSession(ctx, sess.HashedID); err != nil {
		h.log.Error("delete session", "error", err)
		return LogoutResult{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to logout")
	}
	trpcgo.SetCookie(ctx, h.sessionMgr.ClearCookie())
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
		h.log.Error("list sessions", "error", err)
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

// RevokeSession deletes a specific session (must belong to the
// current user). ErrSessionNotOwned collapses "exists but not yours"
// and "doesn't exist" to 404 — stops session-enumeration probing.
func (h *TRPCHandler) RevokeSession(ctx context.Context, input RevokeSessionInput) (LogoutResult, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return LogoutResult{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	if err := h.svc.RevokeUserSession(ctx, user.ID, input.HashedID); err != nil {
		if errors.Is(err, ErrSessionNotOwned) {
			return LogoutResult{}, trpcgo.NewError(trpcgo.CodeNotFound, "session not found")
		}
		h.log.Error("revoke session", "error", err)
		return LogoutResult{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to revoke")
	}

	// If revoking the current session, clear the cookie too so the
	// next request is unambiguously unauthenticated.
	current := middleware.GetSession(ctx)
	if current != nil && current.HashedID == input.HashedID {
		trpcgo.SetCookie(ctx, h.sessionMgr.ClearCookie())
	}
	return LogoutResult{OK: true}, nil
}
