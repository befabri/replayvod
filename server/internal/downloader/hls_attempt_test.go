package downloader

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/background"
	"github.com/befabri/replayvod/server/internal/downloader/hls"
)

func progressAttemptConfig(t *testing.T) hls.JobConfig {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/playlist.m3u8" {
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:1\n#EXTINF:1,\n/1.ts\n#EXT-X-ENDLIST\n")
			return
		}
		fmt.Fprint(w, "segment")
	}))
	t.Cleanup(srv.Close)
	log := slog.New(slog.DiscardHandler)
	return hls.JobConfig{
		MediaPlaylistURL: srv.URL + "/playlist.m3u8", WorkDir: t.TempDir(),
		Fetcher:        hls.NewFetcher(srv.Client(), hls.FetcherConfig{}, log),
		PlaylistClient: srv.Client(), SegmentConcurrency: 1, Log: log,
	}
}

func TestHLSProgressRetainsRecordingOwnershipThroughShutdown(t *testing.T) {
	cfg := progressAttemptConfig(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	output := make(chan Progress, 16)
	emitter := newProgressEmitter("progress", "video", output, func(p Progress) {
		if p.SegmentsDone > 0 {
			close(entered)
			<-release
		}
	})
	work := background.New(map[string]int{"live": 1})
	settled := make(chan error, 1)
	if err := work.Start("live", "progress", func(ctx context.Context) error {
		_, err := runHLSAttempt(ctx, emitter, cfg)
		return err
	}, func(err error) { close(output); settled <- err }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("acquisition did not report progress")
	}
	work.Stop()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := work.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown returned while progress was still publishing: %v", err)
	}
	select {
	case err := <-settled:
		t.Fatalf("recording settled before progress finished: %v", err)
	default:
	}
	unblock.Do(func() { close(release) })
	ctx, cancel = context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := work.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-settled; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("late progress failed during settlement: %v", err)
	}
}

func TestHLSProgressFinishesBeforeNextAttemptCountersReset(t *testing.T) {
	var last Progress
	emitter := newProgressEmitter("progress", "video", make(chan Progress, 16), func(p Progress) { last = p })
	for attempt := int64(1); attempt <= 2; attempt++ {
		cfg := progressAttemptConfig(t)
		if _, err := runHLSAttempt(t.Context(), emitter, cfg); err != nil {
			t.Fatal(err)
		}
		if last.SegmentsDone != attempt || last.BytesWritten != attempt*int64(len("segment")) {
			t.Fatalf("attempt %d returned before cumulative progress: %+v", attempt, last)
		}
	}
}

func TestHLSProgressDoesNotDoubleCountAuthRefreshSeeds(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		done, gaps                 int64
		previousDone, previousGaps int64
	}{
		{name: "completed segments", done: 1},
		{name: "completed segments and accepted gaps", done: 4, gaps: 1},
		{name: "previous finalized parts", done: 4, gaps: 1, previousDone: 10, previousGaps: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var last Progress
			emitter := newProgressEmitter("progress", "video", make(chan Progress, 16), func(p Progress) { last = p })
			doneBefore, gapsBefore := tc.previousDone+tc.done, tc.previousGaps+tc.gaps
			bytesBefore := doneBefore * int64(len("segment"))
			// The previous attempt has already published these counters. The
			// auth-refresh loop also seeds HLS's per-part gap policy with them.
			emitter.bridge(hls.Progress{SegmentsDone: doneBefore, SegmentsGaps: gapsBefore, SegmentsAdGaps: 2, BytesWritten: bytesBefore})
			for retry := int64(1); retry <= 3; retry++ {
				cfg := progressAttemptConfig(t)
				cfg.SeedSegmentsDone, cfg.SeedSegmentsGaps = tc.done+retry-1, tc.gaps
				cfg.GapPolicy.MaxGapRatio = 0.5
				result, err := runHLSAttempt(t.Context(), emitter, cfg)
				if err != nil {
					t.Fatal(err)
				}
				// Keep the policy seeds: removing them from HLS would hide the
				// progress bug by breaking cumulative gap accounting instead.
				if result.SegmentsDone != tc.done+retry || result.SegmentsGaps != tc.gaps || result.SegmentsAdGaps != 0 {
					t.Fatalf("retry lost the policy seeds: %+v", result)
				}
				wantDone, wantBytes := doneBefore+retry, bytesBefore+retry*int64(len("segment"))
				if last.SegmentsDone != wantDone || last.SegmentsGaps != gapsBefore || last.SegmentsAdGaps != 2 || last.BytesWritten != wantBytes || last.SegmentsTotal != wantDone+gapsBefore {
					t.Fatalf("retry %d progress = %+v; want done=%d gaps=%d ad gaps=2 bytes=%d total=%d",
						retry, last, wantDone, gapsBefore, wantBytes, wantDone+gapsBefore)
				}
			}
			// Opening another part resets HLS's policy seeds while recording
			// totals still include every preceding part and auth retry.
			emitter.setPart(2)
			if _, err := runHLSAttempt(t.Context(), emitter, progressAttemptConfig(t)); err != nil {
				t.Fatal(err)
			}
			if last.SegmentsDone != doneBefore+4 || last.SegmentsGaps != gapsBefore || last.SegmentsAdGaps != 2 || last.SegmentsTotal != doneBefore+gapsBefore+4 {
				t.Fatalf("part transition lost prior progress: %+v", last)
			}
		})
	}
}

func TestHLSProgressPanicReachesRecordingSettlement(t *testing.T) {
	cfg := progressAttemptConfig(t)
	output := make(chan Progress, 16)
	emitter := newProgressEmitter("progress", "video", output, func(p Progress) {
		if p.SegmentsDone > 0 {
			panic("progress observer failed")
		}
	})
	work := background.New(nil)
	settled := make(chan error, 1)
	if err := work.Start("live", "progress", func(ctx context.Context) error {
		_, err := runHLSAttempt(ctx, emitter, cfg)
		return err
	}, func(err error) { close(output); settled <- err }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := work.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-settled; err == nil || !strings.Contains(err.Error(), "worker panic: progress observer failed") {
		t.Fatalf("progress panic did not reach settlement: %v", err)
	}
	work.Stop()
}
