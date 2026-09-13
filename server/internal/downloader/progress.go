package downloader

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/hls"
)

// progressEmitter shares cumulative counters between acquisition and stage updates under one mutex.
type progressEmitter struct {
	jobID         string
	out           chan<- Progress
	recordingType string
	onSnapshot    func(Progress)
	mediaOffset   func() (float64, bool)

	mu             sync.Mutex
	stage          string
	partIndex      int
	bytesWritten   int64
	segmentsDone   int64
	segmentsGaps   int64
	segmentsAdGaps int64
	segmentsTot    int64 // -1 until EXT-X-ENDLIST
	quality        string
	fps            *float64
	codec          string

	// HLS progress excludes seeded policy counters; these baselines restore recording-wide totals.
	baselineBytes  int64
	baselineDone   int64
	baselineGaps   int64
	baselineAdGaps int64

	samples []byteSample
}

type byteSample struct {
	at    time.Time
	bytes int64
}

// speedWindow smooths CDN bursts while allowing sustained rate changes to appear promptly.
const speedWindow = 10 * time.Second

// newProgressEmitter sends cumulative snapshots without blocking acquisition.
// The caller closes out only after all progress callbacks finish.
func newProgressEmitter(jobID, recordingType string, out chan<- Progress, onSnapshot ...func(Progress)) *progressEmitter {
	var cb func(Progress)
	if len(onSnapshot) > 0 {
		cb = onSnapshot[0]
	}
	return &progressEmitter{
		jobID:         jobID,
		out:           out,
		recordingType: recordingType,
		onSnapshot:    cb,
		partIndex:     1,
		segmentsTot:   -1,
	}
}

func (p *progressEmitter) setMediaOffsetSource(fn func() (float64, bool)) {
	p.mu.Lock()
	p.mediaOffset = fn
	p.mu.Unlock()
}

// seedCompletedBytes prevents recovery from temporarily resetting progress to zero.
func (p *progressEmitter) seedCompletedBytes(bytes int64) {
	if bytes <= 0 {
		return
	}
	p.mu.Lock()
	if p.bytesWritten < bytes {
		p.bytesWritten = bytes
	}
	p.mu.Unlock()
}

func (p *progressEmitter) setStage(stage string) {
	p.mu.Lock()
	p.stage = stage
	snap := p.snapshotLocked()
	p.mu.Unlock()
	p.send(snap)
}

func (p *progressEmitter) setVariant(quality string, fps *float64, codec string) {
	p.mu.Lock()
	p.quality = quality
	p.fps = fps
	p.codec = codec
	snap := p.snapshotLocked()
	p.mu.Unlock()
	p.send(snap)
}

// setPart clears the previous total so a new part cannot retain a completed percentage.
func (p *progressEmitter) setPart(n int) {
	p.mu.Lock()
	p.partIndex = n
	p.stage = "auth"
	p.quality = ""
	p.fps = nil
	p.codec = ""
	p.segmentsTot = -1
	snap := p.snapshotLocked()
	p.mu.Unlock()
	p.send(snap)
}

// startAttempt retains earlier totals while resetting rate samples for a fresh HLS attempt.
func (p *progressEmitter) startAttempt() {
	p.mu.Lock()
	p.baselineBytes = p.bytesWritten
	p.baselineDone = p.segmentsDone
	p.baselineGaps = p.segmentsGaps
	p.baselineAdGaps = p.segmentsAdGaps
	p.samples = nil
	p.mu.Unlock()
}

func (p *progressEmitter) bridge(hp hls.Progress) {
	p.mu.Lock()
	p.bytesWritten = p.baselineBytes + hp.BytesWritten
	p.segmentsDone = p.baselineDone + hp.SegmentsDone
	p.segmentsGaps = p.baselineGaps + hp.SegmentsGaps
	p.segmentsAdGaps = p.baselineAdGaps + hp.SegmentsAdGaps
	// A finite playlist's total excludes earlier HLS attempts.
	if hp.SegmentsTotal > 0 {
		p.segmentsTot = p.baselineDone + p.baselineGaps + hp.SegmentsTotal
	}
	p.samples = appendSample(p.samples, byteSample{at: time.Now(), bytes: p.bytesWritten})
	snap := p.snapshotLocked()
	p.mu.Unlock()
	p.send(snap)
}

func (p *progressEmitter) finalize() {
	p.mu.Lock()
	p.segmentsTot = p.segmentsDone + p.segmentsGaps
	snap := p.snapshotLocked()
	p.mu.Unlock()
	p.send(snap)
}

func (p *progressEmitter) snapshotLocked() Progress {
	rate, rateOK := currentRate(p.samples)
	snap := Progress{
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
		FPS:            p.fps,
		Codec:          p.codec,
		RecordingType:  p.recordingType,
	}
	if p.mediaOffset != nil {
		if seconds, ok := p.mediaOffset(); ok {
			snap.MediaOffsetSeconds = &seconds
		}
	}
	return snap
}

func (p *progressEmitter) send(snap Progress) {
	if p.onSnapshot != nil {
		p.onSnapshot(snap)
	}
	select {
	case p.out <- snap:
	default:
	}
}

func appendSample(samples []byteSample, s byteSample) []byteSample {
	cutoff := s.at.Add(-speedWindow)
	// Samples must be appended in time order.
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

// currentRate rejects intervals below 100 ms to avoid displaying burst noise.
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

func formatSpeed(rate float64, ok bool) string {
	if !ok {
		return ""
	}
	return formatRate(rate)
}

func computeSpeed(samples []byteSample) string {
	rate, ok := currentRate(samples)
	return formatSpeed(rate, ok)
}

func computeETA(done, total, bytesWritten int64, rate float64, rateOK bool) string {
	if total <= 0 || done >= total || !rateOK || done == 0 {
		return ""
	}
	// Remaining-byte estimates assume segments are roughly equal in size.
	remainingSegs := total - done
	avgBytesPerSeg := float64(bytesWritten) / float64(done)
	remainingBytes := float64(remainingSegs) * avgBytesPerSeg
	secs := remainingBytes / rate
	if math.IsInf(secs, 0) || math.IsNaN(secs) || secs < 0 {
		return ""
	}
	return formatDuration(time.Duration(secs * float64(time.Second)))
}

func computePercent(done, total int64) float64 {
	if total <= 0 {
		return -1
	}
	if done >= total {
		return 100
	}
	return 100 * float64(done) / float64(total)
}

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
