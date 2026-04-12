package downloader

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/hls"
)

// progressEmitter owns per-job cumulative state for the SSE
// progress stream. One instance per run; the orchestrator
// updates it as stages transition and as hls.Progress events
// arrive, and every emission flushes the full cumulative shape
// to the per-job channel.
//
// All fields live inside the emitter rather than scattered
// across run() locals so the bridgeProgress goroutine and the
// synchronous stage transitions can't see divergent views.
type progressEmitter struct {
	jobID         string
	out           chan<- Progress
	recordingType string

	mu             sync.Mutex
	stage          string
	partIndex      int
	bytesWritten   int64
	segmentsDone   int64
	segmentsGaps   int64
	segmentsAdGaps int64
	segmentsTot    int64 // -1 until EXT-X-ENDLIST
	quality        string
	codec          string

	// Baselines captured at the start of each hls.Run invocation.
	// hls.Progress counters reset to zero on every new hls.Run
	// (fresh JobResult), so the auth-refresh loop would regress
	// the UI every iteration without this layering. bridge()
	// computes cumulative = baseline + hp.<field>.
	baselineBytes  int64
	baselineDone   int64
	baselineGaps   int64
	baselineAdGaps int64

	// Speed smoothing: a short window of (time, bytes) samples
	// keeps a single burst from dominating the displayed
	// rate. Each bridged event appends one sample; samples
	// older than speedWindow are dropped.
	samples []byteSample
}

// byteSample pairs an instantaneous byte count with the time it
// was observed. Stored per-bridge-event.
type byteSample struct {
	at    time.Time
	bytes int64
}

// speedWindow bounds the rolling-average window used to compute
// Speed. 10 seconds is long enough that a CDN burst doesn't
// dominate, short enough that a genuine rate drop shows up
// promptly in the UI.
const speedWindow = 10 * time.Second

// newProgressEmitter constructs an emitter. out must be a
// buffered channel owned by the caller; the emitter sends non-
// blocking so a slow subscriber can't throttle the pipeline,
// and the caller is responsible for closing out when the job
// completes.
func newProgressEmitter(jobID, recordingType string, out chan<- Progress) *progressEmitter {
	return &progressEmitter{
		jobID:         jobID,
		out:           out,
		recordingType: recordingType,
		partIndex:     1,
		segmentsTot:   -1,
	}
}

// setStage updates the stage label and fires one event. Called
// on every pipeline stage transition (auth → playlist → segments
// → remux → metadata → thumbnail → done).
func (p *progressEmitter) setStage(stage string) {
	p.mu.Lock()
	p.stage = stage
	snap := p.snapshotLocked()
	p.mu.Unlock()
	p.send(snap)
}

// setVariant records the Stage 3 selection. Called once the
// master playlist has been fetched and the variant picked. The
// next event picks up Quality + Codec.
func (p *progressEmitter) setVariant(quality, codec string) {
	p.mu.Lock()
	p.quality = quality
	p.codec = codec
	snap := p.snapshotLocked()
	p.mu.Unlock()
	p.send(snap)
}

// startAttempt captures the current cumulative counters as the
// baseline for the next hls.Run invocation. Call this
// immediately before every hls.Run in the auth-refresh loop so
// bridge() can layer per-attempt hls deltas on top and the
// cumulative UI view doesn't regress when hls's internal
// counters reset.
//
// Also clears the speed-window samples: the new attempt's
// hp.BytesWritten starts at 0, so keeping old samples would
// produce a negative delta on the first bridged event and
// mis-report the rate as empty.
func (p *progressEmitter) startAttempt() {
	p.mu.Lock()
	p.baselineBytes = p.bytesWritten
	p.baselineDone = p.segmentsDone
	p.baselineGaps = p.segmentsGaps
	p.baselineAdGaps = p.segmentsAdGaps
	p.samples = nil
	p.mu.Unlock()
}

// bridge consumes an hls.Progress event, updates the cumulative
// counters (baseline + hls-attempt delta), refreshes the speed-
// smoothing window, and fires one event. Safe to call from the
// bridge goroutine while other goroutines call setStage /
// setVariant; the mutex serializes all writes.
func (p *progressEmitter) bridge(hp hls.Progress) {
	p.mu.Lock()
	p.bytesWritten = p.baselineBytes + hp.BytesWritten
	p.segmentsDone = p.baselineDone + hp.SegmentsDone
	p.segmentsGaps = p.baselineGaps + hp.SegmentsGaps
	p.segmentsAdGaps = p.baselineAdGaps + hp.SegmentsAdGaps
	// Keep segmentsTot as the orchestrator last set it — hls
	// doesn't currently report total. A later phase can set it
	// on the final event.
	p.samples = appendSample(p.samples, byteSample{at: time.Now(), bytes: p.bytesWritten})
	snap := p.snapshotLocked()
	p.mu.Unlock()
	p.send(snap)
}

// finalize marks the stream as closed — segments total is now
// SegmentsDone + SegmentsGaps. Fires one event so the terminal
// Percent is exact (100% when no gaps, less with tolerated
// gaps). Called from run() once hls.Run returns.
func (p *progressEmitter) finalize() {
	p.mu.Lock()
	p.segmentsTot = p.segmentsDone + p.segmentsGaps
	snap := p.snapshotLocked()
	p.mu.Unlock()
	p.send(snap)
}

