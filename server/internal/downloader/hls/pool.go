package hls

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/befabri/replayvod/server/internal/background"
)

// SegmentResult reports one fetch outcome; policy decisions belong to Run's caller.
type SegmentResult struct {
	MediaSeq     int64
	FinalName    string
	BytesWritten int64
	// DurationSeconds is EXTINF duration and is valid only when Err is nil.
	DurationSeconds float64
	Err             error
}

// Pool runs a bounded set of segment fetchers for one acquisition.
type Pool struct {
	// Files may be nil for direct filesystem access; managed captures supply their workspace.
	Files FileOperations

	Fetcher *Fetcher

	// WorkDir must already exist and be writable.
	WorkDir string

	// Workers defaults to four when nonpositive.
	Workers int

	Log *slog.Logger

	// Limiter paces segment bytes for this job; nil is unlimited.
	Limiter RateLimiter
}

// Run closes out after every worker exits.
// Segment failures are reported in out; worker panics are returned and cancel sibling workers.
func (p *Pool) Run(ctx context.Context, in <-chan segmentJob, out chan<- SegmentResult) error {
	workers := p.Workers
	if workers <= 0 {
		workers = 4
	}
	log := p.Log

	// Cancellation after every worker drained must not turn successful capture into failure.
	var ctxObserved atomic.Bool

	children := background.NewScope(ctx)
	for i := range workers {
		_ = children.Go(fmt.Sprintf("segment worker %d", i), true, func(childCtx context.Context) error {
			workerLog := log.With("worker", i)
			for {
				select {
				case job, ok := <-in:
					if !ok {
						return nil
					}
					result := p.runOne(childCtx, workerLog, job)
					select {
					case out <- result:
					case <-childCtx.Done():
						ctxObserved.Store(true)
						return nil
					}
				case <-childCtx.Done():
					ctxObserved.Store(true)
					return nil
				}
			}
		})
	}
	err := children.Wait()
	close(out)
	if err != nil {
		return err
	}
	if ctxObserved.Load() {
		return ctx.Err()
	}
	return nil
}

// runOne releases its temporary writer even when a fetch panics.
func (p *Pool) runOne(ctx context.Context, log *slog.Logger, job segmentJob) SegmentResult {
	result := SegmentResult{
		MediaSeq:  job.Segment.MediaSeq,
		FinalName: job.FinalName,
	}

	writer, err := NewPartWriter(p.WorkDir, job.FinalName)
	if err != nil {
		result.Err = fmt.Errorf("hls pool writer: %w", err)
		return result
	}
	writer.ctx, writer.files = ctx, p.Files
	defer writer.Abort()

	n, err := p.Fetcher.FetchLimited(ctx, job.Segment.URI, writer, job.TargetDuration, p.Limiter)
	if err != nil {
		result.Err = err
		log.Debug("segment fetch failed", "seq", job.Segment.MediaSeq, "error", err)
		return result
	}
	if err := writer.Commit(); err != nil {
		result.Err = fmt.Errorf("hls pool commit: %w", err)
		return result
	}
	result.BytesWritten = n
	result.DurationSeconds = job.Segment.Duration
	return result
}
