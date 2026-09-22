package middleware

import (
	"context"
	"errors"
	"fmt"
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

// Authenticator shares session validation and context binding across transports.
// Absent, expired, revoked or unreadable credentials produce an unauthenticated
// result; failures reading persisted authentication state remain errors.
type Authenticator struct {
	sessions *session.Manager
	repo     repository.Repository
	tokens   *SessionTokenProvider
	log      *slog.Logger
}

func NewAuthenticator(sessions *session.Manager, repo repository.Repository, tokens *SessionTokenProvider, log *slog.Logger) *Authenticator {
	return &Authenticator{sessions: sessions, repo: repo, tokens: tokens, log: log}
}

var errUnauthenticated = errors.New("not authenticated")

// Match the API's client-closed classification for canceled database reads.
const statusClientClosed = 499

func (a *Authenticator) authenticate(ctx context.Context, r *http.Request) (context.Context, error) {
	sess, err := a.sessions.Get(ctx, r)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, errUnauthenticated
	}

	// Reload the user so role changes take effect on the next request.
	user, err := a.repo.GetUser(ctx, sess.UserID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errUnauthenticated
	}
	if err != nil {
		return nil, fmt.Errorf("get session user: %w", err)
	}
	tokens, err := a.sessions.DecryptTokens(sess)
	if err != nil {
		// The cookie is valid but its stored credential is not, as after a
		// SESSION_SECRET rotation. Revoke it so the next request starts a fresh
		// login instead of failing on every request until the cookie expires.
		a.log.Warn("revoking session with unreadable tokens", "user_id", sess.UserID, "error", err)
		if err := a.sessions.DeleteByHash(ctx, sess.HashedID); err != nil {
			a.log.Error("delete session with unreadable tokens", "error", err)
		}
		return nil, errUnauthenticated
	}
	a.sessions.UpdateActivity(ctx, sess.HashedID)

	ctx = a.tokens.Bind(ctx, sess.HashedID, tokens)
	ctx = context.WithValue(ctx, ctxKeyUser, user)
	ctx = context.WithValue(ctx, ctxKeySession, sess)
	return context.WithValue(ctx, ctxKeyTokens, tokens), nil
}

func (a *Authenticator) HTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, err := a.authenticate(r.Context(), r)
		if errors.Is(err, errUnauthenticated) {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		if err != nil {
			switch {
			case errors.Is(err, context.Canceled):
				w.WriteHeader(statusClientClosed)
				return
			case errors.Is(err, context.DeadlineExceeded):
				a.log.Warn("authentication timed out", "error", err)
				http.Error(w, `{"error":"request timed out"}`, http.StatusRequestTimeout)
				return
			}
			a.log.Error("authentication failed", "error", err)
			http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
