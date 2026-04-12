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
	// gap, or fatal) and closes the channel before Run returns.
	// Closing is the terminal signal — SSE subscribers observe
	// "channel closed" as "job done" and transition out of
	// in-progress state. Mid-stream events use a non-blocking
	// send (drop-is-fine because the next cumulative event
	// supersedes); the close is unconditional so the terminal
	// state is never lost.
	//
	// Caller must NOT write to this channel and must NOT close
	// it; orchestrator owns the close.
	Progress chan<- Progress

	// GapPolicy controls what the orchestrator does when a
	// segment fails. Zero value is tolerant with 1% ratio and
	// the first-content-segment guard on — the spec's default
	// for live recording. Override for VOD or operator-opted
	// strict mode.
	GapPolicy GapPolicy

	// StartMediaSeq optionally resumes from a prior attempt's
	// cursor. Passed directly to the Poller; segments below
	// this threshold are not re-emitted. Zero = fresh start.
	// Auth-refresh and resume-on-restart callers set this to
	// JobResult.LastMediaSeq + 1 from the previous attempt.
	StartMediaSeq int64
}

// GapPolicy decides "accept segment failure as a gap" vs "abort
// the job." The spec's model: tolerant mode for live (a flaky
// edge shouldn't drop a 4-hour recording); strict mode for
// operators who'd rather fail fast than ship a partial VOD.
//
// The first-content-segment guard is non-negotiable in tolerant
// mode — it prevents the "job succeeds having captured only
// preroll-ad segments" silent failure.
type GapPolicy struct {
	// Strict aborts the job on the first segment failure.
	// Overrides MaxGapRatio when true.
	Strict bool

	// MaxGapRatio is the tolerant-mode ceiling: gaps / (gaps +
	// done) above this fraction fails the job. Default 0.01
	// (1%). Zero is treated as "unset" and takes the default —
	// for no-tolerance semantics, set Strict=true instead.
	// (A *float64 sentinel would be the faithful "zero means
	// zero" shape; the simpler "Strict for no-tolerance" path
	// is enough in practice and avoids a pointer-valued config
	// field.)
	MaxGapRatio float64

	// SkipFirstContentGuard disables the "at least one real
	// content segment must succeed before any gap is accepted"
	// rule. Operator-opt-out only; the default posture is
	// "never ship a VOD that's all ads."
	SkipFirstContentGuard bool
}

// normalize fills in zero-value defaults. Mutates in place.
func (p *GapPolicy) normalize() {
	if p.MaxGapRatio <= 0 {
		p.MaxGapRatio = 0.01
	}
}

// Progress carries cumulative counters the UI / SSE subscriber
// consumes to render the real-time progress bar. The cumulative
// shape makes it safe to drop mid-stream events — any received
// event fully replaces the previous state.
type Progress struct {
	SegmentsDone int64
	SegmentsGaps int64
	// SegmentsAdGaps counts stitched-ad segments the poller
	// skipped. Reported separately from SegmentsGaps so the UI
	// can show "Twitch ad content omitted" distinctly from
	// "fetch failures tolerated," and so gap-policy math
	// (MaxGapRatio) doesn't count ads against the ceiling.
	SegmentsAdGaps int64
	BytesWritten   int64
	Kind           SegmentKind
	InitURI        string
}

// JobResult summarizes a completed Run. SegmentsDone counts
// commits, SegmentsGaps counts failures the tolerant policy
// accepted. The orchestrator returns non-nil error for
// bootstrap failures, gap-policy aborts (strict mode, ratio
// breach, first-content guard tripped), or auth refresh
// escalation (ErrPlaylistAuth wrapped); otherwise per-segment
// failures are tallied in gaps.
//
// LastMediaSeq is the highest MediaSeq the result drain
// observed (success OR accepted gap). Auth-refresh callers set
// the next attempt's JobConfig.StartMediaSeq to LastMediaSeq+1
// so already-processed segments aren't re-fetched.
type JobResult struct {
	SegmentsDone int64
	SegmentsGaps int64
	// SegmentsAdGaps counts stitched-ad segments skipped by the
	// poller. Excluded from MaxGapRatio — Twitch-injected
	// content isn't a CDN or transport failure.
	SegmentsAdGaps int64
	BytesWritten   int64
	Kind           SegmentKind
	InitURI        string // empty for ts jobs
	LastMediaSeq   int64
}

