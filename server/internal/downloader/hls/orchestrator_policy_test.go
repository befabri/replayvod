package hls

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// failingSegmentServer wraps a liveServer so specific segment
// seq numbers return a permanent 500 (Fetcher's server-error
// budget exhausts) while the rest succeed. Simulates a
// spotty CDN.
type failingSegmentServer struct {
	live *liveServer
	fail map[int]bool
}

func (f *failingSegmentServer) handler() http.Handler {
	base := f.live.handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /seg/<seq>.ts or /seg/<seq>.mp4 — strip prefix + extension,
		// parse numeric seq, check fail map.
		if segPath, ok := strings.CutPrefix(r.URL.Path, "/seg/"); ok {
			name := strings.TrimSuffix(strings.TrimSuffix(segPath, ".ts"), ".mp4")
			if seq, err := strconv.Atoi(name); err == nil && f.fail[seq] {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}
		base.ServeHTTP(w, r)
	})
}

// newPolicyJob mirrors newJob but lets tests knob gap policy
// without duplicating the full JobConfig construction.
func newPolicyJob(t *testing.T, srv *httptest.Server, dir string, policy GapPolicy) JobConfig {
	t.Helper()
	cfg := newJob(t, srv, dir)
	cfg.GapPolicy = policy
	// Tight retry budgets so failing segments burn through fast —
	// the default 5 attempts with 500ms base backoff makes these
	// tests glacially slow.
	cfg.Fetcher = NewFetcher(http.DefaultClient, FetcherConfig{
		TransportAttempts:   2,
		ServerErrorAttempts: 2,
		CDNLagAttempts:      2,
		TargetDuration:      100 * time.Millisecond,
		BaseBackoff:         time.Millisecond,
		MaxBackoff:          5 * time.Millisecond,
	}, cfg.Log)
	return cfg
}

func TestRun_GapPolicy_StrictAbortsOnFirstFailure(t *testing.T) {
	live := &liveServer{
		kind: SegmentKindTS, maxSegments: 5, windowSize: 3,
		baseSeq: 0, tickInterval: 1,
	}
	// Seg 2 always 500s; segs 0-1 + 3-4 succeed.
	s := &failingSegmentServer{live: live, fail: map[int]bool{2: true}}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	dir := t.TempDir()
	cfg := newPolicyJob(t, srv, dir, GapPolicy{Strict: true})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := Run(ctx, cfg)
	if err == nil {
		t.Fatal("want GapAbortError in strict mode")
	}
	var ge *GapAbortError
	if !errors.As(err, &ge) {
		t.Fatalf("err=%T, want *GapAbortError", err)
	}
	if ge.Reason != "strict mode" {
		t.Errorf("Reason=%q, want strict mode", ge.Reason)
	}
	// Don't assert SegmentsDone > 0 — with concurrent workers
	// the count depends on whether seqs 0+1 land before seq 2's
	// failure propagates. The correctness property the test
	// guards is "strict aborts on first failure," which the
	// Reason check above already covers.
}

func TestRun_GapPolicy_FirstContentGuardFailsEarly(t *testing.T) {
	live := &liveServer{
		kind: SegmentKindTS, maxSegments: 5, windowSize: 3,
		baseSeq: 0, tickInterval: 1,
	}
	// Seg 0 always fails — no real-content segment ever commits.
	s := &failingSegmentServer{live: live, fail: map[int]bool{0: true}}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	dir := t.TempDir()
	cfg := newPolicyJob(t, srv, dir, GapPolicy{})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := Run(ctx, cfg)
	if err == nil {
		t.Fatal("want first-content guard abort")
	}
	var ge *GapAbortError
	if !errors.As(err, &ge) {
		t.Fatalf("err=%T, want *GapAbortError", err)
	}
	if !strings.Contains(ge.Reason, "no content segment") {
		t.Errorf("Reason=%q, want first-content-guard phrasing", ge.Reason)
	}
}

func TestRun_GapPolicy_TolerantUnderRatio(t *testing.T) {
	// One failure among 10 segments = 10% gap ratio. Pick a
	// ceiling above that (0.2 = 20%) so the gap is accepted.
	// Keeps the test wall-clock under one minute — each poll
	// advances the window by 1 segment at the 1s MinTick.
	live := &liveServer{
		kind: SegmentKindTS, maxSegments: 10, windowSize: 3,
		baseSeq: 0, tickInterval: 1,
	}
	// Seg 5 fails (past the first-content guard) so the ratio
	// branch is the one the test actually exercises.
	s := &failingSegmentServer{live: live, fail: map[int]bool{5: true}}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	dir := t.TempDir()
	cfg := newPolicyJob(t, srv, dir, GapPolicy{MaxGapRatio: 0.2})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("want nil under-ratio: %v", err)
	}
	if result.SegmentsGaps != 1 {
		t.Errorf("SegmentsGaps=%d, want 1", result.SegmentsGaps)
	}
	if result.SegmentsDone != 9 {
		t.Errorf("SegmentsDone=%d, want 9", result.SegmentsDone)
	}
}