// snapshotLocked builds a Progress value under the lock. Percent,
// Speed, and ETA derive from the raw counters so the caller
// doesn't have to recompute them.
func (p *progressEmitter) snapshotLocked() Progress {
	rate, rateOK := currentRate(p.samples)
	return Progress{
		JobID:          p.jobID,
		PartIndex:      p.partIndex,
		Stage:          p.stage,
		BytesWritten:   p.bytesWritten,
		SegmentsDone:   p.segmentsDone,
		SegmentsGaps:   p.segmentsGaps,
		SegmentsAdGaps: p.segmentsAdGaps,
		SegmentsTotal:  p.segmentsTot,
		Percent:        computePercent(p.segmentsDone, p.segmentsTot),
		Speed:          formatSpeed(rate, rateOK),
		ETA:            computeETA(p.segmentsDone, p.segmentsTot, p.bytesWritten, rate, rateOK),
		Quality:        p.quality,
		Codec:          p.codec,
		RecordingType:  p.recordingType,
	}
}

// send performs a non-blocking write to the per-job channel.
// Progress is informational — dropping a mid-stream event is
// fine because the next cumulative event supersedes it, and the
// channel close (done by the caller when run exits) is the
// authoritative terminal signal.
func (p *progressEmitter) send(snap Progress) {
	select {
	case p.out <- snap:
	default:
	}
}

// appendSample adds one sample to the rolling window + drops
// everything older than speedWindow. O(n) in window length; n
// is bounded by one sample per hls.Progress event (roughly one
// per segment commit), which is <1000 for any realistic stream.
func appendSample(samples []byteSample, s byteSample) []byteSample {
	cutoff := s.at.Add(-speedWindow)
	// Drop expired samples from the head. Samples are always
	// appended in time order so a linear drop is correct.
	trim := 0
	for ; trim < len(samples); trim++ {
		if !samples[trim].at.Before(cutoff) {
			break
		}
	}
	if trim > 0 {
		samples = samples[trim:]
	}
	return append(samples, s)
}

// currentRate returns the rolling-window rate in bytes/sec and
// whether the window is meaningful. Consolidates the guards that
// computeSpeed and computeETA would otherwise duplicate so the
// two derived values stay consistent: if Speed is empty, ETA is
// empty; if Speed says 5 MiB/s, ETA uses the same 5 MiB/s.
//
// Returns ok=false on <2 samples (no delta), <100ms window (too
// noisy), or non-positive delta (clock skew / sample reset).
func currentRate(samples []byteSample) (float64, bool) {
	if len(samples) < 2 {
		return 0, false
	}
	first, last := samples[0], samples[len(samples)-1]
	dt := last.at.Sub(first.at)
	if dt < 100*time.Millisecond {
		return 0, false
	}
	db := last.bytes - first.bytes
	if db <= 0 {
		return 0, false
	}
	return float64(db) / dt.Seconds(), true
}

// formatSpeed renders the numeric rate as a human string. Empty
// when rate isn't meaningful.
func formatSpeed(rate float64, ok bool) string {
	if !ok {
		return ""
	}
	return formatRate(rate)
}

// computeSpeed is a test-facing convenience that runs currentRate
// + formatSpeed in one call. Production code uses the two steps
// directly via snapshotLocked so the same rate drives both Speed
// and ETA without re-derivation.
func computeSpeed(samples []byteSample) string {
	rate, ok := currentRate(samples)
	return formatSpeed(rate, ok)
}

// computeETA returns a human-readable duration to completion
// when SegmentsTotal is known, at least one segment has committed,
// and the current rate is positive. Empty otherwise —
// indeterminate live streams, brand-new jobs, and unknown rates
// all show blank.
func computeETA(done, total, bytesWritten int64, rate float64, rateOK bool) string {
	if total <= 0 || done >= total || !rateOK || done == 0 {
		return ""
	}
	// Use segment ratio to estimate remaining bytes — assumes
	// segments are roughly equal size, which holds for fMP4 and
	// TS in practice. An early / late bias matters most for
	// short streams where the ETA isn't load-bearing anyway.
	remainingSegs := total - done
	avgBytesPerSeg := float64(bytesWritten) / float64(done)
	remainingBytes := float64(remainingSegs) * avgBytesPerSeg
	secs := remainingBytes / rate
	if math.IsInf(secs, 0) || math.IsNaN(secs) || secs < 0 {
		return ""
	}
	return formatDuration(time.Duration(secs * float64(time.Second)))
}

// computePercent returns SegmentsDone / SegmentsTotal as a
// percentage, or -1 when Total is unknown / zero (live stream).
func computePercent(done, total int64) float64 {
	if total <= 0 {
		return -1
	}
	if done >= total {
		return 100
	}
	return 100 * float64(done) / float64(total)
}

// formatRate prints bytes/sec in binary units with two decimal
// places — the scale the UI renders.
func formatRate(bytesPerSec float64) string {
	const (
		KiB = 1024.0
		MiB = 1024.0 * KiB
		GiB = 1024.0 * MiB
	)
	switch {
	case bytesPerSec >= GiB:
		return fmt.Sprintf("%.2f GiB/s", bytesPerSec/GiB)
	case bytesPerSec >= MiB:
		return fmt.Sprintf("%.2f MiB/s", bytesPerSec/MiB)
	case bytesPerSec >= KiB:
		return fmt.Sprintf("%.2f KiB/s", bytesPerSec/KiB)
	default:
		return fmt.Sprintf("%.0f B/s", bytesPerSec)
	}
}

// formatDuration prints a duration as HH:MM:SS / MM:SS / SS per
// magnitude. Keeps output stable so the UI can left-pad if it
// wants fixed-width.
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	s := int((d % time.Minute) / time.Second)
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%d:%02d", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
