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

// adPodServer serves a fixed playlist with a stitched-ad DateRange
// spanning seqs [baseSeq+adStart, baseSeq+adStart+adLen), plus
// ENDLIST so the test finishes on a known boundary. Tracks which
// segment seqs were actually fetched so the test can assert ads
// were skipped at the HTTP level, not just in the tally.
type adPodServer struct {
	baseSeq      int
	maxSegments  int
	adStart      int // offset into [baseSeq, baseSeq+maxSegments) where the ad pod begins
	adLen        int // number of ad segments
	tickInterval int // target duration in seconds (also segment duration)

	mu      sync.Mutex
	fetched map[int]int // seq → fetch count
}

// adT0 is the anchor timestamp for EXT-X-PROGRAM-DATE-TIME on
// segment baseSeq. Every segment is `tickInterval` seconds after
// its predecessor so the ad DateRange math lines up.
var adT0 = time.Date(2026, 4, 12, 13, 22, 0, 0, time.UTC)

func (s *adPodServer) playlist() string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:6\n")
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n", s.tickInterval)
	fmt.Fprintf(&b, "#EXT-X-MEDIA-SEQUENCE:%d\n", s.baseSeq)

	// Ad-pod DateRange: spans exactly the adLen segments starting
	// at offset adStart.
	adStart := adT0.Add(time.Duration(s.adStart*s.tickInterval) * time.Second)
	adDur := float64(s.adLen * s.tickInterval)
	fmt.Fprintf(&b,
		"#EXT-X-DATERANGE:ID=\"stitched-ad-test\",CLASS=\"twitch-stitched-ad\",START-DATE=\"%s\",DURATION=%.1f\n",
		adStart.UTC().Format("2006-01-02T15:04:05.000Z"), adDur)

	for i := range s.maxSegments {
		seq := s.baseSeq + i
		pdt := adT0.Add(time.Duration(i*s.tickInterval) * time.Second)
		fmt.Fprintf(&b, "#EXT-X-PROGRAM-DATE-TIME:%s\n",
			pdt.UTC().Format("2006-01-02T15:04:05.000Z"))
		fmt.Fprintf(&b, "#EXTINF:%d.000,\n", s.tickInterval)
		fmt.Fprintf(&b, "/seg/%d.ts\n", seq)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

func (s *adPodServer) recordFetch(seq int) {
	s.mu.Lock()
	s.fetched[seq]++
	s.mu.Unlock()
}

func (s *adPodServer) fetchCount(seq int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fetched[seq]
}

func (s *adPodServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/playlist.m3u8", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(w, s.playlist())
	})
	mux.HandleFunc("/seg/", func(w http.ResponseWriter, r *http.Request) {
		var seq int
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/seg/"), ".ts")
		if _, err := fmt.Sscanf(name, "%d", &seq); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		s.recordFetch(seq)
		payload := fmt.Appendf(nil, "seg-%d-payload", seq)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		_, _ = w.Write(payload)
	})
	return mux
}

// TestRun_StitchedAdSegmentsSkipped is the end-to-end regression:
// ad-pod segments must never hit /seg/, must not appear in
// SegmentsDone, and must be counted in SegmentsAdGaps distinctly
// from SegmentsGaps.
func TestRun_StitchedAdSegmentsSkipped(t *testing.T) {
	// 10 segments total; ad pod at offset 3..6 (4 ad segments).
	// Expected outcome: done=6, ad_gaps=4, gaps=0.
	s := &adPodServer{
		baseSeq:      100,
		maxSegments:  10,
		adStart:      3,
		adLen:        4,
		tickInterval: 1,
		fetched:      make(map[int]int),
	}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	dir := t.TempDir()
	cfg := JobConfig{
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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Non-ad segments fetched exactly once.
	for _, seq := range []int{100, 101, 102, 107, 108, 109} {
		if got := s.fetchCount(seq); got != 1 {
			t.Errorf("seq=%d fetched %d times, want 1 (non-ad)", seq, got)
		}
	}
	// Ad segments never fetched.
	for _, seq := range []int{103, 104, 105, 106} {
		if got := s.fetchCount(seq); got != 0 {
			t.Errorf("seq=%d fetched %d times, want 0 (ad, must be skipped)", seq, got)
		}
	}

	if result.SegmentsDone != 6 {
		t.Errorf("SegmentsDone=%d, want 6", result.SegmentsDone)
	}
	if result.SegmentsAdGaps != 4 {
		t.Errorf("SegmentsAdGaps=%d, want 4", result.SegmentsAdGaps)
	}
	if result.SegmentsGaps != 0 {
		t.Errorf("SegmentsGaps=%d, want 0 (ads should NOT count as quality gaps)", result.SegmentsGaps)
	}

	// On-disk files: 6 non-ad segments, no ad segments.
	entries, _ := os.ReadDir(dir)
	var tsFiles int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".ts") {
			tsFiles++
		}
	}
	if tsFiles != 6 {
		t.Errorf("on-disk .ts files=%d, want 6", tsFiles)
	}
	// Spot-check: ad seq 104's file must not exist.
	if _, err := os.Stat(filepath.Join(dir, "104.ts")); !os.IsNotExist(err) {
		t.Errorf("104.ts should not exist on disk, err=%v", err)
	}
}

// TestRun_AdHeavyStreamDoesNotTripMaxGapRatio confirms ad-gaps
// are excluded from the gap-policy ratio check. With MaxGapRatio
// very tight (1%) and an ad pod that's 40% of the stream, a
// naive counter would abort; the correct behavior is to complete
// cleanly because ads are categorically separate from failures.
func TestRun_AdHeavyStreamDoesNotTripMaxGapRatio(t *testing.T) {
	s := &adPodServer{
		baseSeq:      0,
		maxSegments:  10,
		adStart:      2,
		adLen:        4,
		tickInterval: 1,
		fetched:      make(map[int]int),
	}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	dir := t.TempDir()
	cfg := JobConfig{
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
		GapPolicy: GapPolicy{
			MaxGapRatio: 0.01, // 1% — would trip on any real gap
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("want nil (ads don't count against ratio), got %v", err)
	}
	if result.SegmentsGaps != 0 {
		t.Errorf("SegmentsGaps=%d, want 0", result.SegmentsGaps)
	}
	if result.SegmentsAdGaps != 4 {
		t.Errorf("SegmentsAdGaps=%d, want 4", result.SegmentsAdGaps)
	}
}

// TestRun_ProgressReportsAdGapsSeparately drains the Progress
// channel while a run is in flight and confirms the terminal
// event carries SegmentsAdGaps matching the ad-pod size.
func TestRun_ProgressReportsAdGapsSeparately(t *testing.T) {
	s := &adPodServer{
		baseSeq:      0,
		maxSegments:  8,
		adStart:      2,
		adLen:        2,
		tickInterval: 1,
		fetched:      make(map[int]int),
	}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	dir := t.TempDir()
	progress := make(chan Progress, 64)
	cfg := JobConfig{
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
		Progress:           progress,
	}

	var lastAdGaps atomic.Int64
	drained := make(chan struct{})
	go func() {
		for p := range progress {
			lastAdGaps.Store(p.SegmentsAdGaps)
		}
		close(drained)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := Run(ctx, cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	<-drained

	if result.SegmentsAdGaps != 2 {
		t.Errorf("JobResult.SegmentsAdGaps=%d, want 2", result.SegmentsAdGaps)
	}
	if got := lastAdGaps.Load(); got != 2 {
		t.Errorf("last Progress.SegmentsAdGaps=%d, want 2", got)
	}
}
