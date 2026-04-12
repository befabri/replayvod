package hls

import (
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

// Backoff computes the sleep duration before the (attempt+1)-th try.
// Full-jitter formula: rand(0, min(cap, base*2^attempt)). Spec
// Stage 4 choice: AWS's analysis on retry-backoff distributions
// shows full-jitter minimizes tail latency for multi-client
// contention, which is the Twitch-edge contention shape.
//
// attempt is zero-indexed: the retry *after* the first failure is
// attempt=0. base is the initial window (e.g. 500ms); cap caps
// the exponential growth (e.g. 30s).
//
// Uses math/rand/v2 — no global seed needed, and the package is
// thread-safe for package-level functions like Int64N.
func Backoff(attempt int, base, cap time.Duration) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	// 2^attempt overflows int64 at attempt >= 63; clamp long
	// before that.
	if attempt > 30 {
		attempt = 30
	}
	windowNS := int64(base) << attempt
	capNS := int64(cap)
	if capNS > 0 && windowNS > capNS {
		windowNS = capNS
	}
	if windowNS <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(windowNS))
}

// RetryAfter parses the Retry-After header off a response. Honors
// both integer-seconds ("30") and HTTP-date forms per RFC 9110.
// Returns (0, false) when the header is missing, malformed, or
// in the past. Callers fall back to Backoff() when this returns
// zero.
func RetryAfter(resp *http.Response) (time.Duration, bool) {
	if resp == nil {
		return 0, false
	}
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0, false
	}
	// Integer-seconds form is the common case; try it first.
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d, true
		}
	}
	return 0, false
}
