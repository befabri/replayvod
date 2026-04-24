package downloader

import (
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/hls"
)

// drain collects every Progress event sent to ch until the caller
// stops emitting. Returns the last event and the count — tests
// usually care about "did we see N events" and "does the last
// one match expectations."
func drain(ch <-chan Progress) (last Progress, count int) {
	for {
		select {
		case p, ok := <-ch:
			if !ok {
				return last, count
			}
			last = p
			count++
		case <-time.After(50 * time.Millisecond):
			return last, count
		}
	}
}

func TestProgressEmitter_SetStageFiresEvent(t *testing.T) {
	ch := make(chan Progress, 16)
	em := newProgressEmitter("job-1", "video", ch)
	em.setStage("auth")
	em.setStage("playlist")
	em.setStage("done")

	last, count := drain(ch)
	if count != 3 {
		t.Errorf("got %d events, want 3", count)
	}
	if last.Stage != "done" {
		t.Errorf("last stage=%q, want done", last.Stage)
	}
	if last.JobID != "job-1" {
		t.Errorf("JobID=%q", last.JobID)
	}
	if last.RecordingType != "video" {
		t.Errorf("RecordingType=%q, want video", last.RecordingType)
	}
	if last.PartIndex != 1 {
		t.Errorf("PartIndex=%d, want 1", last.PartIndex)
	}
	if last.SegmentsTotal != -1 {
		t.Errorf("SegmentsTotal=%d, want -1 (not finalized)", last.SegmentsTotal)
	}
	if last.Percent != -1 {
		t.Errorf("Percent=%v, want -1 (total unknown)", last.Percent)
	}
}

func TestProgressEmitter_VariantCarriesForward(t *testing.T) {
	ch := make(chan Progress, 16)
	em := newProgressEmitter("job-1", "video", ch)
	fps := 60.0
	em.setVariant("1080", &fps, "h265")
	em.setStage("segments")

	last, _ := drain(ch)
	if last.Quality != "1080" {
		t.Errorf("Quality=%q", last.Quality)
	}
	if last.FPS == nil || *last.FPS != fps {
		t.Errorf("FPS=%v, want %v", last.FPS, fps)
	}
	if last.Codec != "h265" {
		t.Errorf("Codec=%q", last.Codec)
	}
}

func TestProgressEmitter_BridgeAccumulatesCounters(t *testing.T) {
	ch := make(chan Progress, 16)
	em := newProgressEmitter("job-1", "video", ch)
	em.bridge(hls.Progress{SegmentsDone: 1, BytesWritten: 100})
	em.bridge(hls.Progress{SegmentsDone: 2, BytesWritten: 200})
	em.bridge(hls.Progress{SegmentsDone: 3, SegmentsGaps: 1, BytesWritten: 300})

	last, count := drain(ch)
	if count != 3 {
		t.Errorf("events=%d, want 3", count)
	}
	if last.SegmentsDone != 3 {
		t.Errorf("SegmentsDone=%d", last.SegmentsDone)
	}
	if last.SegmentsGaps != 1 {
		t.Errorf("SegmentsGaps=%d", last.SegmentsGaps)
	}
	if last.BytesWritten != 300 {
		t.Errorf("BytesWritten=%d", last.BytesWritten)
	}
}

func TestProgressEmitter_FinalizeSetsTotalAndPercent(t *testing.T) {
	ch := make(chan Progress, 16)
	em := newProgressEmitter("job-1", "video", ch)
	em.bridge(hls.Progress{SegmentsDone: 9, SegmentsGaps: 1, BytesWritten: 900})
	em.finalize()

	last, _ := drain(ch)
	if last.SegmentsTotal != 10 {
		t.Errorf("SegmentsTotal=%d, want 10 (done+gaps)", last.SegmentsTotal)
	}
	// 9 done of 10 total = 90%.
	if last.Percent < 89.9 || last.Percent > 90.1 {
		t.Errorf("Percent=%v, want ~90", last.Percent)
	}
}

