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

	// OnEvent, when non-nil, is invoked synchronously from Run's
	// drain goroutine for every sequence-level outcome:
	// committed segments, accepted gaps, stitched-ad skips, and
	// auth failures. Ordered by drain-processing order (not
	// mediaSeq — concurrent workers can complete out of order).
	//
	// Intended for durable accounting (resume-on-restart,
	// audit logs). Distinct from Progress which is cumulative +
	// lossy; OnEvent is per-event + exact.
	//
	// The callback must be fast. It blocks the drain loop;
	// long-running work belongs behind a channel the callback
	// writes to. Callback is invoked from a single goroutine
	// so it does not need to be thread-safe internally.
	OnEvent func(SegmentEvent)

	// OnFirstPoll, when non-nil, is invoked once with the
	// MediaSequenceBase from the first successful playlist fetch
	// — before any segment outcome flows through OnEvent.
	// Resume-state callers use it to seed the accounted-frontier
	// anchor so out-of-order worker completions at the manifest
	// head don't get silently dropped as "below frontier." Runs
	// on the Run goroutine, so callbacks must be fast and not
	// block; write to a channel if you need async work.
	OnFirstPoll func(mediaSequenceBase int64)

	// OnWindowRoll, when non-nil, is invoked once when the first
	// poll after a resume (StartMediaSeq > 0) observes that the
	// playlist head is already past the caller's requested
	// resume point. The lost range [from, to] is inclusive.
	//
	// Resume-state callers record this as a restart_window_rolled
	// gap so the accounted frontier advances past the loss —
	// without that the frontier stays stuck waiting for segments
	// that will never be fetched. Called before OnFirstPoll +
	// OnEvent so the gap lands before any subsequent commit.
	OnWindowRoll func(from, to int64)

	// ClassifyAuth, when non-nil, is forwarded to both the poller
	// and the segment fetcher. It inspects 401/403 response bodies
	// and reports whether the failure is permanent (entitlement
	// restriction, geoblock, etc.). Permanent failures short-
	// circuit the auth-refresh loop — ErrPlaylistAuthPermanent or
	// FetchKindAuthPermanent — so callers don't spin on a stream
	// they'll never be allowed to watch. Leaving it nil preserves
	// the pre-hook behavior: every 401/403 is treated as a
	// refreshable token expiry.
	ClassifyAuth func(status int, body []byte) (permanent bool)

	// SeedSegmentsDone + SeedSegmentsGaps prime the Run counters
	// with cumulative state from prior auth-refresh attempts for
	// the same part. Gap policy (MaxGapRatio, first-content-guard)
	// must evaluate per part, not per attempt — a token refresh
	// mid-recording must not erase the fact that real content has
	// already been captured, nor reset the ratio denominator.
	//
	// Leave at 0 for fresh jobs or the first attempt. The auth-
	// refresh loop in downloader.fetchWithAuthRefresh passes the
	// rolling aggregate so each attempt starts where the last left
	// off.
	SeedSegmentsDone int64
	SeedSegmentsGaps int64

	// RefetchSeqs carries the previous attempt's AuthErrorSeqs:
	// MediaSeqs that 401'd and need to be re-pulled with the new
	// signed URL. Forwarded verbatim to the Poller, which emits
	// them on the first poll regardless of StartMediaSeq. Seqs
	// that have rolled off the CDN window get dropped with a log
	// warning; the upstream resume state keeps them as gaps.
	//
	// Nil / empty on first attempts and on auth-refresh-free
	// runs. Bounded by AuthRefreshAttempts upstream — a seq that
	// keeps auth-erroring eventually fails the job.
	RefetchSeqs []int64
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

	// AuthErrorSeqs lists MediaSeqs that failed with a 401/403
	// during this run. The auth-refresh caller feeds them back as
	// the next attempt's JobConfig.RefetchSeqs so the poller re-
	// enqueues them under a fresh signed URL. Without this, a
	// mid-stream token expiry leaves a hole in the output at the
	// seq that tripped the refresh.
	AuthErrorSeqs []int64
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

	// Bounded queue: 2 × worker count per spec.
	// Producer blocks when full → natural backpressure so the
	// poller doesn't outrun the fetchers during a CDN burst.
	jobChanCap := 2 * max(1, cfg.SegmentConcurrency)
	jobs := make(chan segmentJob, jobChanCap)
	results := make(chan SegmentResult, jobChanCap)
	first := make(chan PollResult, 1)
	// skipEvents carries sequence-level skip events from the poller
	// (every reason — stitched ads today, other defect classes as
	// they land). Orchestrator drains them alongside worker results
	// so SegmentEvent ordering stays a single stream. Buffered same
	// as jobs so a burst of skips during a poll doesn't block the
	// poll loop.
	skipEvents := make(chan SkipEvent, jobChanCap)

	// Materialize the RefetchSeqs slice into a map the Poller can
	// do O(1) membership checks against. Nil slices yield a nil
	// map, which is valid — the Poller's refetch[seq] read
	// returns false without panicking.
	var refetchMap map[int64]bool
	if len(cfg.RefetchSeqs) > 0 {
		refetchMap = make(map[int64]bool, len(cfg.RefetchSeqs))
		for _, s := range cfg.RefetchSeqs {
			refetchMap[s] = true
		}
	}
	poller := &Poller{
		URL:           cfg.MediaPlaylistURL,
		HTTPClient:    cfg.PlaylistClient,
		Log:           log,
		StartMediaSeq: cfg.StartMediaSeq,
		SkipEvents:    skipEvents,
		ClassifyAuth:  cfg.ClassifyAuth,
		RefetchSeqs:   refetchMap,
	}
	pool := &Pool{
		Fetcher: cfg.Fetcher,
		WorkDir: cfg.WorkDir,
		Workers: cfg.SegmentConcurrency,
		Log:     log,
	}

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
		// Poller closes jobs via its defer; we close skipEvents
		// here in lock-step so the drain loop's select picks up
		// both closures together. Deferred so it fires on any
		// Run return path.
		defer close(skipEvents)
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
	result := &JobResult{
		// Seed policy-relevant counters so MaxGapRatio + the
		// first-content-segment guard evaluate against the
		// cumulative per-part total across auth-refresh attempts.
		// Attribute-counters (BytesWritten, SegmentsAdGaps) stay
		// zero — they're per-attempt for aggregation upstream.
		SegmentsDone: cfg.SeedSegmentsDone,
		SegmentsGaps: cfg.SeedSegmentsGaps,
	}
	var pr PollResult
	select {
	case pr = <-first:
	case <-gctx.Done():
		return result, g.Wait()
	}
	result.Kind = pr.Kind
	// Window-roll fires first so the resume gap is recorded
	// before any frontier/segment callback can observe state.
	// In practice a window-roll only appears for resumed jobs
	// (StartMediaSeq > 0), and OnFirstPoll's StartPart is a
	// no-op on already-bootstrapped resume state — so the
	// ordering is conservative rather than load-bearing.
	if cfg.OnWindowRoll != nil && pr.WindowRollFrom > 0 && pr.WindowRollTo >= pr.WindowRollFrom {
		cfg.OnWindowRoll(pr.WindowRollFrom, pr.WindowRollTo)
	}
	if cfg.OnFirstPoll != nil {
		cfg.OnFirstPoll(pr.MediaSequenceBase)
	}
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

	abortErr, authErr := drainOutcomes(&cfg, result, results, skipEvents, cancel, log)

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

