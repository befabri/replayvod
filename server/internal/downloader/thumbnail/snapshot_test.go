package thumbnail

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// memWriter is the test-side SnapshotWriter — captures snapshots
// in memory so assertions can inspect index + bytes without going
// through storage.
type memWriter struct {
	mu       sync.Mutex
	captures map[int][]byte
	wantErr  error // when non-nil, WriteSnapshot returns it
}

func newMemWriter() *memWriter {
	return &memWriter{captures: map[int][]byte{}}
}

func (m *memWriter) WriteSnapshot(_ context.Context, index int, body io.Reader) error {
	if m.wantErr != nil {
		return m.wantErr
	}
	buf, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.captures[index] = buf
	return nil
}

func (m *memWriter) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.captures)
}

func (m *memWriter) at(index int) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.captures[index]
}

// fakePreviewServer returns a httptest.Server that serves a
// parameterized JPEG body per request. Tracks hit count + last
// URL so tests can assert both the cadence and the cache-buster
// shape. Status code can be overridden to simulate CDN failures.
type fakePreviewServer struct {
	*httptest.Server
	hits    atomic.Int32
	lastURL atomic.Pointer[string]
	status  atomic.Int32
}

func newFakePreviewServer() *fakePreviewServer {
	fp := &fakePreviewServer{}
	fp.status.Store(http.StatusOK)
	fp.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(fp.hits.Add(1))
		u := r.URL.String()
		fp.lastURL.Store(&u)
		if s := int(fp.status.Load()); s != http.StatusOK {
			w.WriteHeader(s)
			return
		}
		// Each body is the hit number as ASCII — easy to
		// assert the Nth capture holds the Nth bytes.
		w.Header().Set("Content-Type", "image/jpeg")
		fmt.Fprintf(w, "snap-%d", n)
	}))
	return fp
}

// snapshotterWithHTTPBase builds a Snapshotter whose HTTP client
// rewrites any request to the test server. The prod code builds
// the URL against static-cdn.jtvnw.net; we intercept at the
// Transport layer so the test doesn't need to know the URL
// template.
func snapshotterWithHTTPBase(t *testing.T, fp *fakePreviewServer, interval time.Duration) *Snapshotter {
	t.Helper()
	tr := &rewriteTransport{base: fp.URL}
	return NewSnapshotter(SnapshotterConfig{
		HTTPClient: &http.Client{Transport: tr, Timeout: 5 * time.Second},
		Interval:   interval,
	})
}

type rewriteTransport struct{ base string }

func (r *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newURL := r.base + req.URL.Path
	if req.URL.RawQuery != "" {
		newURL += "?" + req.URL.RawQuery
	}
	r2 := req.Clone(req.Context())
	newParsed, err := req.URL.Parse(newURL)
	if err != nil {
		return nil, err
	}
	r2.URL = newParsed
	r2.Host = newParsed.Host
	return http.DefaultTransport.RoundTrip(r2)
}

