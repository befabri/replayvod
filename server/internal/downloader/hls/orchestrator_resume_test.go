package hls

import (
	"context"
	"errors"
	"fmt"
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
