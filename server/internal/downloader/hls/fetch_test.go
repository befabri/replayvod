package hls

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestFetcher(cfg FetcherConfig) *Fetcher {
	return NewFetcher(http.DefaultClient, cfg, slog.New(slog.DiscardHandler))
}

// fetchInto is a test helper that creates a PartWriter for the
// destination, runs Fetch, and commits or aborts based on the
// result. Returns the final file contents (or empty bytes +
// the fetch error).
func fetchInto(t *testing.T, f *Fetcher, url, finalName string) ([]byte, error) {
	t.Helper()
	dir := t.TempDir()
	w, err := NewPartWriter(dir, finalName)
	if err != nil {
		t.Fatalf("writer: %v", err)
	}
	defer w.Abort()
	_, err = f.Fetch(context.Background(), url, w, 0)
	if err != nil {
		return nil, err
	}
	if err := w.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	body, readErr := os.ReadFile(filepath.Join(dir, finalName))
	if readErr != nil {
		t.Fatalf("read final: %v", readErr)
	}
	return body, nil
}

func TestFetch_HappyPath(t *testing.T) {
	payload := strings.Repeat("x", 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		_, _ = io.WriteString(w, payload)
	}))
	defer srv.Close()

	f := newTestFetcher(FetcherConfig{})
	body, err := fetchInto(t, f, srv.URL+"/seg.ts", "42.ts")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(body) != payload {
		t.Errorf("body mismatch: got %d bytes, want %d", len(body), len(payload))
	}
}

func TestFetch_TransportExhausted(t *testing.T) {
	// Closed server address → every dial fails. The transport
	// budget exhausts and we get a permanent FetchError.
	srv := httptest.NewServer(http.NotFoundHandler())
	addr := srv.URL
	srv.Close()

	cfg := FetcherConfig{
		TransportAttempts: 3,
		BaseBackoff:       1 * time.Millisecond,
		MaxBackoff:        5 * time.Millisecond,
	}
	f := newTestFetcher(cfg)

	_, err := fetchInto(t, f, addr+"/seg.ts", "1.ts")
	if err == nil {
		t.Fatal("expected error")
	}
	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("err=%T, want *FetchError", err)
	}
	if fe.Kind != FetchKindTransport {
		t.Errorf("Kind=%s, want transport", fe.Kind)
	}
	if !fe.Permanent {
		t.Error("Permanent=false, want true after budget exhausted")
	}
	if fe.Attempts != cfg.TransportAttempts {
		t.Errorf("Attempts=%d, want %d", fe.Attempts, cfg.TransportAttempts)
	}
}

func TestFetch_AuthReturnsImmediately(t *testing.T) {
	// 401/403 must not consume transport or server budget —
	// orchestrator handles auth refresh.
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	f := newTestFetcher(FetcherConfig{})
	_, err := fetchInto(t, f, srv.URL+"/seg.ts", "1.ts")
	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("err=%T, want *FetchError", err)
	}
	if fe.Kind != FetchKindAuth {
		t.Errorf("Kind=%s, want auth", fe.Kind)
	}
	if fe.Permanent {
		t.Error("Permanent=true on auth; want false (orchestrator refreshes)")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server calls=%d, want 1 (auth is one-shot here)", got)
	}
}

// TestFetch_AuthPermanentFlaggedByClassifier confirms the
// ClassifyAuth hook marks the FetchError with Permanent=true so
// the orchestrator can bail without spinning the refresh loop.
// The hook receives both the status and a bounded body copy for
// entitlement-code parsing.
func TestFetch_AuthPermanentFlaggedByClassifier(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error_code":"subscriptions_restricted","error":"sub-only"}`)
	}))
	defer srv.Close()

	var gotStatus int
	var gotBody []byte
	f := newTestFetcher(FetcherConfig{
		ClassifyAuth: func(status int, body []byte) bool {
			gotStatus = status
			gotBody = append(gotBody, body...)
			return true
		},
	})
	_, err := fetchInto(t, f, srv.URL+"/seg.ts", "1.ts")
	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("err=%T, want *FetchError", err)
	}
	if fe.Kind != FetchKindAuth {
		t.Errorf("Kind=%s, want auth", fe.Kind)
	}
	if !fe.Permanent {
		t.Error("Permanent=false, want true (classifier flagged permanent)")
	}
	if !IsAuthPermanent(err) {
		t.Error("IsAuthPermanent=false for a permanent-flagged auth error")
	}
	if gotStatus != http.StatusForbidden {
		t.Errorf("ClassifyAuth got status=%d, want 403", gotStatus)
	}
	if !strings.Contains(string(gotBody), "subscriptions_restricted") {
		t.Errorf("ClassifyAuth body=%q, want entitlement code", string(gotBody))
	}
}

