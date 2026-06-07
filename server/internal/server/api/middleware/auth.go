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

type SessionData struct {
	User    *repository.User
	Session *repository.Session
	Tokens  *session.TwitchTokens
}

func Auth(sessionMgr *session.Manager, repo repository.Repository, tokenProvider *SessionTokenProvider, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			sess, err := sessionMgr.Get(ctx, r)
			if err != nil || sess == nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			// Fresh role check: reload the user each request.
			user, err := repo.GetUser(ctx, sess.UserID)
			if err != nil {
				log.Warn("session user not found", "user_id", sess.UserID)
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			tokens, err := sessionMgr.DecryptTokens(sess)
			if err != nil {
				log.Error("failed to decrypt tokens", "error", err)
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			sessionMgr.UpdateActivity(ctx, sess.HashedID)

			ctx = tokenProvider.Bind(ctx, sess.HashedID, tokens)
			ctx = context.WithValue(ctx, ctxKeyUser, user)
			ctx = context.WithValue(ctx, ctxKeySession, sess)
			ctx = context.WithValue(ctx, ctxKeyTokens, tokens)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetUser(ctx context.Context) *repository.User {
	u, _ := ctx.Value(ctxKeyUser).(*repository.User)
	return u
}

// WithUser returns ctx carrying user, as the auth middleware does on a real
// request. Exposed so handler tests can exercise authed procedures (and the
// RequireUser guard) without standing up the full middleware chain.
func WithUser(ctx context.Context, user *repository.User) context.Context {
	return context.WithValue(ctx, ctxKeyUser, user)
}

func GetTokens(ctx context.Context) *session.TwitchTokens {
	t, _ := ctx.Value(ctxKeyTokens).(*session.TwitchTokens)
	return t
}

func GetSession(ctx context.Context) *repository.Session {
	s, _ := ctx.Value(ctxKeySession).(*repository.Session)
	return s
}
