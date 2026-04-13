package hls

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestRun_StartMediaSeq_SkipsCommittedSegments confirms that
// Run(cfg.StartMediaSeq = N) only fetches segments with
// MediaSeq >= N. Segments 0..N-1 stay out of the fetch flow.
func TestRun_StartMediaSeq_SkipsCommittedSegments(t *testing.T) {
	// Track which segments the server was asked for — the test
	// asserts seq 0 and 1 are never requested when resuming at 2.
	var fetches syncMapInt
	live := &liveServer{
		kind:         SegmentKindTS,
		maxSegments:  5,
		windowSize:   5, // big enough that all 5 appear in the first poll
		baseSeq:      0,
		tickInterval: 1,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if segPath, ok := strings.CutPrefix(r.URL.Path, "/seg/"); ok {
			name := strings.TrimSuffix(segPath, ".ts")
			fetches.Inc(name)
		}
		live.handler().ServeHTTP(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := newJob(t, srv, dir)
	cfg.StartMediaSeq = 2

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.SegmentsDone != 3 {
		t.Errorf("SegmentsDone=%d, want 3 (seqs 2,3,4)", result.SegmentsDone)
	}
	// Segments 0 and 1 must never have been requested.
	for _, unwanted := range []string{"0", "1"} {
		if n := fetches.Get(unwanted); n > 0 {
			t.Errorf("seg %s fetched %d times; resume should have skipped it", unwanted, n)
		}
	}
	// Segments 2, 3, 4 each fetched exactly once.
	for _, wanted := range []string{"2", "3", "4"} {
		if n := fetches.Get(wanted); n != 1 {
			t.Errorf("seg %s fetched %d times, want 1", wanted, n)
		}
	}
	// Files on disk match.
	for seq := 2; seq <= 4; seq++ {
		name := fmt.Sprintf("%d.ts", seq)
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("seg %d file: %v", seq, err)
		}
	}
	if result.LastMediaSeq != 4 {
		t.Errorf("LastMediaSeq=%d, want 4", result.LastMediaSeq)
	}
}

// TestRun_SegmentAuthReturnsErrPlaylistAuth simulates a stale
// signed URL: every segment returns 403 with no playlist error.
// The drain loop's IsAuth branch converts the first segment auth
// failure into ErrPlaylistAuth so the outer refresh caller can
// catch it and re-run Stages 1-3.
func TestRun_SegmentAuthReturnsErrPlaylistAuth(t *testing.T) {
	live := &liveServer{
		kind:         SegmentKindTS,
		maxSegments:  10,
		windowSize:   3,
		baseSeq:      0,
		tickInterval: 1,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/seg/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		live.handler().ServeHTTP(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := newJob(t, srv, dir)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := Run(ctx, cfg)
	if err == nil {
		t.Fatal("want auth error")
	}
	if !errors.Is(err, ErrPlaylistAuth) {
		t.Errorf("err=%v, want errors.Is(ErrPlaylistAuth)", err)
	}
}

// TestRun_LastMediaSeqAdvancesOnGap confirms LastMediaSeq
// tracks accepted gaps too, not just successes. Resume needs
// this so we don't re-fetch a segment that already took a
// permanent failure.
func TestRun_LastMediaSeqAdvancesOnGap(t *testing.T) {
	live := &liveServer{
		kind:         SegmentKindTS,
		maxSegments:  10,
		windowSize:   3,
		baseSeq:      0,
		tickInterval: 1,
	}
	// Seg 5 fails permanently but is accepted as a gap under
	// MaxGapRatio=0.25. The rest succeed.
	fs := &failingSegmentServer{live: live, fail: map[int]bool{5: true}}
	srv := httptest.NewServer(fs.handler())
	defer srv.Close()

	dir := t.TempDir()
	cfg := newPolicyJob(t, srv, dir, GapPolicy{MaxGapRatio: 0.25})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.LastMediaSeq != 9 {
		t.Errorf("LastMediaSeq=%d, want 9 (highest observed seq, success or gap)", result.LastMediaSeq)
	}
	if result.SegmentsGaps != 1 {
		t.Errorf("SegmentsGaps=%d, want 1", result.SegmentsGaps)
	}
}

// TestRun_AuthErrorSeqsPopulatedThenRefetched is the end-to-end
// regression for the post-refresh refetch path: a server that
// 403s a specific seq once then 200s it must produce a final
// workdir where that seq is on disk, not a hole.
//
// First run: seq 2 → 403 → AuthErrorSeqs collects it, Run returns
// ErrPlaylistAuth. Second run: RefetchSeqs=[2], seq 2 now 200s,
// ends up committed. Simulates what fetchWithAuthRefresh does
// across an auth-refresh boundary without the full Twitch stack.
func TestRun_AuthErrorSeqsPopulatedThenRefetched(t *testing.T) {
	live := &liveServer{
		kind:         SegmentKindTS,
		maxSegments:  5,
		windowSize:   5,
		baseSeq:      0,
		tickInterval: 1,
	}
	// Seg 2 403s on its first fetch, 200s thereafter.
	var seg2Hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/seg/2.ts" {
			if seg2Hits.Add(1) == 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}
		live.handler().ServeHTTP(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()

	// First run: seq 2's initial 403 trips the drain's auth
	// branch and aborts with ErrPlaylistAuth. Expect seq 2 in
	// AuthErrorSeqs.
	cfg := newJob(t, srv, dir)
	ctx1, cancel1 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel1()
	result1, err := Run(ctx1, cfg)
	if !errors.Is(err, ErrPlaylistAuth) {
		t.Fatalf("first run err=%v, want ErrPlaylistAuth", err)
	}
	if len(result1.AuthErrorSeqs) == 0 {
		t.Fatal("AuthErrorSeqs empty, want seq 2 present")
	}
	if !slices.Contains(result1.AuthErrorSeqs, 2) {
		t.Errorf("AuthErrorSeqs=%v, want to include 2", result1.AuthErrorSeqs)
	}
	if _, err := os.Stat(filepath.Join(dir, "2.ts")); !os.IsNotExist(err) {
		t.Errorf("2.ts exists after first run; expected hole until refetch. err=%v", err)
	}

	// Second run: cursor past seq 2 via StartMediaSeq, but
	// RefetchSeqs tells the poller to emit seq 2 anyway. Worker
	// re-fetches → this time 200 → commit.
	cfg2 := newJob(t, srv, dir)
	cfg2.StartMediaSeq = result1.LastMediaSeq + 1
	cfg2.RefetchSeqs = result1.AuthErrorSeqs
	ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel2()
	result2, err := Run(ctx2, cfg2)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2.ts")); err != nil {
		t.Errorf("2.ts missing after refetch: %v", err)
	}
	if len(result2.AuthErrorSeqs) != 0 {
		t.Errorf("second run AuthErrorSeqs=%v, want empty (refetch succeeded)", result2.AuthErrorSeqs)
	}
	// Total server hits on seg 2: one 403 + one 200 = 2.
	if got := seg2Hits.Load(); got != 2 {
		t.Errorf("seg 2 fetch count=%d, want 2 (initial 403 + refetch 200)", got)
	}
}

// TestRun_WindowRollCallbackFiresWithLostRange validates the
// resume-path correctness fix: when StartMediaSeq > 0 (resume
// attempt) and the playlist head is already past it, the
// orchestrator invokes cfg.OnWindowRoll with the inclusive
// [from, to] range of lost segments. Without this, the resume
// state's frontier would stall forever waiting on segments the
// CDN no longer serves.
func TestRun_WindowRollCallbackFiresWithLostRange(t *testing.T) {
	// Playlist head is 100; caller asks to resume at 50.
	// Expected lost range: [50, 99].
	live := &liveServer{
		kind:         SegmentKindTS,
		maxSegments:  5,
		windowSize:   5,
		baseSeq:      100,
		tickInterval: 1,
	}
	srv := httptest.NewServer(live.handler())
	defer srv.Close()

	dir := t.TempDir()
	cfg := newJob(t, srv, dir)
	cfg.StartMediaSeq = 50

	var called atomic.Int32
	var gotFrom, gotTo atomic.Int64
	cfg.OnWindowRoll = func(from, to int64) {
		called.Add(1)
		gotFrom.Store(from)
		gotTo.Store(to)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := Run(ctx, cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := called.Load(); got != 1 {
		t.Fatalf("OnWindowRoll called %d times, want 1", got)
	}
	if gotFrom.Load() != 50 {
		t.Errorf("OnWindowRoll from=%d, want 50", gotFrom.Load())
	}
	if gotTo.Load() != 99 {
		t.Errorf("OnWindowRoll to=%d, want 99 (playlist head - 1)", gotTo.Load())
	}
}

// TestRun_WindowRollCallbackSkippedWhenNoRoll: sanity check that
// resumes landing inside the playlist window (no loss) do NOT
// fire the callback. StartMediaSeq = playlistHead is the edge
// case — zero lost segments, no callback.
func TestRun_WindowRollCallbackSkippedWhenNoRoll(t *testing.T) {
	live := &liveServer{
		kind:         SegmentKindTS,
		maxSegments:  5,
		windowSize:   5,
		baseSeq:      100,
		tickInterval: 1,
	}
	srv := httptest.NewServer(live.handler())
	defer srv.Close()

	dir := t.TempDir()
	cfg := newJob(t, srv, dir)
	cfg.StartMediaSeq = 100 // lands exactly on the playlist head

	var called atomic.Int32
	cfg.OnWindowRoll = func(from, to int64) { called.Add(1) }

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := Run(ctx, cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := called.Load(); got != 0 {
		t.Errorf("OnWindowRoll called %d times, want 0 (no window roll)", got)
	}
}

// TestRun_WindowRollCallbackSkippedOnFreshJob: a StartMediaSeq=0
// run is a fresh job; the poller's WindowRollFrom is 0 and the
// orchestrator's guard (WindowRollFrom > 0) must suppress the
// callback. A false positive here would record a phantom
// restart_window_rolled gap for every fresh recording.
func TestRun_WindowRollCallbackSkippedOnFreshJob(t *testing.T) {
	live := &liveServer{
		kind:         SegmentKindTS,
		maxSegments:  5,
		windowSize:   5,
		baseSeq:      100,
		tickInterval: 1,
	}
	srv := httptest.NewServer(live.handler())
	defer srv.Close()

	dir := t.TempDir()
	cfg := newJob(t, srv, dir) // StartMediaSeq = 0

	var called atomic.Int32
	cfg.OnWindowRoll = func(from, to int64) { called.Add(1) }

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := Run(ctx, cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := called.Load(); got != 0 {
		t.Errorf("OnWindowRoll called %d times on fresh job, want 0", got)
	}
}

// syncMapInt is a tiny wrapper over sync.Map specialized for
// int counters keyed by string. Avoids the interface-casting
// noise in the fetch counter.
type syncMapInt struct{ m sync.Map }

func (s *syncMapInt) Inc(k string) {
	c, _ := s.m.LoadOrStore(k, new(atomic.Int32))
	c.(*atomic.Int32).Add(1)
}
func (s *syncMapInt) Get(k string) int32 {
	c, ok := s.m.Load(k)
	if !ok {
		return 0
	}
	return c.(*atomic.Int32).Load()
}
