package hls

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"golang.org/x/sync/errgroup"
)

// JobConfig is the input to Run. Everything the orchestrator
// needs to turn a signed media-playlist URL into a directory
// full of committed segment files.
type JobConfig struct {
	// MediaPlaylistURL is the per-variant playlist URL from
	// Stage 3. Must already carry Twitch's ?sig=&token= params
	// — the orchestrator does not stamp signatures.
	MediaPlaylistURL string

	// WorkDir is the job's scratch directory. Already created
	// and writable. Segment files land at WorkDir/<seq>.<ext>;
	// the init segment lands at WorkDir/init.mp4 (fmp4 only).
	WorkDir string

	// Fetcher is shared across the pool's workers. Usually
	// one per process (or per downloader.Service).
	Fetcher *Fetcher

	// PlaylistClient fetches media playlists. Separate from
	// the Fetcher's http.Client so playlist + segment traffic
	// can have different timeouts without cross-contamination.
	// Nil uses http.DefaultClient.
	PlaylistClient *http.Client

	// SegmentConcurrency is the worker count.
	// cfg.Download.SegmentConcurrency at the config level.
	// Default 4.
	SegmentConcurrency int

	// Log is the per-job logger.
	Log *slog.Logger

	// Progress is optional; when non-nil the orchestrator sends
	// a Progress event after every finished segment (success,
	// gap, or fatal). Caller owns the channel and must drain
	// it. Buffer it or accept back-pressure at the fetch rate.
	Progress chan<- Progress
}

// Progress carries cumulative counters the UI / SSE subscriber
// consumes to render the real-time progress bar. The cumulative
// shape makes it safe to drop mid-stream events — any received
// event fully replaces the previous state.
type Progress struct {
	SegmentsDone int64
	SegmentsGaps int64
	BytesWritten int64
	Kind         SegmentKind
	InitURI      string
}

// JobResult summarizes a completed Run. SegmentsDone counts
// commits, SegmentsGaps counts failures the tolerant policy
// accepted (none for now — gap policy comes in 4d alongside
// resume state). The orchestrator returns non-nil error only for
// bootstrap failures (first playlist fetch) or unrecoverable
// wiring errors; per-segment failures are tallied in gaps.
type JobResult struct {
	SegmentsDone int64
	SegmentsGaps int64
	BytesWritten int64
	Kind         SegmentKind
	InitURI      string // empty for ts jobs
}

