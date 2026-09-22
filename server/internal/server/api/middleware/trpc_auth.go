package middleware

import (
	"context"
	"net/http"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/apierr"
	"github.com/befabri/trpcgo"
)

const ctxKeyHTTPRequest contextKey = "http_request"

// RequireUser returns the authenticated user from ctx, or a CodeUnauthorized
// "not authenticated" error when there is none. It replaces the hand-rolled
// `user := GetUser(ctx); if user == nil { ... }` guard duplicated across every
// authed handler.
func RequireUser(ctx context.Context) (*repository.User, error) {
	user := GetUser(ctx)
	if user == nil {
		return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	return user, nil
}

func WithContextCreator(ctx context.Context, r *http.Request) context.Context {
	return context.WithValue(ctx, ctxKeyHTTPRequest, r)
}

func getHTTPRequest(ctx context.Context) *http.Request {
	r, _ := ctx.Value(ctxKeyHTTPRequest).(*http.Request)
	return r
}

func (a *Authenticator) TRPC(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
	return func(ctx context.Context, input any) (any, error) {
		r := getHTTPRequest(ctx)
		if r == nil {
			return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "missing request context")
		}
		ctx, err := a.authenticate(ctx, r)
		if err != nil {
			return nil, apierr.Map(a.log, err, "authenticate",
				apierr.On(errUnauthenticated, trpcgo.CodeUnauthorized))
		}
		return next(ctx, input)
	}
}

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