// TestFetch_ThroughputWatchdogCancelsStalledBody confirms the
// spec's Stage 4 gotcha mitigation: a TCP connection that arrives
// with headers but then trickles zero bytes must not pin the
// worker. The watchdog observes zero forward progress and closes
// the body, which fails the Copy and burns a transport attempt.
func TestFetch_ThroughputWatchdogCancelsStalledBody(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000000")
		w.WriteHeader(http.StatusOK)
		// Flush headers, then stall until the test releases.
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-release
	}))
	defer func() {
		close(release)
		srv.Close()
	}()

	// Tight window + tight transport budget so the test finishes
	// in under a second rather than 30.
	f := newTestFetcher(FetcherConfig{
		TransportAttempts: 1,
		ThroughputWindow:  150 * time.Millisecond,
		BaseBackoff:       time.Millisecond,
		MaxBackoff:        2 * time.Millisecond,
	})

	start := time.Now()
	_, err := fetchInto(t, f, srv.URL+"/seg.ts", "1.ts")
	elapsed := time.Since(start)

	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("err=%T, want *FetchError", err)
	}
	if fe.Kind != FetchKindTransport {
		t.Errorf("Kind=%s, want transport (stalled body = transport failure)", fe.Kind)
	}
	if elapsed > 2*time.Second {
		t.Errorf("elapsed=%v, want fast abort (<2s) — watchdog didn't trigger", elapsed)
	}
}

// TestFetch_AuthRetryableWhenClassifierReturnsFalse confirms the
// default (non-permanent) path: ClassifyAuth returning false
// leaves Permanent=false, signaling the orchestrator to refresh
// and retry.
func TestFetch_AuthRetryableWhenClassifierReturnsFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `stale signature`)
	}))
	defer srv.Close()

	f := newTestFetcher(FetcherConfig{
		ClassifyAuth: func(status int, body []byte) bool { return false },
	})
	_, err := fetchInto(t, f, srv.URL+"/seg.ts", "1.ts")
	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("err=%T, want *FetchError", err)
	}
	if fe.Permanent {
		t.Error("Permanent=true; want false — classifier returned false (retryable)")
	}
	if IsAuthPermanent(err) {
		t.Error("IsAuthPermanent=true for a non-permanent auth error")
	}
}

func TestFetch_CDNLagExhausted(t *testing.T) {
	// Every request returns 404 — CDN-lag budget exhausts.
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := FetcherConfig{
		CDNLagAttempts: 3,
		TargetDuration: 4 * time.Millisecond, // halved → 2ms between retries
	}
	f := newTestFetcher(cfg)
	_, err := fetchInto(t, f, srv.URL+"/seg.ts", "1.ts")
	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("err=%T, want *FetchError", err)
	}
	if fe.Kind != FetchKindCDNLag {
		t.Errorf("Kind=%s, want cdn_lag", fe.Kind)
	}
	if !fe.Permanent {
		t.Error("Permanent=false, want true")
	}
	if got := atomic.LoadInt32(&calls); int(got) != cfg.CDNLagAttempts {
		t.Errorf("server calls=%d, want %d", got, cfg.CDNLagAttempts)
	}
}

func TestFetch_ServerErrorExhausted(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	cfg := FetcherConfig{
		ServerErrorAttempts: 3,
		BaseBackoff:         1 * time.Millisecond,
		MaxBackoff:          5 * time.Millisecond,
	}
	f := newTestFetcher(cfg)
	_, err := fetchInto(t, f, srv.URL+"/seg.ts", "1.ts")
	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("err=%T, want *FetchError", err)
	}
	if fe.Kind != FetchKindServer {
		t.Errorf("Kind=%s, want server", fe.Kind)
	}
	if got := atomic.LoadInt32(&calls); int(got) != cfg.ServerErrorAttempts {
		t.Errorf("server calls=%d, want %d", got, cfg.ServerErrorAttempts)
	}
}

