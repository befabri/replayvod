package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/trpcgo"
)

// ctxKeyHTTPRequest stores the *http.Request for tRPC handlers.
const ctxKeyHTTPRequest contextKey = "http_request"

// WithContextCreator is passed to trpcgo.WithContextCreator.
// It stores the incoming *http.Request in the context so tRPC middleware can access cookies.
func WithContextCreator(ctx context.Context, r *http.Request) context.Context {
	return context.WithValue(ctx, ctxKeyHTTPRequest, r)
}

// getHTTPRequest returns the *http.Request stored in context.
func getHTTPRequest(ctx context.Context) *http.Request {
	r, _ := ctx.Value(ctxKeyHTTPRequest).(*http.Request)
	return r
}

// TRPCAuth returns tRPC middleware that validates the session cookie and injects user context.
func TRPCAuth(sessionMgr *session.Manager, repo repository.Repository, tokenProvider *SessionTokenProvider, log *slog.Logger) trpcgo.Middleware {
	return func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			r := getHTTPRequest(ctx)
			if r == nil {
				return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "missing request context")
			}

			sess, err := sessionMgr.Get(ctx, r)
			if err != nil || sess == nil {
				return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
			}

			user, err := repo.GetUser(ctx, sess.UserID)
			if err != nil {
				log.Warn("session user not found", "user_id", sess.UserID)
				return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
			}

			tokens, err := sessionMgr.DecryptTokens(sess)
			if err != nil {
				log.Error("failed to decrypt tokens", "error", err)
				return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
			}

			sessionMgr.UpdateActivity(ctx, sess.HashedID)

			ctx = tokenProvider.Bind(ctx, sess.HashedID, tokens)
			ctx = context.WithValue(ctx, ctxKeyUser, user)
			ctx = context.WithValue(ctx, ctxKeySession, sess)
			ctx = context.WithValue(ctx, ctxKeyTokens, tokens)

			return next(ctx, input)
		}
	}
}

// TRPCRequireRole returns tRPC middleware that enforces a minimum role.
func TRPCRequireRole(minRole string) trpcgo.Middleware {
	minLevel := roleLevel[minRole]

	return func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			user := GetUser(ctx)
			if user == nil {
				return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
			}

			userLevel := roleLevel[user.Role]
			if userLevel < minLevel {
				return nil, trpcgo.NewError(trpcgo.CodeForbidden, "insufficient permissions")
			}

			return next(ctx, input)
		}
	}
}
