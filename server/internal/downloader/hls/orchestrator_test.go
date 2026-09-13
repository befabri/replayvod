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

	"github.com/befabri/replayvod/server/internal/background"
)

// liveServer advances a sliding playlist on each poll until ENDLIST, with
// deterministic segment payloads for file assertions.
type liveServer struct {
	t            *testing.T
	kind         SegmentKind // ts or fmp4
	maxSegments  int
	windowSize   int
	baseSeq      int
	tickInterval int // target-duration in seconds

	// goneAfter makes the playlist return 404 at this inclusive media sequence; zero disables it.
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
		// Expose one new segment per poll without a background ticker.
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
	cfg.Log = slog.New(slog.DiscardHandler)

	// The sliding fixture requires several real poll intervals to reach ENDLIST.
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
	// A run that terminated on EXT-X-ENDLIST captured the whole broadcast;
	// EndList must report it so the downloader marks the video not-truncated.
	if !result.EndList {
		t.Error("result.EndList=false after natural ENDLIST completion, want true")
	}
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
	// Sliding windows repeat playlist entries; each sequence must be fetched once.
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
	segCalls.Range(func(k, v any) bool {
		n := atomic.LoadInt32(v.(*int32))
		if n != 1 {
			t.Errorf("seg %v fetched %d times, want 1", k, n)
		}
		return true
	})
}

func TestRun_CtxCancelReturnsPartialResult(t *testing.T) {
	// Cancellation returns partial counters without a fatal error; a slow run may
	// cancel before any segment finishes, so no specific segment count is required.
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
	// A mid-flight cancel is NOT a natural broadcast end; EndList must stay
	// false so the downloader marks the recording truncated.
	if result.EndList {
		t.Error("result.EndList=true after a mid-flight cancel, want false")
	}
}

func TestRun_EndListFalseWhenPoolCanceledBeforeFinalCommit(t *testing.T) {
	segStarted := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlist.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:1.000,\n/seg/0.ts\n#EXT-X-ENDLIST\n")
		case "/seg/0.ts":
			once.Do(func() { close(segStarted) })
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := newJob(t, srv, dir)
	cfg.SegmentConcurrency = 1

	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		result *JobResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := Run(ctx, cfg)
		done <- outcome{result: result, err: err}
	}()

	select {
	case <-segStarted:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("segment fetch did not start")
	}
	cancel()

	var out outcome
	select {
	case out = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if out.err != nil {
		t.Fatalf("Run err=%v, want nil ctx-cancel partial result", out.err)
	}
	if out.result == nil {
		t.Fatal("result nil")
	}
	if out.result.EndList {
		t.Fatal("EndList=true even though pool was canceled before final segment committed")
	}
}

// TestRun_PlaylistGoneBubbles checks the sentinel that makes the downloader
// split when Twitch removes a rendition.
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
	// Callers must recognize the authorization sentinel through errors.Is.
	if !errors.Is(err, ErrPlaylistAuth) {
		t.Errorf("err=%v, want errors.Is(ErrPlaylistAuth)", err)
	}
}

// TestRun_InitFetchFailureStopsGoroutines checks that a failed initialization
// cannot leave background fetches or playlist polling after Run returns.
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

	// Wait beyond one poll interval so a leaked poller has time to issue a request.
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

func TestRunProgressCompletesBeforeReturn(t *testing.T) {
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
	var last Progress
	cfg.OnProgress = func(p Progress) { last = p }

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if last.SegmentsDone != 3 {
		t.Errorf("last Progress SegmentsDone=%d, want 3", last.SegmentsDone)
	}
	// A closed (VOD) playlist reports a real total instead of leaving it unknown.
	if last.SegmentsTotal != 3 {
		t.Errorf("last Progress SegmentsTotal=%d, want 3", last.SegmentsTotal)
	}
}

func TestRunWritePanicIsJoinedAndReported(t *testing.T) {
	s := &liveServer{t: t, kind: SegmentKindTS, maxSegments: 3, windowSize: 3, baseSeq: 1, tickInterval: 1}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()
	cfg := newJob(t, srv, t.TempDir())
	var writers atomic.Int64
	cfg.Files = writeFileFunc(func(context.Context, *os.File, []byte) (int, error) {
		writers.Add(1)
		defer writers.Add(-1)
		panic("write boundary failure")
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err := Run(ctx, cfg)
	if err == nil || !strings.Contains(err.Error(), "worker panic: write boundary failure") {
		t.Fatalf("worker panic escaped: %v", err)
	}
	if writers.Load() != 0 {
		t.Fatal("failed pipeline returned before writer exit")
	}
}

func TestRunObserverPanicJoinsWriters(t *testing.T) {
	s := &liveServer{t: t, kind: SegmentKindTS, maxSegments: 3, windowSize: 3, baseSeq: 1, tickInterval: 1}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()
	cfg := newJob(t, srv, t.TempDir())
	var writers atomic.Int64
	cfg.Files = writeFileFunc(func(_ context.Context, f *os.File, p []byte) (int, error) {
		writers.Add(1)
		defer writers.Add(-1)
		return f.Write(p)
	})
	cfg.OnEvent = func(SegmentEvent) { panic("observer failure") }
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err := background.Call(ctx, func(c context.Context) error { _, err := Run(c, cfg); return err })
	if err == nil || !strings.Contains(err.Error(), "worker panic: observer failure") {
		t.Fatalf("observer panic escaped: %v", err)
	}
	if writers.Load() != 0 {
		t.Fatal("observer failure returned before writer exit")
	}
}
