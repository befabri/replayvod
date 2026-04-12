package hls

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestBackoff_WithinWindow(t *testing.T) {
	base := 100 * time.Millisecond
	cap := 5 * time.Second

	// attempt=0 → window is [0, 100ms); attempt=3 → [0, 800ms);
	// attempt large enough to hit cap → [0, cap).
	for _, tc := range []struct {
		attempt int
		window  time.Duration
	}{
		{0, base},              // 100ms
		{3, base << 3},         // 800ms
		{10, cap},              // capped
		{100, cap},             // capped (clamped)
		{-5, base},             // negative clamped to 0 → base window
	} {
		for range 20 {
			got := Backoff(tc.attempt, base, cap)
			if got < 0 || got >= tc.window {
				t.Errorf("Backoff(attempt=%d) = %v, want in [0, %v)", tc.attempt, got, tc.window)
			}
		}
	}
}

func TestBackoff_ZeroBaseReturnsZero(t *testing.T) {
	if got := Backoff(5, 0, time.Second); got != 0 {
		t.Errorf("Backoff with zero base = %v, want 0", got)
	}
}

func TestRetryAfter_IntegerSeconds(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "15")
	d, ok := RetryAfter(resp)
	if !ok {
		t.Fatal("ok=false, want true")
	}
	if d != 15*time.Second {
		t.Errorf("d=%v, want 15s", d)
	}
}

func TestRetryAfter_HTTPDateInFuture(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	future := time.Now().Add(10 * time.Second)
	resp.Header.Set("Retry-After", future.UTC().Format(http.TimeFormat))
	d, ok := RetryAfter(resp)
	if !ok {
		t.Fatal("ok=false, want true")
	}
	// Parsing + clock skew means it won't be exactly 10s; allow
	// a generous ±2s.
	if d < 8*time.Second || d > 12*time.Second {
		t.Errorf("d=%v, want ~10s", d)
	}
}

func TestRetryAfter_PastDate(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat))
	if _, ok := RetryAfter(resp); ok {
		t.Error("past date should return ok=false")
	}
}

func TestRetryAfter_Missing(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	if _, ok := RetryAfter(resp); ok {
		t.Error("missing header should return ok=false")
	}
}

func TestRetryAfter_Malformed(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "not-a-date-or-number")
	if _, ok := RetryAfter(resp); ok {
		t.Error("malformed header should return ok=false")
	}
}

func TestRetryAfter_ZeroSeconds(t *testing.T) {
	// Spec: Retry-After: 0 is valid HTTP but means "retry
	// immediately" — we surface it as (0, false) so the caller
	// falls back to Backoff() which returns something >= 0
	// rather than spin-looping on a zero sleep.
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", strconv.Itoa(0))
	if _, ok := RetryAfter(resp); ok {
		t.Error("Retry-After: 0 should return ok=false")
	}
}