// GapAbortError is the typed error returned when the gap policy
// aborts the job. Carries the triggering reason so the caller's
// operator logs / UI can distinguish "first content never
// succeeded" from "1.5% gap ratio exceeded 1% ceiling."
type GapAbortError struct {
	Reason   string
	Done     int64
	Gaps     int64
	LastSeq  int64
	LastErr  error
}

func (e *GapAbortError) Error() string {
	return fmt.Sprintf("hls job: gap policy abort (%s): done=%d gaps=%d last_seq=%d: %v",
		e.Reason, e.Done, e.Gaps, e.LastSeq, e.LastErr)
}

func (e *GapAbortError) Unwrap() error { return e.LastErr }

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
	cfg.GapPolicy.normalize()

	log := cfg.Log.With("domain", "hls.job")

	// Close Progress exactly once on the way out, regardless
	// of whether Run succeeds, errors, or the ctx cancels.
	// Subscribers treat "chan closed" as "terminal state
	// reached" — the close signals that no more updates will
	// arrive and the cumulative counters are final.
	if cfg.Progress != nil {
		defer close(cfg.Progress)
	}

	poller := &Poller{
		URL:           cfg.MediaPlaylistURL,
		HTTPClient:    cfg.PlaylistClient,
		Log:           log,
		StartMediaSeq: cfg.StartMediaSeq,
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

	// Explicit cancel so a synchronous bootstrap failure
	// (init-segment fetch error) can stop the poller + pool
	// and drain them before Run returns. The errgroup's own
	// context-cancel fires only when a g.Go function returns
	// non-nil; fetchInit lives outside g.Go and so needs a
	// direct cancel handle.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	g, gctx := errgroup.WithContext(runCtx)

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
			// fmp4 fragments can't be muxed. Cancel poller +
			// pool and drain — otherwise they keep polling the
			// playlist + committing segments to WorkDir after
			// the caller has already been told the job failed.
			log.Error("init segment fetch failed; aborting job", "error", err)
			cancel()
			_ = g.Wait()
			return result, fmt.Errorf("hls init segment: %w", err)
		}
	}

	// Drain results. The pool closes `results` after all
	// workers finish, so the for-range exits cleanly on
	// normal termination. A gap-policy abort or a segment-
	// level auth error cancel()s the siblings and breaks out;
	// the remaining results are drained silently to let the
	// pool close cleanly.
	//
	// authErr is its own var so the outer auth-refresh caller
	// (downloader.run in Phase 6b) can detect it without
	// string-matching: errors.Is(err, ErrPlaylistAuth) matches.
	var abortErr *GapAbortError
	var authErr error
	for res := range results {
		// LastMediaSeq advances on EVERY outcome — success, gap,
		// or auth error. The auth-refresh caller uses it as the
		// next attempt's StartMediaSeq, so advancing past an
		// auth-errored segment means that segment is not retried
		// on refresh: it becomes a gap. Deliberate trade-off —
		// on a fast CDN-window-roll refresh cycle the segment has
		// likely already rolled off the edge by the time a new
		// signed URL exists, so retrying would 404 anyway. The
		// spec's first-content guard + per-attempt gap policy
		// still protect against "every segment auth-failed" as
		// silent corruption.
		if res.MediaSeq > result.LastMediaSeq {
			result.LastMediaSeq = res.MediaSeq
		}
		if abortErr != nil || authErr != nil {
			// Already aborting — keep draining so the pool's
			// workers can exit via ctx.Done and close the
			// channel. Don't update counters.
			continue
		}
		if res.Err != nil {
			// Auth errors escape gap policy: they're fixable
			// by re-running Stages 1-3 for a fresh signed URL.
			// Surface via ErrPlaylistAuth so the outer refresh
			// loop detects it.
			if IsAuth(res.Err) {
				authErr = fmt.Errorf("hls: segment seq=%d auth error: %w", res.MediaSeq, ErrPlaylistAuth)
				log.Info("segment auth error; requesting refresh",
					"seq", res.MediaSeq)
				cancel()
				continue
			}
			abortErr = evaluateGap(&cfg.GapPolicy, result, res)
			if abortErr == nil {
				result.SegmentsGaps++
				log.Debug("segment gap accepted", "seq", res.MediaSeq, "error", res.Err)
			} else {
				log.Warn("segment gap aborts job",
					"reason", abortErr.Reason,
					"seq", res.MediaSeq,
					"done", result.SegmentsDone,
					"gaps", result.SegmentsGaps)
				cancel()
				continue
			}
		} else {
			result.SegmentsDone++
			result.BytesWritten += res.BytesWritten
		}
		if cfg.Progress != nil {
			// Non-blocking send — progress is informational.
			// A slow subscriber shouldn't throttle the fetch.
			select {
			case cfg.Progress <- Progress{
				SegmentsDone:   result.SegmentsDone,
				SegmentsGaps:   result.SegmentsGaps,
				SegmentsAdGaps: poller.AdSkipped(),
				BytesWritten:   result.BytesWritten,
				Kind:           result.Kind,
				InitURI:        result.InitURI,
			}:
			default:
			}
		}
	}

	// Snapshot ad-skip count now that the result drain has
	// finished; the poller won't enqueue any more segments.
	result.SegmentsAdGaps = poller.AdSkipped()

	if authErr != nil {
		_ = g.Wait()
		return result, authErr
	}
	if abortErr != nil {
		_ = g.Wait()
		return result, abortErr
	}

	// Filter both ctx-err kinds — the JobResult is always
	// valid on shutdown (partial tally), so the caller only
	// wants the error when something actually broke. Returning
	// ctx-err on normal shutdown would make every caller
	// special-case both Canceled and DeadlineExceeded.
	if err := g.Wait(); err != nil &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded) {
		return result, err
	}
	return result, nil
}

