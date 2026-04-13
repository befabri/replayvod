package hls

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
)

// SegmentResult is what the pool reports per finished segment.
// The orchestrator consumes this to track progress, classify
// failures, and decide gap vs. job-failure per the tolerant/
// strict policy. Stitched-ad-gap entries are recorded by the
// poller upstream (or the orchestrator based on Discontinuity);
// the pool itself only reports fetch outcomes.
type SegmentResult struct {
	MediaSeq     int64
	FinalName    string
	BytesWritten int64
	Err          error // nil on success
}

// Pool fans N workers over a bounded channel of segmentJobs. One
// Pool per job. Workers share the same Fetcher + http.Transport
// — this is why a dedicated Pool exists rather than a free goroutine
// per segment: the transport's MaxConnsPerHost is what actually
// caps concurrent connections to the Twitch edge, and it only
// helps when a finite set of long-lived workers drive the traffic.
type Pool struct {
	// Fetcher drives the per-segment retry loop + .part/rename
	// primitives.
	Fetcher *Fetcher

	// WorkDir is the job's scratch directory. Each committed
	// segment lands at WorkDir/<mediaSeq>.<ext>.
	WorkDir string

	// Workers is the goroutine count. Matches
	// cfg.Download.SegmentConcurrency at the orchestrator level.
	// Default 4 if zero.
	Workers int

	// Log is the per-job logger. Pool-level fields land under
	// domain=hls.pool.
	Log *slog.Logger
}

// Run consumes jobs from in until the channel is closed, fans out
// to N workers, and emits a SegmentResult per job onto out. Blocks
// until all workers have drained and exited, then closes out.
//
// Errors per segment are reported via SegmentResult.Err rather
// than bubbling from Run — the orchestrator's gap policy needs to
// see every outcome, not fail-fast on the first permanent error.
// Run itself only returns an error when a worker actually exited
// via ctx.Done(); if every worker exited because `in` closed
// cleanly and ctx happens to cancel after wg.Wait() returns, we
// still report success.
func (p *Pool) Run(ctx context.Context, in <-chan segmentJob, out chan<- SegmentResult) error {
	workers := p.Workers
	if workers <= 0 {
		workers = 4
	}
	log := p.Log

	// ctxObserved is set by any worker that exited because ctx
	// was canceled rather than because `in` closed. Only then
	// do we surface ctx.Err() — otherwise a post-wait ctx-
	// cancel race would make a cleanly-completed job report as
	// failed.
	var ctxObserved atomic.Bool

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := range workers {
		go func(id int) {
			defer wg.Done()
			workerLog := log.With("worker", id)
			for {
				select {
				case job, ok := <-in:
					if !ok {
						return
					}
					result := p.runOne(ctx, workerLog, job)
					select {
					case out <- result:
					case <-ctx.Done():
						ctxObserved.Store(true)
						return
					}
				case <-ctx.Done():
					ctxObserved.Store(true)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(out)
	if ctxObserved.Load() {
		return ctx.Err()
	}
	return nil
}

// runOne fetches a single segment to disk. The PartWriter
// lifecycle (NewPartWriter → Commit|Abort) is scoped to this
// function so a worker crashing one segment doesn't leak an open
// file handle or orphan a .part.
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
	defer writer.Abort()

	n, err := p.Fetcher.Fetch(ctx, job.Segment.URI, writer, job.TargetDuration)
	if err != nil {
		// Fetcher drained the body and logged internally; we
		// surface the error for the orchestrator's gap policy.
		result.Err = err
		log.Debug("segment fetch failed", "seq", job.Segment.MediaSeq, "error", err)
		return result
	}
	if err := writer.Commit(); err != nil {
		result.Err = fmt.Errorf("hls pool commit: %w", err)
		return result
	}
	result.BytesWritten = n
	return result
}