func TestFetch_ServerErrorRecovers(t *testing.T) {
	// Two 503s, then 200.
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, "recovered")
	}))
	defer srv.Close()

	cfg := FetcherConfig{
		ServerErrorAttempts: 5,
		BaseBackoff:         1 * time.Millisecond,
		MaxBackoff:          5 * time.Millisecond,
	}
	f := newTestFetcher(cfg)
	body, err := fetchInto(t, f, srv.URL+"/seg.ts", "1.ts")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(body) != "recovered" {
		t.Errorf("body=%q, want recovered", body)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("calls=%d, want 3 (2 fail + 1 success)", got)
	}
}

func TestFetch_CDNLagRecovers(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, "here-now")
	}))
	defer srv.Close()

	cfg := FetcherConfig{
		CDNLagAttempts: 3,
		TargetDuration: 2 * time.Millisecond,
	}
	f := newTestFetcher(cfg)
	body, err := fetchInto(t, f, srv.URL+"/seg.ts", "1.ts")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(body) != "here-now" {
		t.Errorf("body=%q", body)
	}
}

func TestFetch_ContentLengthMismatchTriggersRetry(t *testing.T) {
	// Server lies about Content-Length: declared 100, sends 5.
	// Fetcher must notice and burn the transport budget.
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "short")
	}))
	defer srv.Close()

	cfg := FetcherConfig{
		TransportAttempts: 2,
		BaseBackoff:       1 * time.Millisecond,
		MaxBackoff:        2 * time.Millisecond,
	}
	f := newTestFetcher(cfg)
	_, err := fetchInto(t, f, srv.URL+"/seg.ts", "1.ts")
	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("err=%T, want *FetchError", err)
	}
	if fe.Kind != FetchKindTransport {
		t.Errorf("Kind=%s, want transport (short read)", fe.Kind)
	}
	if got := atomic.LoadInt32(&calls); int(got) != cfg.TransportAttempts {
		t.Errorf("calls=%d, want %d", got, cfg.TransportAttempts)
	}
}

func TestFetch_RetryAfterHonoredForServerErrors(t *testing.T) {
	// Server sends 503 + Retry-After: 1 on first call, 200 on
	// second. We can't easily assert the sleep duration without
	// clock surgery, but we can assert the response path fires.
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	cfg := FetcherConfig{
		ServerErrorAttempts: 3,
		BaseBackoff:         1 * time.Millisecond,
	}
	f := newTestFetcher(cfg)

	// Use a ctx with a short deadline — the Retry-After of 1s
	// would ordinarily sleep longer than this test wants. We
	// verify Retry-After was observed by parsing RetryAfter()
	// directly in a separate test; this one just ensures the
	// 503 → retry path doesn't explode.
	start := time.Now()
	body, err := fetchInto(t, f, srv.URL+"/seg.ts", "1.ts")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(body) != "ok" {
		t.Errorf("body=%q", body)
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("elapsed=%v, expected ≥ ~1s for Retry-After honored", elapsed)
	}
}

func TestFetch_UnexpectedStatusIsMalformed(t *testing.T) {
	// 418 is none of the handled classes. Treat as permanent
	// malformed so the orchestrator surfaces it rather than
	// looping forever.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()

	f := newTestFetcher(FetcherConfig{})
	_, err := fetchInto(t, f, srv.URL+"/seg.ts", "1.ts")
	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("err=%T", err)
	}
	if fe.Kind != FetchKindMalformed {
		t.Errorf("Kind=%s, want malformed", fe.Kind)
	}
	if !fe.Permanent {
		t.Error("Permanent=false, want true")
	}
}

func TestFetch_CtxCancelShortCircuits(t *testing.T) {
	// Server that hangs until request is canceled.
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-done
	}))
	defer srv.Close()
	defer close(done)

	f := newTestFetcher(FetcherConfig{})
	dir := t.TempDir()
	w, err := NewPartWriter(dir, "1.ts")
	if err != nil {
		t.Fatalf("writer: %v", err)
	}
	defer w.Abort()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = f.Fetch(ctx, srv.URL+"/seg.ts", w, 0)
	if err == nil {
		t.Fatal("expected cancel error")
	}
	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("err=%T, want *FetchError", err)
	}
	if fe.Kind != FetchKindCanceled {
		t.Errorf("Kind=%s, want canceled", fe.Kind)
	}
	if !errors.Is(fe.Cause, context.Canceled) && !errors.Is(fe.Cause, context.DeadlineExceeded) {
		t.Errorf("Cause=%v, want context.{Canceled,DeadlineExceeded}", fe.Cause)
	}
}

