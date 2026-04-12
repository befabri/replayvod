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
// attempt=0. base is the initial window (e.g. 500ms); maxWindow
// caps the exponential growth (e.g. 30s).
//
// The window is computed overflow-safely: we start from the cap
// and only lower it if base<<attempt stays representable in an
// int64 and beats the cap. A naïve `base<<attempt` overflows into
// negative values at modest (base, attempt) combinations — e.g.
// base=10s, attempt=30 — and the subsequent comparison with the
// cap produces nonsense (negative < positive, so the overflowed
// value "wins"), silently collapsing backoff to zero.
//
// Uses math/rand/v2 — no global seed needed, and the package is
// thread-safe for package-level functions like Int64N.
func Backoff(attempt int, base, maxWindow time.Duration) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	capNS := int64(maxWindow)
	baseNS := int64(base)
	// Zero base means "no backoff requested" — return immediately
	// so callers with a disabled backoff don't silently wait the
	// full cap window.
	if baseNS <= 0 {
		return 0
	}
	if capNS <= 0 {
		return 0
	}
	// windowNS starts at the cap. We only lower it when the
	// exponential term is (a) representable and (b) smaller.
	windowNS := capNS
	if attempt < 63 {
		// Overflow-safe bound: if baseNS * 2^attempt would
		// exceed int64, we already know it exceeds the cap (cap
		// fits in int64 by construction), so leave windowNS at
		// capNS.
		if baseNS <= (1<<62)>>attempt {
			candidate := baseNS << attempt
			if candidate > 0 && candidate < windowNS {
				windowNS = candidate
			}
		}
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
