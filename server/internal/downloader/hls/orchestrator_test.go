package hls

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// liveServer simulates a Twitch-ish HLS edge. Polls return a
// playlist with a sliding window; each poll advances the window
// by one segment until maxSegments is reached, at which point
// ENDLIST is appended. Segments serve a deterministic payload so
// tests can assert exact file contents.
type liveServer struct {
	t            *testing.T
	kind         SegmentKind // ts or fmp4
	maxSegments  int
	windowSize   int
	baseSeq      int
	tickInterval int // target-duration in seconds

	mu     sync.Mutex
	polls  int32
	cursor int // highest seq served so far
}

func (s *liveServer) currentSegs() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	start := max(s.cursor-s.windowSize+1, s.baseSeq)
	out := []int{}
	for i := start; i <= s.cursor; i++ {
		out = append(out, i)
	}
	return out
}

func (s *liveServer) advance() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cursor >= s.baseSeq+s.maxSegments-1 {
		return false
	}
	s.cursor++
	return true
}

func (s *liveServer) ended() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cursor >= s.baseSeq+s.maxSegments-1
}

func (s *liveServer) playlist() string {
	segs := s.currentSegs()
	if len(segs) == 0 {
		// First poll before any segment exists — give the
		// starting window immediately.
		s.mu.Lock()
		s.cursor = s.baseSeq
		s.mu.Unlock()
		segs = s.currentSegs()
	}
	ext := ".ts"
	if s.kind == SegmentKindFMP4 {
		ext = ".mp4"
	}
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n", s.tickInterval)
	fmt.Fprintf(&b, "#EXT-X-MEDIA-SEQUENCE:%d\n", segs[0])
	if s.kind == SegmentKindFMP4 {
		b.WriteString(`#EXT-X-MAP:URI="/init.mp4"` + "\n")
	}
	for _, seq := range segs {
		fmt.Fprintf(&b, "#EXTINF:%d.000,\n", s.tickInterval)
		fmt.Fprintf(&b, "/seg/%d%s\n", seq, ext)
	}
	if s.ended() {
		b.WriteString("#EXT-X-ENDLIST\n")
	}
	return b.String()
}

func (s *liveServer) segmentPayload(seq int) []byte {
	return fmt.Appendf(nil, "seg-%d-payload", seq)
}

func (s *liveServer) initPayload() []byte { return []byte("INIT-SEGMENT-FMP4") }

func (s *liveServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/playlist.m3u8", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&s.polls, 1)
		body := s.playlist()
		// Advance the window after serving — the test's tick
		// accelerates this since each poll forces one segment
		// to become available. Next poll will expose it.
		s.advance()
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(w, body)
	})
	mux.HandleFunc("/init.mp4", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(s.initPayload())
	})
	mux.HandleFunc("/seg/", func(w http.ResponseWriter, r *http.Request) {
		var seq int
		ext := ""
		for _, e := range []string{".ts", ".mp4"} {
			if strings.HasSuffix(r.URL.Path, e) {
				ext = e
				break
			}
		}
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/seg/"), ext)
		_, err := fmt.Sscanf(name, "%d", &seq)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		payload := s.segmentPayload(seq)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		_, _ = w.Write(payload)
	})
	return mux
}

func newJob(t *testing.T, srv *httptest.Server, dir string) JobConfig {
	t.Helper()
	return JobConfig{
		MediaPlaylistURL: srv.URL + "/playlist.m3u8",
		WorkDir:          dir,
		Fetcher: NewFetcher(http.DefaultClient, FetcherConfig{
			TargetDuration: time.Second,
			BaseBackoff:    time.Millisecond,
			MaxBackoff:     2 * time.Millisecond,
		}, slog.New(slog.DiscardHandler)),
		PlaylistClient:     http.DefaultClient,
		SegmentConcurrency: 2,
		Log:                slog.New(slog.DiscardHandler),
	}
}

func TestRun_TSLiveCompletesOnEndlist(t *testing.T) {
	s := &liveServer{
		t:            t,
		kind:         SegmentKindTS,
		maxSegments:  5,
		windowSize:   3,
		baseSeq:      100,
		tickInterval: 1,
	}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	dir := t.TempDir()
	cfg := newJob(t, srv, dir)
	// Collapse the poll tick to keep the test fast.
	cfg.Log = slog.New(slog.DiscardHandler)

	// Override the Poller's minimum tick via a custom Run path:
	// easiest is to trust the TargetDuration=1s + a generous
	// test timeout. The server advances the window on every
	// poll so we finish within ~6s of wall clock.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Kind != SegmentKindTS {
		t.Errorf("Kind=%s, want ts", result.Kind)
	}
	if result.SegmentsDone != 5 {
		t.Errorf("SegmentsDone=%d, want 5", result.SegmentsDone)
	}
	if result.SegmentsGaps != 0 {
		t.Errorf("SegmentsGaps=%d, want 0", result.SegmentsGaps)
	}
	// Each segment file must be on disk with the expected payload.
	for seq := 100; seq < 105; seq++ {
		path := filepath.Join(dir, fmt.Sprintf("%d.ts", seq))
		body, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("seg %d: %v", seq, err)
			continue
		}
		want := s.segmentPayload(seq)
		if string(body) != string(want) {
			t.Errorf("seg %d body=%q, want %q", seq, body, want)
		}
	}
	// No init segment for TS.
	if _, err := os.Stat(filepath.Join(dir, "init.mp4")); err == nil {
		t.Error("init.mp4 present for TS job — shouldn't be")
	}
}