func TestRun_GapPolicy_AbortsOverRatio(t *testing.T) {
	// Short stream where one failure trips a 1% ceiling. With
	// 5 total segments, one gap = 20% ≫ 1%. Needs the first
	// content to commit before the guard will even let the
	// ratio check run.
	live := &liveServer{
		kind: SegmentKindTS, maxSegments: 5, windowSize: 3,
		baseSeq: 0, tickInterval: 1,
	}
	// Seg 0 succeeds (clears first-content guard), seg 2 fails.
	s := &failingSegmentServer{live: live, fail: map[int]bool{2: true}}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	dir := t.TempDir()
	cfg := newPolicyJob(t, srv, dir, GapPolicy{MaxGapRatio: 0.01})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := Run(ctx, cfg)
	if err == nil {
		t.Fatal("want ratio-breach abort")
	}
	var ge *GapAbortError
	if !errors.As(err, &ge) {
		t.Fatalf("err=%T, want *GapAbortError", err)
	}
	if !strings.Contains(ge.Reason, "ratio") {
		t.Errorf("Reason=%q, want ratio phrasing", ge.Reason)
	}
}

func TestRun_GapPolicy_SkipFirstContentGuard(t *testing.T) {
	// Confirms the guard actually gates on SegmentsDone==0
	// rather than on "seg at index 0 failed." With the flag
	// on, a first-segment failure is evaluated purely against
	// the ratio ceiling. With ratio=1.0, any gap is under, so
	// the first failure becomes an accepted gap rather than a
	// guard abort.
	live := &liveServer{
		kind: SegmentKindTS, maxSegments: 3, windowSize: 3,
		baseSeq: 0, tickInterval: 1,
	}
	s := &failingSegmentServer{live: live, fail: map[int]bool{0: true}}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	dir := t.TempDir()
	cfg := newPolicyJob(t, srv, dir, GapPolicy{
		SkipFirstContentGuard: true,
		// 1.0 means "accept any ratio" — test is gating on the
		// guard flag, not the ratio.
		MaxGapRatio: 1.0,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if result.SegmentsGaps != 1 {
		t.Errorf("SegmentsGaps=%d, want 1", result.SegmentsGaps)
	}
	if result.SegmentsDone != 2 {
		t.Errorf("SegmentsDone=%d, want 2", result.SegmentsDone)
	}
}

// TestRun_GapPolicy_AbortCancelsPoller guards the cleanup path.
// A gap-policy abort must cancel both the poller and the pool so
// neither keeps working after Run returns. Watches both signals
// (playlist polls and segment fetches) — the init-fetch test
// catches the same shape for the bootstrap path; this one covers
// mid-stream aborts.
func TestRun_GapPolicy_AbortCancelsPoller(t *testing.T) {
	var playlistPolls, segFetches int32
	live := &liveServer{
		kind: SegmentKindTS, maxSegments: 100, windowSize: 3,
		baseSeq: 0, tickInterval: 1,
	}
	s := &failingSegmentServer{live: live, fail: map[int]bool{0: true}}
	baseHandler := s.handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/playlist.m3u8":
			atomic.AddInt32(&playlistPolls, 1)
		case strings.HasPrefix(r.URL.Path, "/seg/"):
			atomic.AddInt32(&segFetches, 1)
		}
		baseHandler.ServeHTTP(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := newPolicyJob(t, srv, dir, GapPolicy{})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := Run(ctx, cfg)
	if err == nil {
		t.Fatal("want abort error")
	}

	pollsBefore := atomic.LoadInt32(&playlistPolls)
	segsBefore := atomic.LoadInt32(&segFetches)
	time.Sleep(1200 * time.Millisecond) // > one TargetDuration tick + slack
	if got := atomic.LoadInt32(&playlistPolls); got != pollsBefore {
		t.Errorf("playlist polls kept firing after abort: before=%d after=%d", pollsBefore, got)
	}
	if got := atomic.LoadInt32(&segFetches); got != segsBefore {
		t.Errorf("segment fetches kept firing after abort: before=%d after=%d", segsBefore, got)
	}
}

