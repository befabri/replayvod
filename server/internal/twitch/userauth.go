package twitch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

type userTokenProviderCtxKey struct{}

// UserTokenProvider returns a valid Twitch user access token for the current
// request. force=true means the caller already saw a Helix auth failure and
// wants a forced refresh before retrying once.
type UserTokenProvider interface {
	AccessToken(ctx context.Context, force bool) (string, error)
}

// UserAuthError wraps failures to obtain or refresh a request-scoped Twitch
// user token.
type UserAuthError struct {
	Expired bool
	Cause   error
}

func (e *UserAuthError) Error() string {
	if e == nil || e.Cause == nil {
		return "twitch: user auth failed"
	}
	return fmt.Sprintf("twitch: user auth failed: %v", e.Cause)
}

func (e *UserAuthError) Unwrap() error { return e.Cause }

// WithUserTokenProvider attaches a managed user-token provider to ctx.
func WithUserTokenProvider(ctx context.Context, provider UserTokenProvider) context.Context {
	if provider == nil {
		return ctx
	}
	return context.WithValue(ctx, userTokenProviderCtxKey{}, provider)
}

func userTokenProviderFrom(ctx context.Context) UserTokenProvider {
	p, _ := ctx.Value(userTokenProviderCtxKey{}).(UserTokenProvider)
	return p
}

// IsUserAuthError reports whether err signals a failed user-scoped
// Twitch call that warrants a re-auth response to the caller. It
// matches a UserAuthError{Expired: true} (refresh rejected by Twitch)
// and a HelixError with 401/403 (user token present but Helix refused
// it). Handlers use this to map to Unauthorized instead of InternalError.
func IsUserAuthError(err error) bool {
	var userAuthErr *UserAuthError
	if errors.As(err, &userAuthErr) {
		return userAuthErr.Expired
	}
	var helixErr *HelixError
	if !errors.As(err, &helixErr) {
		return false
	}
	return helixErr.Status == http.StatusUnauthorized || helixErr.Status == http.StatusForbidden
}