// TestFetch_PartialBodyThenSuccess is the regression guard for
// the bug where the fetcher retried a mid-body copy failure on
// the same PartWriter, producing a final file equal to
// partial+full rather than the second response's body. Serves
// a short response with a broken Content-Length on the first
// hit, then the real body on the second.
func TestFetch_PartialBodyThenSuccess(t *testing.T) {
	const good = "this-is-the-real-body"
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			// Declared length longer than what we actually
			// send → Fetcher hits the short-read path, burns
			// transport budget, retries.
			w.Header().Set("Content-Length", "999")
			_, _ = io.WriteString(w, "junk-")
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(good)))
		_, _ = io.WriteString(w, good)
	}))
	defer srv.Close()

	cfg := FetcherConfig{
		TransportAttempts: 3,
		BaseBackoff:       1 * time.Millisecond,
		MaxBackoff:        2 * time.Millisecond,
	}
	f := newTestFetcher(cfg)
	body, err := fetchInto(t, f, srv.URL+"/seg.ts", "seg.ts")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(body) != good {
		t.Errorf("body=%q, want %q (partial must NOT persist into final file)", body, good)
	}
}

// TestFetch_BodyReadErrorThenSuccess exercises the other
// retry-after-partial-write path: the 2xx body stream breaks
// mid-copy (connection closed after partial bytes), the Fetcher
// resets the writer, next attempt delivers the full body.
func TestFetch_BodyReadErrorThenSuccess(t *testing.T) {
	const good = "complete-body-content"
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			// Declare one length, send the first chunk, then
			// hijack the connection to simulate a mid-body
			// failure (EOF before the promised body length).
			w.Header().Set("Content-Length", "500")
			_, _ = io.WriteString(w, "AAAA-BBBB-CCCC")
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("Hijacker unavailable")
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(good)))
		_, _ = io.WriteString(w, good)
	}))
	defer srv.Close()

	cfg := FetcherConfig{
		TransportAttempts: 3,
		BaseBackoff:       1 * time.Millisecond,
		MaxBackoff:        2 * time.Millisecond,
	}
	f := newTestFetcher(cfg)
	body, err := fetchInto(t, f, srv.URL+"/seg.ts", "seg.ts")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(body) != good {
		t.Errorf("body=%q, want %q (partial from attempt 1 must NOT persist)", body, good)
	}
}

// TestFetch_CancelDuringSleep confirms a context cancellation
// mid-backoff is reported as FetchKindCanceled, not as whatever
// class triggered the retry. This matters for graceful-shutdown
// logs in Phase 4c: "job canceled" should not look like a
// transport flake.
func TestFetch_CancelDuringSleep(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always 503 so the fetcher goes to sleep honoring a
		// Retry-After that's longer than the test ctx timeout.
		w.Header().Set("Retry-After", "10")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cfg := FetcherConfig{
		ServerErrorAttempts: 5,
	}
	f := newTestFetcher(cfg)
	dir := t.TempDir()
	w, err := NewPartWriter(dir, "seg.ts")
	if err != nil {
		t.Fatalf("writer: %v", err)
	}
	defer w.Abort()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err = f.Fetch(ctx, srv.URL+"/seg.ts", w, 0)
	if err == nil {
		t.Fatal("expected cancel error")
	}
	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("err=%T, want *FetchError", err)
	}
	if fe.Kind != FetchKindCanceled {
		t.Errorf("Kind=%s, want canceled (sleep was canceled mid-Retry-After)", fe.Kind)
	}
}

func TestIsAuth(t *testing.T) {
	authErr := &FetchError{Kind: FetchKindAuth}
	if !IsAuth(authErr) {
		t.Error("IsAuth(FetchKindAuth)=false, want true")
	}
	if IsAuth(errors.New("plain")) {
		t.Error("IsAuth(plain)=true, want false")
	}
	if IsAuth(&FetchError{Kind: FetchKindTransport}) {
		t.Error("IsAuth(transport)=true, want false")
	}
	if IsAuth(nil) {
		t.Error("IsAuth(nil)=true, want false")
	}
}
