package downloader

import (
	"context"

	"github.com/befabri/replayvod/server/internal/downloader/hls"
)

// runHLSAttempt finishes progress callbacks before counters reset or the subscriber channel closes.
func runHLSAttempt(ctx context.Context, emitter *progressEmitter, cfg hls.JobConfig) (*hls.JobResult, error) {
	emitter.startAttempt()
	emitter.setStage("segments")
	cfg.OnProgress = func(progress hls.Progress) {
		// HLS retains these seeds for per-part gap policy. The recording
		// emitter already includes them in its baseline, so bridge only the
		// new attempt's contribution. Bytes, ad gaps and total are unseeded.
		progress.SegmentsDone -= cfg.SeedSegmentsDone
		progress.SegmentsGaps -= cfg.SeedSegmentsGaps
		emitter.bridge(progress)
	}
	return hls.Run(ctx, cfg)
}
