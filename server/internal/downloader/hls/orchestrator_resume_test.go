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

// TestRun_StartMediaSeq_SkipsCommittedSegments checks that recovery never reacquires saved sequences.
func TestRun_StartMediaSeq_SkipsCommittedSegments(t *testing.T) {
	var fetches syncMapInt
	live := &liveServer{
		kind:         SegmentKindTS,
		maxSegments:  5,
		windowSize:   5, // keep earlier sequences visible during renewal
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
	for _, unwanted := range []string{"0", "1"} {
		if n := fetches.Get(unwanted); n > 0 {
			t.Errorf("seg %s fetched %d times; resume should have skipped it", unwanted, n)
		}
	}
	for _, wanted := range []string{"2", "3", "4"} {
		if n := fetches.Get(wanted); n != 1 {
			t.Errorf("seg %s fetched %d times, want 1", wanted, n)
		}
	}
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

// TestRun_SegmentAuthReturnsErrPlaylistAuth checks the renewal sentinel for
// a valid playlist whose signed segment URLs have expired.
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

// TestRun_LastMediaSeqAdvancesOnGap guards against retrying accepted permanent loss.
func TestRun_LastMediaSeqAdvancesOnGap(t *testing.T) {
	live := &liveServer{
		kind:         SegmentKindTS,
		maxSegments:  10,
		windowSize:   3,
		baseSeq:      0,
		tickInterval: 1,
	}
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

// TestRun_AuthErrorSeqsPopulatedThenRefetched checks that renewal fills a
// sequence left unresolved below the forward cursor.
func TestRun_AuthErrorSeqsPopulatedThenRefetched(t *testing.T) {
	live := &liveServer{
		kind:         SegmentKindTS,
		maxSegments:  5,
		windowSize:   5,
		baseSeq:      0,
		tickInterval: 1,
	}
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

	// An unresolved sequence must bypass the forward cursor after renewal.
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
	if got := seg2Hits.Load(); got != 2 {
		t.Errorf("seg 2 fetch count=%d, want 2 (initial 403 + refetch 200)", got)
	}
}

// TestRun_RefetchHandlesMultipleSeqs checks that concurrently failed sequences
// survive renewal even when cancellation prevents some outcomes from reaching the drain.
func TestRun_RefetchHandlesMultipleSeqs(t *testing.T) {
	live := &liveServer{
		kind:         SegmentKindTS,
		maxSegments:  5,
		windowSize:   5,
		baseSeq:      0,
		tickInterval: 1,
	}
	// Adjacent failures can both reach the drain before cancellation.
	var seg0Hits, seg1Hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/seg/0.ts":
			if seg0Hits.Add(1) == 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		case "/seg/1.ts":
			if seg1Hits.Add(1) == 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}
		live.handler().ServeHTTP(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := newJob(t, srv, dir)
	ctx1, cancel1 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel1()
	result1, err := Run(ctx1, cfg)
	if !errors.Is(err, ErrPlaylistAuth) {
		t.Fatalf("first run err=%v, want ErrPlaylistAuth", err)
	}
	// Cancellation can prevent the second authorization result from reaching the drain.
	if len(result1.AuthErrorSeqs) == 0 {
		t.Fatalf("AuthErrorSeqs empty, want at least one of 0 or 1")
	}

	// Repeated renewal must recover sequences whose outcomes were lost to cancellation.
	prev := result1
	budget := 5
	for budget > 0 {
		if fileExists(dir, "0.ts") && fileExists(dir, "1.ts") {
			break
		}
		cfgN := newJob(t, srv, dir)
		cfgN.StartMediaSeq = prev.LastMediaSeq + 1
		cfgN.RefetchSeqs = prev.AuthErrorSeqs
		// Missing files reveal canceled outcomes that never reached the drain.
		for _, s := range []int64{0, 1} {
			if !fileExists(dir, fmt.Sprintf("%d.ts", s)) && !slices.Contains(cfgN.RefetchSeqs, s) {
				cfgN.RefetchSeqs = append(cfgN.RefetchSeqs, s)
			}
		}
		ctxN, cancelN := context.WithTimeout(context.Background(), 15*time.Second)
		rN, err := Run(ctxN, cfgN)
		cancelN()
		if err != nil && !errors.Is(err, ErrPlaylistAuth) {
			t.Fatalf("refetch loop iter %d: %v", 6-budget, err)
		}
		prev = rN
		budget--
	}

	for _, seq := range []string{"0.ts", "1.ts"} {
		if _, err := os.Stat(filepath.Join(dir, seq)); err != nil {
			t.Errorf("%s missing after refetch chain: %v", seq, err)
		}
	}
}

// TestRun_MalformedSegmentNotFetched checks that malformed content is never fetched
// or counted as an advertisement, regardless of whether commits precede its policy check.
func TestRun_MalformedSegmentNotFetched(t *testing.T) {
	playlist := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:2.000,
/seg/0.ts
#EXTINF:2.000,
/seg/1.ts
#EXTINF:0,
/seg/2.ts
#EXTINF:2.000,
/seg/3.ts
#EXTINF:2.000,
/seg/4.ts
#EXT-X-ENDLIST
`
	var segHits atomic.Int32
	var seg2Hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/playlist.m3u8" {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte(playlist))
			return
		}
		if r.URL.Path == "/seg/2.ts" {
			seg2Hits.Add(1)
		}
		segHits.Add(1)
		_, _ = w.Write(fmt.Appendf(nil, "seg-%s-payload", r.URL.Path))
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := newJob(t, srv, dir)
	// Disable the first-content guard to isolate the ratio check under either drain order.
	cfg.GapPolicy.SkipFirstContentGuard = true
	cfg.GapPolicy.MaxGapRatio = 0.5

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := Run(ctx, cfg)

	// Malformed segments must never reach acquisition, regardless of outcome ordering.
	if seg2Hits.Load() != 0 {
		t.Errorf("/seg/2.ts fetched %d times; poller must skip malformed segs before enqueue", seg2Hits.Load())
	}

	if err != nil {
		// A skip processed before enough commits can legitimately exceed the ratio.
		var gapErr *GapAbortError
		if !errors.As(err, &gapErr) {
			t.Fatalf("err=%v, want *GapAbortError", err)
		}
		if !strings.Contains(gapErr.Reason, "malformed") {
			t.Errorf("gap reason=%q, want reference to malformed", gapErr.Reason)
		}
		return
	}
	if result.SegmentsDone != 4 {
		t.Errorf("SegmentsDone=%d, want 4 (seqs 0,1,3,4)", result.SegmentsDone)
	}
	// Malformed content must not inherit the advertisement policy exemption.
	if result.SegmentsAdGaps != 0 {
		t.Errorf("SegmentsAdGaps=%d, want 0 (malformed is not an ad)", result.SegmentsAdGaps)
	}
	if result.SegmentsGaps != 1 {
		t.Errorf("SegmentsGaps=%d, want 1 (the malformed seg)", result.SegmentsGaps)
	}
	if result.LastMediaSeq < 4 {
		t.Errorf("LastMediaSeq=%d, want at least 4", result.LastMediaSeq)
	}
}

// TestRun_MalformedFirstSegTripsFirstContentGuard prevents successful capture
// when malformed content is skipped before any real media is committed.
func TestRun_MalformedFirstSegTripsFirstContentGuard(t *testing.T) {
	playlist := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:0,
/seg/0.ts
#EXTINF:2.000,
/seg/1.ts
#EXT-X-ENDLIST
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/playlist.m3u8" {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte(playlist))
			return
		}
		_, _ = w.Write(fmt.Appendf(nil, "seg-%s-payload", r.URL.Path))
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := newJob(t, srv, dir)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := Run(ctx, cfg)
	var gapErr *GapAbortError
	if !errors.As(err, &gapErr) {
		t.Fatalf("err=%v, want *GapAbortError for preroll-malformed guard", err)
	}
	if !strings.Contains(gapErr.Reason, "no content segment committed yet") {
		t.Errorf("gap reason=%q, want first-content-guard reason", gapErr.Reason)
	}
}

// TestRun_MalformedRatioTripsGapPolicy checks that excessive malformed content
// fails capture with a typed loss error.
func TestRun_MalformedRatioTripsGapPolicy(t *testing.T) {
	playlist := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:2.000,
/seg/0.ts
#EXTINF:0,
/seg/1.ts
#EXTINF:0,
/seg/2.ts
#EXTINF:0,
/seg/3.ts
#EXTINF:2.000,
/seg/4.ts
#EXT-X-ENDLIST
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/playlist.m3u8" {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte(playlist))
			return
		}
		_, _ = w.Write(fmt.Appendf(nil, "seg-%s-payload", r.URL.Path))
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := newJob(t, srv, dir)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := Run(ctx, cfg)
	var gapErr *GapAbortError
	if !errors.As(err, &gapErr) {
		t.Fatalf("err=%v, want *GapAbortError", err)
	}
}

// TestRun_RefetchRolledOffStaysAsGap checks that permitted expired retries
// count as permanent loss while acquisition continues over available media.
func TestRun_RefetchRolledOffStaysAsGap(t *testing.T) {
	live := &liveServer{
		kind:         SegmentKindTS,
		maxSegments:  5,
		windowSize:   5,
		baseSeq:      50,
		tickInterval: 1,
	}
	srv := httptest.NewServer(live.handler())
	defer srv.Close()

	dir := t.TempDir()
	cfg := newJob(t, srv, dir)
	cfg.StartMediaSeq = 50
	cfg.RefetchSeqs = []int64{10} // rolled off — not in playlist
	cfg.SeedSegmentsDone = 100
	cfg.GapPolicy.MaxGapRatio = 1

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.SegmentsDone != 105 || result.SegmentsGaps != 1 {
		t.Fatalf("expired refetch accounting: done=%d gaps=%d, want 105 and 1", result.SegmentsDone, result.SegmentsGaps)
	}
	// Permanently lost retries must not request another authorization attempt.
	if slices.Contains(result.AuthErrorSeqs, 10) {
		t.Error("AuthErrorSeqs contains rolled-off seq; should be empty")
	}
	// The expired sequence must never acquire a file.
	if _, err := os.Stat(filepath.Join(dir, "10.ts")); !os.IsNotExist(err) {
		t.Errorf("10.ts should not exist; err=%v", err)
	}
}

// TestRun_WindowRollCallbackFiresWithLostRange checks that anchored recovery
// records the lost range before advancing its durable frontier.
func TestRun_WindowRollCallbackFiresWithLostRange(t *testing.T) {
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
	cfg.Recovering = true

	var called atomic.Int32
	var gotFrom, gotTo atomic.Int64
	cfg.OnWindowRoll = func(from, to int64, _ time.Duration) {
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

// TestRun_WindowRollCallbackSkippedWhenNoRoll checks the inclusive recovery boundary.
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
	cfg.OnWindowRoll = func(from, to int64, _ time.Duration) { called.Add(1) }

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := Run(ctx, cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := called.Load(); got != 0 {
		t.Errorf("OnWindowRoll called %d times, want 0 (no window roll)", got)
	}
}

// TestRun_WindowRollCallbackSkippedOnFreshJob guards against inventing loss
// before the first observed segment of a fresh recording.
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
	cfg.OnWindowRoll = func(from, to int64, _ time.Duration) { called.Add(1) }

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := Run(ctx, cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := called.Load(); got != 0 {
		t.Errorf("OnWindowRoll called %d times on fresh job, want 0", got)
	}
}

func fileExists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// syncMapInt counts concurrent fixture requests by segment name.
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
