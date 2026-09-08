// Package playbackauth manages the Twitch website session used for recordings.
// It is separate from the OAuth grant used to sign in to ReplayVOD.
package playbackauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/twitch"
)

var (
	ErrInvalidInput = errors.New("paste only the Twitch auth-token cookie value")
	ErrRejected     = errors.New("the Twitch session has expired or been revoked; reconnect with a fresh auth-token cookie")
	ErrWrongClient  = errors.New("this is not a Twitch website session; use the auth-token cookie from twitch.tv, not a Twitch Connect token")
	ErrChanged      = errors.New("the Twitch playback connection changed; retry playback resolution")
	ErrUnavailable  = errors.New("Twitch session validation is temporarily unavailable; try again")
)

var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{20,512}$`)

// Normalize accepts the cookie value or the single auth-token=value pair.
// Whole cookie headers, exports and scripts are deliberately rejected: the
// application only needs this one credential.
func Normalize(input string) (string, error) {
	token := strings.TrimSpace(input)
	token = strings.TrimPrefix(token, "auth-token=")
	if !tokenPattern.MatchString(token) {
		return "", ErrInvalidInput
	}
	return token, nil
}

type Identity struct {
	UserID    string
	Login     string
	ExpiresAt int64 // Unix seconds; zero means Twitch did not supply an expiry.
}

type Validator interface {
	Validate(context.Context, string) (Identity, error)
}

type TwitchValidator struct {
	client *http.Client
	url    string
}

func NewTwitchValidator() *TwitchValidator {
	return &TwitchValidator{
		client: &http.Client{
			Timeout: 15 * time.Second,
			// Never forward the credential to a redirected destination.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		url: "https://id.twitch.tv/oauth2/validate",
	}
}

func (v *TwitchValidator) Validate(ctx context.Context, token string) (Identity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.url, nil)
	if err != nil {
		return Identity{}, ErrUnavailable
	}
	req.Header.Set("Authorization", "OAuth "+token)
	resp, err := v.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Identity{}, ctx.Err()
		}
		return Identity{}, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return Identity{}, ErrRejected
	}
	if resp.StatusCode != http.StatusOK {
		return Identity{}, &UnavailableError{RetryAfter: retryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	var result struct {
		ClientID  string `json:"client_id"`
		UserID    string `json:"user_id"`
		Login     string `json:"login"`
		ExpiresIn *int64 `json:"expires_in"`
	}
	// Never include Twitch's response body or a supplied credential in errors.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&result); err != nil {
		return Identity{}, ErrUnavailable
	}
	if result.ClientID == "" || result.UserID == "" || result.Login == "" || result.ExpiresIn == nil || *result.ExpiresIn < 0 {
		return Identity{}, ErrUnavailable
	}
	if result.ClientID != twitch.DefaultClientID {
		return Identity{}, ErrWrongClient
	}
	identity := Identity{UserID: result.UserID, Login: result.Login}
	if *result.ExpiresIn > 0 {
		// Bound duration arithmetic on an untrusted response.
		if *result.ExpiresIn > 10*365*24*60*60 {
			return Identity{}, ErrUnavailable
		}
		identity.ExpiresAt = time.Now().Unix() + *result.ExpiresIn
	}
	return identity, nil
}

// UnavailableError carries only safe retry metadata, never Twitch response
// bodies or credentials. A long Retry-After is bounded by the caller's deadline.
type UnavailableError struct{ RetryAfter time.Duration }

func (e *UnavailableError) Error() string { return ErrUnavailable.Error() }
func (e *UnavailableError) Unwrap() error { return ErrUnavailable }
func retryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds > 0 {
			return time.Duration(min(seconds, 86400)) * time.Second
		}
		return 0
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return min(date.Sub(now), 24*time.Hour)
	}
	return 0
}