// Run is the top-level entry point for Phase 4c. Blocks until the
// playlist's ENDLIST is observed, ctx is canceled, or an unrecov-
// erable bootstrap error occurs. The returned JobResult is valid
// even on ctx-cancel — callers that need "what got written before
// shutdown" can inspect it.
//
// Lifecycle:
//
//	1. Poll the playlist once to learn Kind + Init + TargetDuration.
//	2. If fmp4, fetch init.mp4 synchronously before any segment.
//	3. Start the poller and the pool under an errgroup.
//	4. Drain results, emitting Progress events, until the pool
//	   closes its result chan. Return the final tally.
//
// Auth refresh is NOT handled here — Phase 4d wraps Run with an
// outer retry that re-runs Stages 1-3 on ErrPlaylistAuth / on
// FetchKindAuth escalation.
func Run(ctx context.Context, cfg JobConfig) (*JobResult, error) {
	if err := validateJobConfig(&cfg); err != nil {
		return nil, err
	}

	log := cfg.Log.With("domain", "hls.job")

	poller := &Poller{
		URL:        cfg.MediaPlaylistURL,
		HTTPClient: cfg.PlaylistClient,
		Log:        log,
	}
	pool := &Pool{
		Fetcher: cfg.Fetcher,
		WorkDir: cfg.WorkDir,
		Workers: cfg.SegmentConcurrency,
		Log:     log,
	}

	// Bounded queue: 2 × worker count per spec.
	// Producer blocks when full → natural backpressure so the
	// poller doesn't outrun the fetchers during a CDN burst.
	jobChanCap := 2 * max(1, cfg.SegmentConcurrency)
	jobs := make(chan segmentJob, jobChanCap)
	results := make(chan SegmentResult, jobChanCap)
	first := make(chan PollResult, 1)

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		err := poller.Run(gctx, first, jobs)
		// ENDLIST + ctx.Canceled both arrive as nil/ctx.Err;
		// the errgroup won't cancel siblings on nil. We do
		// NOT treat ENDLIST as an error.
		return err
	})

	g.Go(func() error {
		return pool.Run(gctx, jobs, results)
	})

	// Bootstrap: wait for the first PollResult before looking
	// at segment results. Must fetch the init segment (fmp4)
	// synchronously so workers that pick up the first segment
	// already see init.mp4 on disk if they care. For TS jobs
	// this is a one-value channel read.
	//
	// If the poller errors before producing a PollResult
	// (ErrPlaylistAuth on the very first fetch, for instance),
	// gctx gets canceled by errgroup. Drain g.Wait() and
	// surface the original error rather than the downstream
	// "context canceled" — the auth-refresh caller needs the
	// typed error to know what to do.
	result := &JobResult{}
	var pr PollResult
	select {
	case pr = <-first:
	case <-gctx.Done():
		return result, g.Wait()
	}
	result.Kind = pr.Kind
	if pr.Init != nil {
		result.InitURI = pr.Init.URI
		if err := fetchInit(gctx, cfg.Fetcher, cfg.WorkDir, pr.Init.URI); err != nil {
			// Init fetch is the one hard failure: without it,
			// fmp4 fragments can't be muxed. Cancel the pool +
			// poller and bail.
			log.Error("init segment fetch failed; aborting job", "error", err)
			return result, fmt.Errorf("hls init segment: %w", err)
		}
	}

	// Drain results. The pool closes `results` after all
	// workers finish, so the for-range exits cleanly on
	// normal termination.
	for res := range results {
		if res.Err != nil {
			// Phase 4c policy: every failure is a gap. 4d
			// will split this into gap-vs-fatal per the
			// tolerant/strict config and the first-content-
			// segment guard.
			result.SegmentsGaps++
			log.Debug("segment gap", "seq", res.MediaSeq, "error", res.Err)
		} else {
			result.SegmentsDone++
			result.BytesWritten += res.BytesWritten
		}
		if cfg.Progress != nil {
			// Non-blocking send — progress is informational.
			// A slow subscriber shouldn't throttle the fetch.
			select {
			case cfg.Progress <- Progress{
				SegmentsDone: result.SegmentsDone,
				SegmentsGaps: result.SegmentsGaps,
				BytesWritten: result.BytesWritten,
				Kind:         result.Kind,
				InitURI:      result.InitURI,
			}:
			default:
			}
		}
	}

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return result, err
	}
	return result, nil
}

// fetchInit synchronously downloads the fmp4 initialization
// segment to WorkDir/init.mp4. Any failure aborts the job — the
// segments after it can't be played without their init.
//
// Runs through the same Fetcher as media segments so retry
// budgets + backoff apply. The orchestrator doesn't care about
// the byte count (small file, ~4-8 KB).
func fetchInit(ctx context.Context, f *Fetcher, workDir, url string) error {
	w, err := NewPartWriter(workDir, "init.mp4")
	if err != nil {
		return err
	}
	defer w.Abort()
	if _, err := f.Fetch(ctx, url, w); err != nil {
		return err
	}
	return w.Commit()
}

// validateJobConfig sanity-checks the input and fills in zero-
// value defaults that are safe at runtime. Keeps Run from
// growing a mile-long if-chain at its start.
func validateJobConfig(cfg *JobConfig) error {
	if cfg.MediaPlaylistURL == "" {
		return errors.New("hls job: empty MediaPlaylistURL")
	}
	if cfg.WorkDir == "" {
		return errors.New("hls job: empty WorkDir")
	}
	if cfg.Fetcher == nil {
		return errors.New("hls job: nil Fetcher")
	}
	if cfg.PlaylistClient == nil {
		cfg.PlaylistClient = http.DefaultClient
	}
	if cfg.SegmentConcurrency <= 0 {
		cfg.SegmentConcurrency = 4
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	return nil
}