// TestSnapshotter_FiresImmediately verifies the first capture
// happens without waiting a full interval — a 3-minute recording
// with a 5-minute interval should still produce a snapshot.
func TestSnapshotter_FiresImmediately(t *testing.T) {
	fp := newFakePreviewServer()
	defer fp.Close()
	// Long interval — we never want the ticker to fire in this
	// test; only the immediate fetch should land.
	s := snapshotterWithHTTPBase(t, fp, 30*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	w := newMemWriter()
	done := make(chan int, 1)
	go func() { done <- s.Run(ctx, "tumblurr", w) }()

	// Wait for the first hit.
	deadline := time.Now().Add(2 * time.Second)
	for fp.hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	count := <-done

	if count != 1 {
		t.Errorf("count=%d, want 1", count)
	}
	if got := w.count(); got != 1 {
		t.Errorf("writer captures=%d, want 1", got)
	}
	if got := string(w.at(0)); got != "snap-1" {
		t.Errorf("capture[0]=%q, want snap-1", got)
	}
}

// TestSnapshotter_TicksAtInterval verifies multiple snapshots
// land at the configured cadence. Short interval so the test
// stays fast.
func TestSnapshotter_TicksAtInterval(t *testing.T) {
	fp := newFakePreviewServer()
	defer fp.Close()
	s := snapshotterWithHTTPBase(t, fp, 50*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()
	w := newMemWriter()
	got := s.Run(ctx, "tumblurr", w)

	// Expect immediate + ~6 ticks at 50ms over 350ms. Slack
	// generously for scheduler jitter so a heavily-loaded CI
	// doesn't flake.
	if got < 3 {
		t.Errorf("count=%d, want ≥ 3", got)
	}
	if got > 10 {
		t.Errorf("count=%d, unusually high — ticker leaked?", got)
	}
	// Each capture should hold a distinct body (proves we
	// actually fetched each one, not cached).
	seen := map[string]bool{}
	for i := 0; i < got; i++ {
		seen[string(w.at(i))] = true
	}
	if len(seen) != got {
		t.Errorf("distinct bodies=%d, want %d (dedupe or cache?)", len(seen), got)
	}
}

// TestSnapshotter_URLShape verifies the complete URL we send to
// the CDN, not just a substring. The earlier "contains
// live_user_X-" test passed with a typo in any other path
// component — this asserts the exact path Twitch's CDN expects,
// so a future edit to livePreviewURLTemplate that breaks the
// shape fails here instead of 404ing silently in production.
//
// The hermetic test can't verify "the path works on real Twitch"
// — that's what snapshot_live_test.go does. What it CAN verify is
// that whatever URL template we use produces the path we intended.
func TestSnapshotter_URLShape(t *testing.T) {
	fp := newFakePreviewServer()
	defer fp.Close()
	s := snapshotterWithHTTPBase(t, fp, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	w := newMemWriter()
	done := make(chan struct{})
	go func() { s.Run(ctx, "Tumblurr", w); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for fp.hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	raw := fp.lastURL.Load()
	if raw == nil {
		t.Fatal("no URL recorded")
	}
	u, err := url.Parse(*raw)
	if err != nil {
		t.Fatalf("parse captured URL %q: %v", *raw, err)
	}

	// Exact path — matches the Twitch CDN contract exactly,
	// including the default dimensions. A typo anywhere fails.
	if got, want := u.Path, "/previews-ttv/live_user_tumblurr-1280x720.jpg"; got != want {
		t.Errorf("path=%q, want %q", got, want)
	}

	// Login is lowercased — Twitch's CDN is case-sensitive.
	if strings.Contains(u.Path, "Tumblurr") {
		t.Error("path preserved uppercase login — CDN would 404 this")
	}

	// Cache-buster is present and is an integer Unix timestamp.
	// Asserting the shape ensures we don't regress to a constant
	// value or drop the param entirely (both would produce a
	// stale image from intermediate CDN caches in prod).
	tsParam := u.Query().Get("_t")
	if tsParam == "" {
		t.Error("cache-buster query param _t missing")
	}
	if !regexp.MustCompile(`^\d+$`).MatchString(tsParam) {
		t.Errorf("cache-buster _t=%q, want integer unix seconds", tsParam)
	}
}

// TestSnapshotter_URLUsesConfiguredDimensions verifies that
// non-default Width/Height propagate into the URL. Without this,
// a typo in the URL template (using a constant instead of the
// configured value) would silently hand Twitch the wrong size and
// we'd get the default 1280×720 regardless of config.
func TestSnapshotter_URLUsesConfiguredDimensions(t *testing.T) {
	fp := newFakePreviewServer()
	defer fp.Close()
	s := NewSnapshotter(SnapshotterConfig{
		HTTPClient: &http.Client{Transport: &rewriteTransport{base: fp.URL}, Timeout: 5 * time.Second},
		Interval:   time.Hour,
		Width:      640,
		Height:     360,
	})

	ctx, cancel := context.WithCancel(context.Background())
	w := newMemWriter()
	done := make(chan struct{})
	go func() { s.Run(ctx, "tumblurr", w); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for fp.hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	raw := fp.lastURL.Load()
	if raw == nil {
		t.Fatal("no URL recorded")
	}
	u, _ := url.Parse(*raw)
	if got, want := u.Path, "/previews-ttv/live_user_tumblurr-640x360.jpg"; got != want {
		t.Errorf("path=%q, want %q (dimensions must come from config)", got, want)
	}
}

// TestSnapshotter_CacheBuster verifies each fetch carries a
// distinct _t query param — without this the CDN's intermediate
// proxies return the same cached bytes on every refresh.
func TestSnapshotter_CacheBuster(t *testing.T) {
	fp := newFakePreviewServer()
	defer fp.Close()
	s := snapshotterWithHTTPBase(t, fp, 50*time.Millisecond)

	seen := map[string]struct{}{}
	fp.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.URL.RawQuery] = struct{}{}
		fp.hits.Add(1)
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("ok"))
	})

	// Make sure the cache-buster resolution is high enough for
	// the test to observe distinct values. The production code
	// uses time.Now().Unix() which is second-granular; pad the
	// test ticks across 2+ seconds so we definitely cross a
	// second boundary.
	ctx, cancel := context.WithTimeout(context.Background(), 2200*time.Millisecond)
	defer cancel()
	_ = s.Run(ctx, "tumblurr", newMemWriter())

	// Expect at least 2 distinct query strings over the window.
	// Relax on CI where timing is fuzzy.
	if len(seen) < 2 {
		t.Errorf("distinct cache-buster values=%d, want ≥ 2 (hits=%d)", len(seen), fp.hits.Load())
	}
}

// TestSnapshotter_TransientErrorsSkipped verifies a CDN 404
// (which happens when a stream briefly disappears) doesn't kill
// the ticker — subsequent ticks recover.
func TestSnapshotter_TransientErrorsSkipped(t *testing.T) {
	fp := newFakePreviewServer()
	defer fp.Close()
	fp.status.Store(int32(http.StatusNotFound))

	// Flip back to 200 after the first failure.
	go func() {
		time.Sleep(75 * time.Millisecond)
		fp.status.Store(int32(http.StatusOK))
	}()

	s := snapshotterWithHTTPBase(t, fp, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	w := newMemWriter()
	got := s.Run(ctx, "tumblurr", w)

	if got == 0 {
		t.Errorf("count=0 — ticker died on transient 404")
	}
	if int(fp.hits.Load()) < got+1 {
		t.Errorf("hits=%d, want ≥ count+1=%d (no failed attempts?)",
			fp.hits.Load(), got+1)
	}
}

// TestSnapshotter_WriteErrorDoesNotAbort verifies that a storage
// failure on one snapshot (disk full, object store hiccup) is
// logged and skipped — the next tick proceeds normally.
func TestSnapshotter_WriteErrorDoesNotAbort(t *testing.T) {
	fp := newFakePreviewServer()
	defer fp.Close()
	s := snapshotterWithHTTPBase(t, fp, 50*time.Millisecond)

	w := newMemWriter()
	w.wantErr = fmt.Errorf("fake storage down")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	got := s.Run(ctx, "tumblurr", w)

	if got != 0 {
		t.Errorf("count=%d, want 0 (all writes failed)", got)
	}
	// Hits should still have happened — the fetcher didn't
	// short-circuit after the first write error.
	if fp.hits.Load() < 2 {
		t.Errorf("hits=%d, want ≥ 2 (ticker aborted on first error?)", fp.hits.Load())
	}
}

// TestSnapshotter_ContextCancelStopsFast verifies cancellation
// during a slow CDN response doesn't leak the goroutine.
func TestSnapshotter_ContextCancelStopsFast(t *testing.T) {
	// Slow handler: blocks for 5s so the test would hang if
	// cancellation didn't propagate through the HTTP client.
	block := make(chan struct{})
	defer close(block)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	s := NewSnapshotter(SnapshotterConfig{
		HTTPClient: &http.Client{Transport: &rewriteTransport{base: srv.URL}},
		Interval:   time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = s.Run(ctx, "tumblurr", newMemWriter())
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit within 2s after cancel")
	}
}

// io.Reader stub for the write-body error case — unused directly
// but kept in the file for symmetry with the memWriter helpers so
// future snapshot-related tests don't have to re-invent it.
var _ io.Reader = bytes.NewReader(nil)