// evaluateGap applies the gap policy to a failed segment result.
// Returns non-nil when the job should abort; nil when the caller
// should treat the failure as an accepted gap and keep going.
//
// Order of checks:
//  1. Strict mode: any failure aborts.
//  2. First-content guard: failure before any success aborts.
//  3. Ratio check: if accepting this gap would push gaps / total
//     above MaxGapRatio, abort.
//
// "Total" here is (gaps_after + done) — the denominator grows as
// the job progresses so a single early failure in a long stream
// doesn't immediately trip the ratio.
func evaluateGap(p *GapPolicy, r *JobResult, res SegmentResult) *GapAbortError {
	if p.Strict {
		return &GapAbortError{
			Reason:  "strict mode",
			Done:    r.SegmentsDone,
			Gaps:    r.SegmentsGaps,
			LastSeq: res.MediaSeq,
			LastErr: res.Err,
		}
	}
	if !p.SkipFirstContentGuard && r.SegmentsDone == 0 {
		return &GapAbortError{
			Reason:  "no content segment committed yet",
			Done:    r.SegmentsDone,
			Gaps:    r.SegmentsGaps,
			LastSeq: res.MediaSeq,
			LastErr: res.Err,
		}
	}
	gapsAfter := r.SegmentsGaps + 1
	total := gapsAfter + r.SegmentsDone
	if float64(gapsAfter)/float64(total) > p.MaxGapRatio {
		return &GapAbortError{
			Reason:  fmt.Sprintf("gap ratio %.2f%% over ceiling %.2f%%", 100*float64(gapsAfter)/float64(total), 100*p.MaxGapRatio),
			Done:    r.SegmentsDone,
			Gaps:    r.SegmentsGaps,
			LastSeq: res.MediaSeq,
			LastErr: res.Err,
		}
	}
	return nil
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
