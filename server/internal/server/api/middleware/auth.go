package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/session"
)

type contextKey string

const (
	ctxKeyUser    contextKey = "user"
	ctxKeySession contextKey = "session"
	ctxKeyTokens  contextKey = "tokens"
)

// SessionData holds the authenticated user's context data.
type SessionData struct {
	User    *repository.User
	Session *repository.Session
	Tokens  *session.TwitchTokens
}

// Auth returns middleware that validates the session cookie and injects user context.
func Auth(sessionMgr *session.Manager, repo repository.Repository, tokenProvider *SessionTokenProvider, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			sess, err := sessionMgr.Get(ctx, r)
			if err != nil || sess == nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			// Load user from DB (fresh role check)
			user, err := repo.GetUser(ctx, sess.UserID)
			if err != nil {
				log.Warn("session user not found", "user_id", sess.UserID)
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			// Decrypt tokens
			tokens, err := sessionMgr.DecryptTokens(sess)
			if err != nil {
				log.Error("failed to decrypt tokens", "error", err)
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			// Update activity
			sessionMgr.UpdateActivity(ctx, sess.HashedID)

			// Inject into context
			ctx = tokenProvider.Bind(ctx, sess.HashedID, tokens)
			ctx = context.WithValue(ctx, ctxKeyUser, user)
			ctx = context.WithValue(ctx, ctxKeySession, sess)
			ctx = context.WithValue(ctx, ctxKeyTokens, tokens)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUser returns the authenticated user from context.
func GetUser(ctx context.Context) *repository.User {
	u, _ := ctx.Value(ctxKeyUser).(*repository.User)
	return u
}

// GetTokens returns the Twitch tokens from context.
func GetTokens(ctx context.Context) *session.TwitchTokens {
	t, _ := ctx.Value(ctxKeyTokens).(*session.TwitchTokens)
	return t
}

// GetSession returns the session from context.
func GetSession(ctx context.Context) *repository.Session {
	s, _ := ctx.Value(ctxKeySession).(*repository.Session)
	return s
}
