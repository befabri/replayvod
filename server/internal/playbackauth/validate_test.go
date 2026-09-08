package playbackauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/twitch"
)

const testToken = "test-session-0123456789abcdef"

func TestNormalize(t *testing.T) {
	for _, input := range []string{testToken, " \n" + testToken + "\n", "auth-token=" + testToken} {
		got, err := Normalize(input)
		if err != nil || got != testToken {
			t.Fatalf("valid cookie rejected: %v", err)
		}
	}
	for _, input := range []string{"", "short", "Cookie: auth-token=" + testToken, "auth-token=" + testToken + "; other=value", testToken + "\r\nX-Header: value", `{"auth-token":"` + testToken + `"}`, strings.Repeat("a", 513)} {
		if _, err := Normalize(input); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("invalid cookie accepted")
		}
	}
}

func TestTwitchValidator(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
		want error
	}{
		{"website session with unknown expiry", 200, `{"client_id":"` + twitch.DefaultClientID + `","user_id":"123","login":"viewer","expires_in":0}`, nil},
		{"app OAuth is not playback", 200, `{"client_id":"replayvod-client","user_id":"123","login":"viewer","expires_in":3600}`, ErrWrongClient},
		{"revoked", 401, testToken, ErrRejected},
		{"rate limited", 429, testToken, ErrUnavailable},
		{"server unavailable", 503, testToken, ErrUnavailable},
		{"malformed", 200, testToken, ErrUnavailable},
		{"missing identity", 200, `{}`, ErrUnavailable},
		{"invalid expiry", 200, `{"client_id":"` + twitch.DefaultClientID + `","user_id":"123","login":"viewer","expires_in":-1}`, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "OAuth "+testToken {
					t.Error("missing session validation header")
				}
				if r.URL.RawQuery != "" {
					t.Error("credential must not be in URL")
				}
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			v := NewTwitchValidator()
			v.url = srv.URL
			got, err := v.Validate(context.Background(), testToken)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if err != nil && strings.Contains(err.Error(), testToken) {
				t.Fatal("credential in error")
			}
			if err == nil && (got.UserID != "123" || got.Login != "viewer" || got.ExpiresAt != 0) {
				t.Fatalf("identity: %+v", got)
			}
		})
	}
}

func TestValidationDoesNotForwardCredentialOnRedirect(t *testing.T) {
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded = true }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer origin.Close()
	v := NewTwitchValidator()
	v.url = origin.URL
	if _, err := v.Validate(context.Background(), testToken); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("redirect = %v", err)
	}
	if forwarded {
		t.Fatal("credential forwarded on redirect")
	}
}

func TestRetryAfter(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"12", 12 * time.Second}, {now.Add(20 * time.Second).Format(http.TimeFormat), 20 * time.Second},
		{"-1", 0}, {"0", 0}, {"invalid", 0}, {now.Add(-time.Second).Format(http.TimeFormat), 0},
		{"9223372036854775807", 24 * time.Hour},
	} {
		if got := retryAfter(tc.value, now); got != tc.want {
			t.Errorf("Retry-After %q=%s, want %s", tc.value, got, tc.want)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	v := NewTwitchValidator()
	v.url = srv.URL
	_, err := v.Validate(context.Background(), testToken)
	var unavailable *UnavailableError
	if !errors.Is(err, ErrUnavailable) || !errors.As(err, &unavailable) || unavailable.RetryAfter != 12*time.Second {
		t.Fatalf("retry metadata: %v", err)
	}
}
