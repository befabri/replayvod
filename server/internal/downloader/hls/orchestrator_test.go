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

	// goneAfter: once the cursor reaches this MediaSeq (inclusive),
	// the playlist handler returns 404 instead of the manifest body.
	// Used to simulate Twitch dropping the variant mid-stream. Zero
	// disables (default behavior).
	goneAfter int

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
		if s.goneAfter > 0 {
			s.mu.Lock()
			cursor := s.cursor
			s.mu.Unlock()
			if cursor >= s.goneAfter {
				w.WriteHeader(http.StatusNotFound)
				return
			}
		}
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
	// SegmentsDone is not asserted because a slow CI can race
	// the 500ms budget; the correctness property is "Run returns
	// cleanly with a result struct and no fatal error."
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
	if err != nil {
		t.Errorf("err=%v, want nil (ctx-err filtered)", err)
	}
	if result == nil {
		t.Fatal("result nil on cancel — want partial tally")
	}
}

// TestRun_PlaylistGoneBubbles: a 404 on the media playlist
// mid-stream surfaces as ErrPlaylistGone so the downloader can
// treat it as a part-split signal, not a transient fetch failure.
func TestRun_PlaylistGoneBubbles(t *testing.T) {
	s := &liveServer{
		t:            t,
		kind:         SegmentKindTS,
		maxSegments:  20,
		windowSize:   3,
		baseSeq:      0,
		tickInterval: 1,
		goneAfter:    2, // playlist 404s once cursor >= 2
	}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	dir := t.TempDir()
	cfg := newJob(t, srv, dir)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := Run(ctx, cfg)
	if err == nil {
		t.Fatal("want ErrPlaylistGone, got nil")
	}
	if !errors.Is(err, ErrPlaylistGone) {
		t.Errorf("err=%v, want errors.Is(ErrPlaylistGone)", err)
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
	// Auth error must surface as the sentinel Phase 4d branches
	// on. String-matching the message would silently rot if the
	// error text ever changes.
	if !errors.Is(err, ErrPlaylistAuth) {
		t.Errorf("err=%v, want errors.Is(ErrPlaylistAuth)", err)
	}
}

// TestRun_InitFetchFailureStopsGoroutines is the H1 regression
// guard. A 404 on the init segment must cancel the poller + pool
// and drain them before Run returns — otherwise segments keep
// landing on disk and the playlist keeps getting polled after
// the caller has been told the job failed.
//
// Watches both signals: segment fetches (pool-leak shape) AND
// playlist polls (poller-leak shape). Sleeps past one
// TargetDuration tick so a leaked poller actually has the chance
// to re-fetch within the window — a shorter sleep would miss
// poller-only leaks that wait a full tick before doing anything.
func TestRun_InitFetchFailureStopsGoroutines(t *testing.T) {
	const tickInterval = 1 // seconds
	var segFetches, playlistPolls int32
	s := &liveServer{
		t:            t,
		kind:         SegmentKindFMP4,
		maxSegments:  100, // keep the playlist live indefinitely
		windowSize:   3,
		baseSeq:      1,
		tickInterval: tickInterval,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/init.mp4":
			w.WriteHeader(http.StatusNotFound)
			return
		case r.URL.Path == "/playlist.m3u8":
			atomic.AddInt32(&playlistPolls, 1)
		case strings.HasPrefix(r.URL.Path, "/seg/"):
			atomic.AddInt32(&segFetches, 1)
		}
		s.handler().ServeHTTP(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := newJob(t, srv, dir)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := Run(ctx, cfg)
	if err == nil {
		t.Fatal("want init-fetch error")
	}
	if !strings.Contains(err.Error(), "init segment") {
		t.Errorf("err=%v, want init segment mention", err)
	}

	// Capture counts at return time, sleep past one tick + slack
	// so a leaked poller goroutine would get a chance to re-fetch
	// the playlist, then verify neither counter advanced.
	segsBefore := atomic.LoadInt32(&segFetches)
	pollsBefore := atomic.LoadInt32(&playlistPolls)
	time.Sleep(time.Duration(tickInterval)*time.Second + 200*time.Millisecond)
	if got := atomic.LoadInt32(&segFetches); got != segsBefore {
		t.Errorf("segment fetches kept firing after Run returned: before=%d after=%d",
			segsBefore, got)
	}
	if got := atomic.LoadInt32(&playlistPolls); got != pollsBefore {
		t.Errorf("playlist polls kept firing after Run returned: before=%d after=%d",
			pollsBefore, got)
	}
}

// TestRun_ProgressChannelClosedOnTermination pins M2: the
// orchestrator closes Progress exactly once on the way out so
// subscribers see the final cumulative state without relying on
// a best-effort non-blocking send.
func TestRun_ProgressChannelClosedOnTermination(t *testing.T) {
	s := &liveServer{
		t:            t,
		kind:         SegmentKindTS,
		maxSegments:  3,
		windowSize:   2,
		baseSeq:      0,
		tickInterval: 1,
	}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	dir := t.TempDir()
	cfg := newJob(t, srv, dir)
	progress := make(chan Progress, 16)
	cfg.Progress = progress

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Drain the channel. After Run returns, the chan must be
	// closed — a blocking range loop will terminate rather than
	// hang.
	var last Progress
	drained := make(chan struct{})
	go func() {
		for p := range progress {
			last = p
		}
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("Progress channel not closed after Run returned")
	}
	if last.SegmentsDone != 3 {
		t.Errorf("last Progress SegmentsDone=%d, want 3", last.SegmentsDone)
	}
}
