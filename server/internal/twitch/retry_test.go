package twitch

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestShouldRetryHelix(t *testing.T) {
	cases := []struct {
		method string
		status int
		want   bool
	}{
		{http.MethodGet, http.StatusTooManyRequests, true},
		{http.MethodPost, http.StatusTooManyRequests, true}, // 429: rejected before processing, safe for any method
		{http.MethodGet, http.StatusInternalServerError, true},
		{http.MethodGet, http.StatusServiceUnavailable, true},
		{http.MethodDelete, http.StatusBadGateway, true},
		{http.MethodPut, http.StatusGatewayTimeout, true},
		{http.MethodPost, http.StatusInternalServerError, false}, // non-idempotent: never auto-retry a 5xx
		{http.MethodPost, http.StatusServiceUnavailable, false},
		{http.MethodGet, http.StatusNotFound, false},
		{http.MethodGet, http.StatusOK, false},
		{http.MethodGet, http.StatusUnauthorized, false}, // handled by the token-refresh path, not retry
	}
	for _, tc := range cases {
		if got := shouldRetryHelix(tc.method, tc.status); got != tc.want {
			t.Errorf("shouldRetryHelix(%s, %d) = %v, want %v", tc.method, tc.status, got, tc.want)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t.Run("delta seconds", func(t *testing.T) {
		h := http.Header{"Retry-After": []string{"7"}}
		if got := parseRetryAfter(h, now); got != 7*time.Second {
			t.Fatalf("got %v, want 7s", got)
		}
	})
	t.Run("http date", func(t *testing.T) {
		h := http.Header{"Retry-After": []string{now.Add(12 * time.Second).Format(http.TimeFormat)}}
		if got := parseRetryAfter(h, now); got != 12*time.Second {
			t.Fatalf("got %v, want 12s", got)
		}
	})
	t.Run("ratelimit-reset fallback", func(t *testing.T) {
		h := http.Header{"Ratelimit-Reset": []string{strconv.FormatInt(now.Add(5*time.Second).Unix(), 10)}}
		if got := parseRetryAfter(h, now); got != 5*time.Second {
			t.Fatalf("got %v, want 5s", got)
		}
	})
	t.Run("none", func(t *testing.T) {
		if got := parseRetryAfter(http.Header{}, now); got != 0 {
			t.Fatalf("got %v, want 0", got)
		}
	})
	t.Run("past date ignored", func(t *testing.T) {
		h := http.Header{"Retry-After": []string{now.Add(-time.Hour).Format(http.TimeFormat)}}
		if got := parseRetryAfter(h, now); got != 0 {
			t.Fatalf("got %v, want 0", got)
		}
	})
}

func TestRetryDelayHonorsRetryAfterAndCaps(t *testing.T) {
	c := NewClient("id", "secret", slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Retry-After hint wins over exponential backoff.
	if got := c.retryDelay(0, &HelixError{Status: 429, RetryAfter: 3 * time.Second}); got != 3*time.Second {
		t.Fatalf("hint: got %v, want 3s", got)
	}
	// A hint above the cap is clamped.
	if got := c.retryDelay(0, &HelixError{Status: 429, RetryAfter: time.Hour}); got != helixRetryMaxDelay {
		t.Fatalf("cap: got %v, want %v", got, helixRetryMaxDelay)
	}
	// No hint → exponential from the base.
	if got := c.retryDelay(2, &HelixError{Status: 500}); got != c.retryBaseDelay<<2 {
		t.Fatalf("backoff: got %v, want %v", got, c.retryBaseDelay<<2)
	}
}

func TestClientRetriesTransientServerErrorThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	client := NewClient("client-id", "secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	client.retryBaseDelay = 0 // exercise the retry loop without a wall-clock wait
	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":"unavailable"}`)),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"1","login":"l","display_name":"N"}]}`)),
		}, nil
	})}

	ctx := WithUserToken(context.Background(), "tok")
	users, err := client.GetUsers(ctx, &GetUsersParams{ID: []string{"1"}})
	if err != nil {
		t.Fatalf("GetUsers: %v", err)
	}
	if len(users) != 1 || users[0].ID != "1" {
		t.Fatalf("users = %#v, want single user", users)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("transport calls = %d, want 2 (one retry)", got)
	}
}

func TestClientRetryStopsOnContextCancel(t *testing.T) {
	hit := make(chan struct{}, 1)
	client := NewClient("client-id", "secret", slog.New(slog.NewTextHandler(io.Discard, nil)))
	// A delay long enough that only the cancellation, never the timer, can end
	// the backoff wait — keeps the test deterministic without a real sleep.
	client.retryBaseDelay = time.Hour
	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		select {
		case hit <- struct{}{}:
		default:
		}
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"error":"unavailable"}`)),
		}, nil
	})}

	ctx, cancel := context.WithCancel(context.Background())
	ctx = WithUserToken(ctx, "tok")
	go func() {
		<-hit // first attempt returned a retryable 503; we're now in the backoff wait
		cancel()
	}()

	_, err := client.GetUsers(ctx, &GetUsersParams{ID: []string{"1"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GetUsers err = %v, want context.Canceled", err)
	}
}