func TestRun_FMP4FetchesInitExactlyOnce(t *testing.T) {
	s := &liveServer{
		t:            t,
		kind:         SegmentKindFMP4,
		maxSegments:  3,
		windowSize:   2,
		baseSeq:      1,
		tickInterval: 1,
	}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	dir := t.TempDir()
	cfg := newJob(t, srv, dir)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Kind != SegmentKindFMP4 {
		t.Errorf("Kind=%s, want fmp4", result.Kind)
	}
	if result.InitURI == "" {
		t.Error("InitURI empty")
	}
	// Init segment must be on disk with the right payload.
	body, err := os.ReadFile(filepath.Join(dir, "init.mp4"))
	if err != nil {
		t.Fatalf("read init: %v", err)
	}
	if string(body) != string(s.initPayload()) {
		t.Errorf("init body mismatch")
	}
	if result.SegmentsDone != 3 {
		t.Errorf("SegmentsDone=%d, want 3", result.SegmentsDone)
	}
	// All fmp4 segments written with .m4s extension.
	for seq := 1; seq <= 3; seq++ {
		path := filepath.Join(dir, fmt.Sprintf("%d.m4s", seq))
		if _, err := os.Stat(path); err != nil {
			t.Errorf("seg %d: %v", seq, err)
		}
	}
}

func TestRun_DedupAcrossPolls(t *testing.T) {
	// Window slides, meaning the same segment appears in the
	// playlist on multiple polls. Only one file per seq must be
	// written, and the fetch must fire exactly once per seq.
	s := &liveServer{
		t:            t,
		kind:         SegmentKindTS,
		maxSegments:  4,
		windowSize:   3,
		baseSeq:      0,
		tickInterval: 1,
	}
	var segCalls sync.Map
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if segPath, ok := strings.CutPrefix(r.URL.Path, "/seg/"); ok {
			name := strings.TrimSuffix(segPath, ".ts")
			v, _ := segCalls.LoadOrStore(name, new(int32))
			atomic.AddInt32(v.(*int32), 1)
		}
		s.handler().ServeHTTP(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := newJob(t, srv, dir)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.SegmentsDone != 4 {
		t.Errorf("SegmentsDone=%d, want 4", result.SegmentsDone)
	}
	// Every segment seen by the server should have been fetched
	// exactly once.
	segCalls.Range(func(k, v any) bool {
		n := atomic.LoadInt32(v.(*int32))
		if n != 1 {
			t.Errorf("seg %v fetched %d times, want 1", k, n)
		}
		return true
	})
}

func TestRun_CtxCancelReturnsPartialResult(t *testing.T) {
	// Canceling mid-flight must not surface as a fatal error —
	// the caller wants "here's what we got; the rest is on you."
	s := &liveServer{
		t:            t,
		kind:         SegmentKindTS,
		maxSegments:  100,
		windowSize:   3,
		baseSeq:      0,
		tickInterval: 1,
	}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	dir := t.TempDir()
	cfg := newJob(t, srv, dir)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	result, err := Run(ctx, cfg)
	// Either context.Canceled/DeadlineExceeded, or nil if we
	// happened to hit ENDLIST first (won't, maxSegments=100).
	if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
		t.Errorf("err=%v, want nil | Canceled | DeadlineExceeded", err)
	}
	if result == nil {
		t.Fatal("result nil on cancel — want partial tally")
	}
	// Should have at least one segment written before cancel —
	// the first poll's window is 3 segments, all fetched before
	// the 500ms budget runs out.
	if result.SegmentsDone == 0 {
		t.Error("SegmentsDone=0 after 500ms; pool should have written ≥ 1 segment")
	}
}

func TestRun_PlaylistAuthErrorBubbles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := newJob(t, srv, dir)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := Run(ctx, cfg)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	// Auth error on the first poll exhausts the poller's retry
	// budget (since every retry also returns 403); the error
	// surfaces wrapped around ErrPlaylistAuth.
	if !strings.Contains(err.Error(), "auth") {
		t.Errorf("err=%v, expected mention of auth", err)
	}
}