func TestProgressEmitter_PercentAt100OnAllDone(t *testing.T) {
	ch := make(chan Progress, 16)
	em := newProgressEmitter("job-1", "video", ch)
	em.bridge(hls.Progress{SegmentsDone: 10, BytesWritten: 1000})
	em.finalize()

	last, _ := drain(ch)
	if last.Percent != 100 {
		t.Errorf("Percent=%v, want 100", last.Percent)
	}
}

func TestComputeSpeed_NotEnoughSamples(t *testing.T) {
	if got := computeSpeed(nil); got != "" {
		t.Errorf("nil samples = %q, want empty", got)
	}
	if got := computeSpeed([]byteSample{{at: time.Now(), bytes: 100}}); got != "" {
		t.Errorf("one sample = %q, want empty", got)
	}
}

func TestComputeSpeed_WindowTooShort(t *testing.T) {
	t0 := time.Now()
	got := computeSpeed([]byteSample{
		{at: t0, bytes: 0},
		{at: t0.Add(10 * time.Millisecond), bytes: 1000}, // < 100ms guard
	})
	if got != "" {
		t.Errorf("sub-100ms window = %q, want empty (too noisy)", got)
	}
}

func TestComputeSpeed_RateScalesToUnit(t *testing.T) {
	// 5 MiB over 1 second = 5 MiB/s.
	t0 := time.Now()
	got := computeSpeed([]byteSample{
		{at: t0, bytes: 0},
		{at: t0.Add(time.Second), bytes: 5 << 20}, // 5 MiB
	})
	if !strings.Contains(got, "MiB/s") {
		t.Errorf("got %q, want MiB/s unit for 5 MiB/s rate", got)
	}
}

func TestComputeSpeed_ZeroOrNegativeDeltaIsEmpty(t *testing.T) {
	// A stalled stream (no bytes moved) reports empty speed
	// rather than "0.00 B/s" — prevents the UI from showing a
	// zero rate that looks like "stuck" when the stream's
	// just quiet.
	t0 := time.Now()
	got := computeSpeed([]byteSample{
		{at: t0, bytes: 5000},
		{at: t0.Add(time.Second), bytes: 5000},
	})
	if got != "" {
		t.Errorf("zero-byte window = %q, want empty", got)
	}
}

func TestAppendSample_DropsExpired(t *testing.T) {
	now := time.Now()
	old := now.Add(-2 * speedWindow)
	samples := []byteSample{
		{at: old, bytes: 1},
		{at: old.Add(time.Second), bytes: 2},
	}
	samples = appendSample(samples, byteSample{at: now, bytes: 100})

	if len(samples) != 1 {
		t.Errorf("len=%d, want 1 (both old samples expired)", len(samples))
	}
	if samples[0].bytes != 100 {
		t.Errorf("retained bytes=%d, want 100", samples[0].bytes)
	}
}

func TestAppendSample_KeepsWithinWindow(t *testing.T) {
	now := time.Now()
	samples := []byteSample{
		{at: now.Add(-5 * time.Second), bytes: 1},
		{at: now.Add(-2 * time.Second), bytes: 2},
	}
	samples = appendSample(samples, byteSample{at: now, bytes: 3})

	if len(samples) != 3 {
		t.Errorf("len=%d, want 3 (all within %v window)", len(samples), speedWindow)
	}
}

func TestComputePercent(t *testing.T) {
	cases := []struct {
		done, total int64
		want        float64
	}{
		{0, -1, -1}, // live
		{0, 0, -1},  // unknown
		{5, 10, 50}, // mid
		{10, 10, 100},
		{11, 10, 100}, // safety clamp
	}
	for _, c := range cases {
		got := computePercent(c.done, c.total)
		if got != c.want {
			t.Errorf("computePercent(%d, %d) = %v, want %v", c.done, c.total, got, c.want)
		}
	}
}