// emitEvent invokes onEvent with the given event if onEvent is
// non-nil. Nil-safe so call sites don't need to guard; keeps the
// drain loop readable.
func emitEvent(onEvent func(SegmentEvent), ev SegmentEvent) {
	if onEvent != nil {
		onEvent(ev)
	}
}

// drainOutcomes consumes every SegmentResult + ad-skip event until
// both channels are closed, maintaining result counters, firing
// OnEvent, and streaming Progress snapshots. Exported from Run for
// unit-testability: the drain's behavior after an auth/abort latch
// needs to be verified directly, and wiring a real CDN race to
// produce a "commit after cancel" outcome is hostile to the test
// runner.
//
// Every drained outcome advances LastMediaSeq, updates counters,
// and fires OnEvent + Progress — even after an abort marker is
// set. Reason: once the worker has finished (file written, error
// observed, ad skipped) the work has already happened on disk or
// on the wire; the next attempt will skip past LastMediaSeq and
// never re-process it, so durable accounting (OnEvent) and totals
// (counters) must see it. The abort/auth markers latch once to
// trigger cancel() exactly once and to drive Run's return value —
// they are NOT used to gate counters or events.
//
// The returned (abortErr, authErr) carry whichever marker was
// latched first; Run's caller errors-out on authErr in preference
// to abortErr so the auth-refresh loop can distinguish "refresh
// the token" from "give up."
func drainOutcomes(
	cfg *JobConfig,
	result *JobResult,
	results <-chan SegmentResult,
	skipEvents <-chan SkipEvent,
	cancel context.CancelFunc,
	log *slog.Logger,
) (*GapAbortError, error) {
	var abortErr *GapAbortError
	var authErr error

	resultsCh := results
	skipEventsCh := skipEvents
	for resultsCh != nil || skipEventsCh != nil {
		select {
		case res, ok := <-resultsCh:
			if !ok {
				resultsCh = nil
				continue
			}
			// LastMediaSeq advances on every outcome — success,
			// gap, auth error. The auth-refresh caller uses it
			// as the next attempt's StartMediaSeq, so advancing
			// past an auth-errored segment means that segment
			// is not retried on refresh: it becomes a gap.
			// Trade-off — on a fast refresh cycle the segment
			// has usually rolled off the CDN window anyway.
			if res.MediaSeq > result.LastMediaSeq {
				result.LastMediaSeq = res.MediaSeq
			}
			if res.Err != nil {
				// Permanent auth (entitlement restriction,
				// geoblock): short-circuit the refresh loop —
				// the outer caller's errors.Is(ErrPlaylistAuth)
				// check must return false so fetchWithAuthRefresh
				// bails to the job-level failure path rather
				// than burning the auth-refresh budget.
				if IsAuthPermanent(res.Err) {
					if authErr == nil && abortErr == nil {
						authErr = fmt.Errorf("hls: segment seq=%d permanent auth: %w", res.MediaSeq, ErrPlaylistAuthPermanent)
						log.Info("segment permanent auth failure; failing job", "seq", res.MediaSeq)
						cancel()
					}
					emitEvent(cfg.OnEvent, SegmentEvent{
						MediaSeq: res.MediaSeq,
						Outcome:  OutcomeAuth,
						Err:      res.Err,
					})
					continue
				}
				// Retryable auth: the outer auth-refresh loop
				// handles it. First auth error latches authErr +
				// cancel(); subsequent ones in the drain tail
				// still emit OnEvent for accounting but don't
				// re-trigger.
				//
				// AuthErrorSeqs collects every retryable-auth seq
				// so the next attempt can refetch them with the
				// fresh URL — without this the output file has a
				// hole at the seq that tripped the refresh.
				// Permanent-auth seqs are intentionally NOT in
				// this list: a refresh won't unlock an
				// entitlement restriction.
				if IsAuth(res.Err) {
					result.AuthErrorSeqs = append(result.AuthErrorSeqs, res.MediaSeq)
					if authErr == nil && abortErr == nil {
						authErr = fmt.Errorf("hls: segment seq=%d auth error: %w", res.MediaSeq, ErrPlaylistAuth)
						log.Info("segment auth error; requesting refresh", "seq", res.MediaSeq)
						cancel()
					}
					emitEvent(cfg.OnEvent, SegmentEvent{
						MediaSeq: res.MediaSeq,
						Outcome:  OutcomeAuth,
						Err:      res.Err,
					})
					continue
				}
				// Once aborting (auth or gap), treat subsequent
				// failures as accepted gaps — the files are lost,
				// the next attempt will skip past LastMediaSeq.
				// Not evaluating the policy again avoids
				// re-assigning abortErr to a later, less-
				// informative trigger.
				if authErr != nil || abortErr != nil {
					result.SegmentsGaps++
					log.Debug("segment gap accepted post-abort", "seq", res.MediaSeq, "error", res.Err)
					emitEvent(cfg.OnEvent, SegmentEvent{
						MediaSeq: res.MediaSeq,
						Outcome:  OutcomeGapAccepted,
						Err:      res.Err,
					})
				} else if gapErr := evaluateGap(&cfg.GapPolicy, result, res); gapErr == nil {
					result.SegmentsGaps++
					log.Debug("segment gap accepted", "seq", res.MediaSeq, "error", res.Err)
					emitEvent(cfg.OnEvent, SegmentEvent{
						MediaSeq: res.MediaSeq,
						Outcome:  OutcomeGapAccepted,
						Err:      res.Err,
					})
				} else {
					// Trigger gap: causes the abort, so it is
					// intentionally not counted or emitted — the
					// GapAbortError carries its seq + err.
					abortErr = gapErr
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
				emitEvent(cfg.OnEvent, SegmentEvent{
					MediaSeq:     res.MediaSeq,
					Outcome:      OutcomeCommitted,
					BytesWritten: res.BytesWritten,
				})
			}
			emitProgress(cfg.Progress, result)

		case ev, ok := <-skipEventsCh:
			if !ok {
				skipEventsCh = nil
				continue
			}
			// Skips advance LastMediaSeq too — resume must see
			// them as "accounted for, no retry." Otherwise the
			// next attempt's StartMediaSeq would be before the
			// skipped seqs and re-process them.
			if ev.MediaSeq > result.LastMediaSeq {
				result.LastMediaSeq = ev.MediaSeq
			}
			switch ev.Reason {
			case SkipReasonStitchedAd:
				// Structurally expected: Twitch-injected ad
				// content is not a CDN or transport failure.
				// Counted separately from SegmentsGaps so
				// MaxGapRatio doesn't trip on ad-heavy streams.
				result.SegmentsAdGaps++
				emitEvent(cfg.OnEvent, SegmentEvent{
					MediaSeq: ev.MediaSeq,
					Outcome:  OutcomeAdSkipped,
				})
			default:
				// Unknown reason — defensive fallback. Log +
				// advance the frontier so we don't stall, but
				// don't apply any policy. Future reasons get an
				// explicit case branch above.
				log.Warn("unknown skip reason; advancing frontier without policy",
					"seq", ev.MediaSeq,
					"reason", ev.Reason)
			}
			emitProgress(cfg.Progress, result)
		}
	}
	return abortErr, authErr
}

// emitProgress does a non-blocking snapshot send onto the Progress
// channel when non-nil. Nil-safe; drop-on-contention is the spec's
// Progress contract (cumulative, informational).
func emitProgress(ch chan<- Progress, r *JobResult) {
	if ch == nil {
		return
	}
	select {
	case ch <- Progress{
		SegmentsDone:   r.SegmentsDone,
		SegmentsGaps:   r.SegmentsGaps,
		SegmentsAdGaps: r.SegmentsAdGaps,
		BytesWritten:   r.BytesWritten,
		Kind:           r.Kind,
		InitURI:        r.InitURI,
	}:
	default:
	}
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
	// Init segment is a one-shot: no CDN-lag cadence to tune, so
	// we pass 0 and let the Fetcher fall back to its default.
	if _, err := f.Fetch(ctx, url, w, 0); err != nil {
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