// TestProgressEmitter_StartAttemptPreservesCumulativeState is the
// M2 regression guard: under auth-refresh, hls.Run restarts with
// a fresh internal counter (BytesWritten=0, SegmentsDone=0).
// Without startAttempt() the emitter would overwrite its
// cumulative view with the per-attempt value and regress the UI
// back to zero. With startAttempt() snapshotting a baseline, the
// next attempt's deltas add on top of what was already shown.
func TestProgressEmitter_StartAttemptPreservesCumulativeState(t *testing.T) {
	ch := make(chan Progress, 64)
	e := newProgressEmitter("job-1", "video", ch)

	// Attempt 1: hls reports 10 segments, 1 MiB.
	e.bridge(hls.Progress{BytesWritten: 1 << 20, SegmentsDone: 10})
	mid, _ := drain(ch)
	if mid.BytesWritten != 1<<20 || mid.SegmentsDone != 10 {
		t.Fatalf("after attempt 1: bytes=%d done=%d, want 1MiB/10",
			mid.BytesWritten, mid.SegmentsDone)
	}

	// Simulate an auth-refresh boundary.
	e.startAttempt()

	// Attempt 2: hls reports its own fresh counter starting at 0,
	// then grows. The emitter must keep attempt 1's cumulative
	// state as the baseline.
	e.bridge(hls.Progress{BytesWritten: 512 << 10, SegmentsDone: 5})
	after, _ := drain(ch)
	wantBytes := int64(1<<20 + (512 << 10)) // 1.5 MiB
	wantDone := int64(15)
	if after.BytesWritten != wantBytes || after.SegmentsDone != wantDone {
		t.Errorf("after attempt 2: bytes=%d done=%d, want %d/%d (baseline + attempt delta)",
			after.BytesWritten, after.SegmentsDone, wantBytes, wantDone)
	}
}

func TestComputeETA_EmptyOnLiveOrUnknownRate(t *testing.T) {
	// No SegmentsTotal → no ETA.
	got := computeETA(5, -1, 1000, 1024, true)
	if got != "" {
		t.Errorf("live ETA=%q, want empty", got)
	}
	// SegmentsTotal known but rate unknown.
	got = computeETA(5, 10, 1000, 0, false)
	if got != "" {
		t.Errorf("no-rate ETA=%q, want empty", got)
	}
	// done == 0 guard — avgBytesPerSeg would div by zero.
	got = computeETA(0, 10, 0, 1024, true)
	if got != "" {
		t.Errorf("done=0 ETA=%q, want empty", got)
	}
}

func TestComputeETA_FormatRoundTrip(t *testing.T) {
	// done=5, total=10, bytesWritten=2500 → avgBytesPerSeg=500.
	// remainingSegs=5 → remainingBytes=2500. rate=100 B/s →
	// secs = 2500/100 = 25s exactly.
	got := computeETA(5, 10, 2500, 100.0, true)
	if got == "" {
		t.Fatal("ETA empty, want non-empty")
	}
	// With the /done fix (was /(done+1)), 25s is exact.
	if got != "25s" {
		t.Errorf("ETA=%q, want %q", got, "25s")
	}
}

func TestFormatRate(t *testing.T) {
	cases := []struct {
		rate float64
		want string
	}{
		{500, "500 B/s"},
		{1500, "1.46 KiB/s"},
		{5 << 20, "5.00 MiB/s"},
		{2 << 30, "2.00 GiB/s"},
	}
	for _, c := range cases {
		if got := formatRate(c.rate); got != c.want {
			t.Errorf("formatRate(%v) = %q, want %q", c.rate, got, c.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{-time.Second, "0s"},
		{0, "0s"},
		{30 * time.Second, "30s"},
		{90 * time.Second, "1:30"},
		{(time.Hour + 2*time.Minute + 5*time.Second), "1:02:05"},
	}
	for _, c := range cases {
		if got := formatDuration(c.d); got != c.want {
			t.Errorf("formatDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestProgressEmitter_NonBlockingOnFullChannel(t *testing.T) {
	// A full channel must not wedge the emitter — mid-stream
	// events drop and the next cumulative event replaces them.
	ch := make(chan Progress, 1)
	em := newProgressEmitter("job-1", "video", ch)
	// Fill the buffer.
	em.setStage("auth")
	// Next calls must not block even though the buffer is full.
	done := make(chan struct{})
	go func() {
		em.setStage("playlist")
		em.setStage("segments")
		em.setStage("done")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("emitter wedged on full channel")
	}
}
