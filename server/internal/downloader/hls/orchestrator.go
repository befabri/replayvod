package hls

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/befabri/replayvod/server/internal/background"

	"golang.org/x/sync/errgroup"
)

// JobConfig supplies acquisition inputs and synchronous observation callbacks for Run.
type JobConfig struct {
	// WriteFile may be nil to write directly; managed callers must supply scratch accounting.
	WriteFile func(context.Context, *os.File, []byte) (int, error)

	// MediaPlaylistURL must already include any required authorization query parameters.
	MediaPlaylistURL string

	// WorkDir must exist and be writable; fMP4 initialization uses init.mp4 within it.
	WorkDir string

	Fetcher *Fetcher

	// PlaylistClient defaults to http.DefaultClient and may use different timeouts than Fetcher.
	PlaylistClient *http.Client

	// SegmentConcurrency defaults to four when nonpositive.
	SegmentConcurrency int

	Log *slog.Logger

	// RateLimiter may be nil for unlimited acquisition.
	RateLimiter RateLimiter

	// OnProgress runs synchronously and must finish quickly; no callback survives Run.
	OnProgress func(Progress)

	GapPolicy GapPolicy

	// StartMediaSeq excludes earlier sequences except RefetchSeqs; zero starts from the playlist head.
	StartMediaSeq int64

	// OnEvent reports exact outcomes in processing order, which can differ from media sequence order.
	// Callbacks run sequentially and must finish quickly.
	OnEvent func(SegmentEvent)

	// OnFirstPoll runs before any segment outcome, allowing callers to persist the acquisition anchor.
	OnFirstPoll func(PollResult)

	// OnWindowRoll runs before OnFirstPoll when recovery loses the inclusive range [from, to].
	// The playlist target duration estimates lost time; callers must record the loss to advance
	// recovery.
	OnWindowRoll func(from, to int64, targetDuration time.Duration)

	// OnMidStreamWindowRoll records the inclusive lost range [from, to] during capture.
	// A missing callback makes unrecorded window loss fatal.
	OnMidStreamWindowRoll func(from, to int64)

	// ClassifyAuth identifies permanent playlist authorization failures; nil treats 401/403 as
	// refreshable.
	ClassifyAuth func(status int, body []byte) (permanent bool)

	// SeedSegmentsDone and SeedSegmentsGaps retain per-part gap policy across authentication
	// refreshes.
	SeedSegmentsDone int64
	SeedSegmentsGaps int64

	// RefetchSeqs retries unresolved sequences below StartMediaSeq under a fresh playback URL.
	// Sequences already absent from the CDN remain gaps.
	RefetchSeqs []int64
}

// GapPolicy bounds tolerated content loss; by default, some real content must precede any gap.
type GapPolicy struct {
	// Strict aborts the job on the first segment failure.
	// Overrides MaxGapRatio when true.
	Strict bool

	// MaxGapRatio defaults to 0.01 when nonpositive; use Strict for zero tolerance.
	MaxGapRatio float64

	// SkipFirstContentGuard permits gaps before any real content has been saved.
	SkipFirstContentGuard bool
}

func (p *GapPolicy) normalize() {
	if p.MaxGapRatio <= 0 {
		p.MaxGapRatio = 0.01
	}
}

// Progress contains cumulative acquisition counters; newer snapshots supersede older ones.
type Progress struct {
	SegmentsDone int64
	SegmentsGaps int64
	// SegmentsAdGaps is excluded from gap policy because stitched advertisements are not content loss.
	SegmentsAdGaps int64
	// SegmentsTotal is zero until the playlist closes.
	SegmentsTotal int64
	BytesWritten  int64
	Kind          SegmentKind
	InitURI       string
}

// JobResult includes saved media counters even when acquisition ends with an error.
type JobResult struct {
	SegmentsDone   int64
	SegmentsGaps   int64
	SegmentsAdGaps int64
	// SegmentsCanceled counts uncommitted fetches that require same-sequence retries.
	SegmentsCanceled int64
	// CanceledSeqs must be resolved by later attempts before the aggregate can claim ENDLIST.
	CanceledSeqs []int64
	BytesWritten int64
	Kind         SegmentKind
	InitURI      string // empty for ts jobs
	LastMediaSeq int64

	// AuthErrorSeqs must be passed as RefetchSeqs after renewal to avoid holes below the cursor.
	AuthErrorSeqs []int64

	// EndList requires both ENDLIST and every final queued segment to finish durably.
	EndList bool
}

// GapAbortError identifies a content loss that exceeded policy.
type GapAbortError struct {
	Reason  string
	Done    int64
	Gaps    int64
	LastSeq int64
	LastErr error
}

func (e *GapAbortError) Error() string {
	return fmt.Sprintf("hls job: gap policy abort (%s): done=%d gaps=%d last_seq=%d: %v",
		e.Reason, e.Done, e.Gaps, e.LastSeq, e.LastErr)
}

func (e *GapAbortError) Unwrap() error { return e.LastErr }

// Run acquires a playlist through ENDLIST, cancellation, or fatal failure.
// It joins acquisition workers before returning; callers handle playback-token renewal.
func Run(ctx context.Context, cfg JobConfig) (*JobResult, error) {
	if err := validateJobConfig(&cfg); err != nil {
		return nil, err
	}
	cfg.GapPolicy.normalize()

	log := cfg.Log.With("domain", "hls.job")

	// Bounded queues prevent playlist polling from outrunning segment acquisition.
	jobChanCap := 2 * max(1, cfg.SegmentConcurrency)
	jobs := make(chan segmentJob, jobChanCap)
	results := make(chan SegmentResult, jobChanCap)
	first := make(chan PollResult, 1)
	// Process poller skips and worker results in one goroutine to serialize accounting callbacks.
	skipEvents := make(chan SkipEvent, jobChanCap)

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
		WriteFile: cfg.WriteFile,
		Fetcher:   cfg.Fetcher,
		WorkDir:   cfg.WorkDir,
		Workers:   cfg.SegmentConcurrency,
		Log:       log,
		Limiter:   cfg.RateLimiter,
	}

	// Synchronous bootstrap and callback failures must cancel and join acquisition workers.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	g, gctx := errgroup.WithContext(runCtx)
	defer func() { cancel(); _ = g.Wait() }()

	var pollerErrMu sync.Mutex
	var pollerErr error
	recordPollerErr := func(err error) {
		pollerErrMu.Lock()
		pollerErr = err
		pollerErrMu.Unlock()
	}
	shouldIgnoreCanceledSegment := func() bool {
		pollerErrMu.Lock()
		err := pollerErr
		pollerErrMu.Unlock()
		return !errors.Is(err, ErrPlaylistGone)
	}

	g.Go(func() error {
		// Close skips when the poller exits so outcome draining cannot wait forever.
		defer close(skipEvents)
		err := background.Call(gctx, func(c context.Context) error { return poller.Run(c, first, jobs) })
		recordPollerErr(err)
		return err
	})

	// Fetch fMP4 initialization before starting segment workers.
	result := &JobResult{
		// Preserve per-part policy counters; bytes and advertisement gaps remain per attempt.
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
	// Record the lost range before any callback advances the durable frontier.
	if cfg.OnWindowRoll != nil && pr.WindowRollFrom > 0 && pr.WindowRollTo >= pr.WindowRollFrom {
		cfg.OnWindowRoll(pr.WindowRollFrom, pr.WindowRollTo, pr.TargetDuration)
	}
	if cfg.OnFirstPoll != nil {
		cfg.OnFirstPoll(pr)
	}
	if pr.Init != nil {
		result.InitURI = pr.Init.URI
		if err := fetchInit(gctx, cfg.Fetcher, cfg.WorkDir, pr.Init.URI, cfg.WriteFile); err != nil {
			// Without initialization, fMP4 segments cannot be decoded.
			log.Error("init segment fetch failed; aborting job", "error", err)
			cancel()
			_ = g.Wait()
			return result, fmt.Errorf("hls init segment: %w", err)
		}
	}

	var poolCompletedClean atomic.Bool
	g.Go(func() error {
		err := pool.Run(gctx, jobs, results)
		if err == nil {
			poolCompletedClean.Store(true)
		}
		return err
	})

	abortErr, authErr := drainOutcomes(&cfg, result, results, skipEvents, cancel, shouldIgnoreCanceledSegment, poller.totalSegments.Load, log)

	if authErr != nil {
		_ = g.Wait()
		return result, authErr
	}
	if abortErr != nil {
		_ = g.Wait()
		return result, abortErr
	}

	// Cancellation still returns captured counters for checkpointing.
	if err := g.Wait(); err != nil &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded) {
		return result, err
	}
	// ENDLIST alone cannot prove completion if cancellation interrupted the final worker queue.
	result.EndList = poller.endListSeen && poolCompletedClean.Load() && result.SegmentsCanceled == 0
	return result, nil
}

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
	ignoreCanceledSegment func() bool,
	segmentsTotal func() int64,
	log *slog.Logger,
) (*GapAbortError, error) {
	var abortErr *GapAbortError
	var authErr error
	if segmentsTotal == nil {
		segmentsTotal = func() int64 { return 0 }
	}

	resultsCh := results
	skipEventsCh := skipEvents
	for resultsCh != nil || skipEventsCh != nil {
		select {
		case res, ok := <-resultsCh:
			if !ok {
				resultsCh = nil
				continue
			}
			if res.Err != nil && isCanceledSegmentResult(res.Err) &&
				(ignoreCanceledSegment == nil || ignoreCanceledSegment()) {
				result.SegmentsCanceled++
				result.CanceledSeqs = append(result.CanceledSeqs, res.MediaSeq)
				log.Debug("segment fetch canceled before commit; leaving unresolved for retry",
					"seq", res.MediaSeq,
					"error", res.Err)
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
					MediaSeq:        res.MediaSeq,
					Outcome:         OutcomeCommitted,
					BytesWritten:    res.BytesWritten,
					DurationSeconds: res.DurationSeconds,
				})
			}
			emitProgress(cfg.OnProgress, result, segmentsTotal())

		case ev, ok := <-skipEventsCh:
			if !ok {
				skipEventsCh = nil
				continue
			}
			// Skips are already accounted for; advancing LastMediaSeq prevents
			// retries. Range skips use EndMediaSeq as the frontier.
			advanceSkip := func() int64 {
				skipEnd := max(ev.MediaSeq, ev.EndMediaSeq)
				if skipEnd > result.LastMediaSeq {
					result.LastMediaSeq = skipEnd
				}
				return skipEnd
			}
			switch ev.Reason {
			case SkipReasonStitchedAd:
				advanceSkip()
				// Structurally expected: Twitch-injected ad
				// content is not a CDN or transport failure.
				// Counted separately from SegmentsGaps so
				// MaxGapRatio doesn't trip on ad-heavy streams.
				result.SegmentsAdGaps++
				emitEvent(cfg.OnEvent, SegmentEvent{
					MediaSeq: ev.MediaSeq,
					Outcome:  OutcomeAdSkipped,
				})
			case SkipReasonMalformed:
				advanceSkip()
				// Real content loss, not structurally expected.
				// Apply the same gap policy a worker failure
				// would: first-content-guard aborts if no
				// content has committed yet, and the running
				// MaxGapRatio computation aborts a truly
				// pathological manifest (many zero-duration
				// segments).
				//
				// OutcomeMalformedSkip rather than the generic
				// OutcomeGapAccepted so resume-state consumers
				// can attribute the gap to structural manifest
				// defect (GapReasonMalformed) rather than a
				// fetch failure.
				//
				// When already aborting (prior gap/auth), count
				// it post-abort — the next attempt's cursor
				// will skip past, matching the "the skip already
				// happened" semantics of the results branch.
				if authErr != nil || abortErr != nil {
					result.SegmentsGaps++
					emitEvent(cfg.OnEvent, SegmentEvent{
						MediaSeq: ev.MediaSeq,
						Outcome:  OutcomeMalformedSkip,
					})
				} else if gapErr := evaluateMalformedGap(&cfg.GapPolicy, result, ev.MediaSeq); gapErr == nil {
					result.SegmentsGaps++
					log.Debug("malformed segment accepted as gap", "seq", ev.MediaSeq)
					emitEvent(cfg.OnEvent, SegmentEvent{
						MediaSeq: ev.MediaSeq,
						Outcome:  OutcomeMalformedSkip,
					})
				} else {
					abortErr = gapErr
					log.Warn("malformed segment aborts job",
						"reason", abortErr.Reason,
						"seq", ev.MediaSeq,
						"done", result.SegmentsDone,
						"gaps", result.SegmentsGaps)
					cancel()
					continue
				}
			case SkipReasonWindowRolled:
				if cfg.OnMidStreamWindowRoll == nil {
					if authErr == nil && abortErr == nil {
						abortErr = &GapAbortError{
							Reason:  "mid-stream window roll callback not configured",
							Done:    result.SegmentsDone,
							Gaps:    result.SegmentsGaps,
							LastSeq: ev.MediaSeq,
							LastErr: errors.New("playlist window rolled mid-stream without range-gap callback"),
						}
						log.Error("playlist window rolled mid-stream without range-gap callback; leaving frontier unresolved",
							"from", ev.MediaSeq,
							"to", ev.EndMediaSeq)
						cancel()
					}
					continue
				}
				skipEnd := advanceSkip()
				lostSegments := skipEnd - ev.MediaSeq + 1
				if lostSegments < 1 {
					lostSegments = 1
				}
				// Mid-stream rolls are real range gaps; count the whole lost range
				// before calling durable accounting.
				acceptWindowRoll := func(postAbort bool) {
					gapsBefore := result.SegmentsGaps
					result.SegmentsGaps += lostSegments
					total := result.SegmentsDone + result.SegmentsGaps
					var gapRatio float64
					if total > 0 {
						gapRatio = float64(result.SegmentsGaps) / float64(total)
					}
					log.Warn("mid-stream window roll accepted as gap",
						"from", ev.MediaSeq,
						"to", skipEnd,
						"lost_segments", lostSegments,
						"done", result.SegmentsDone,
						"gaps_before", gapsBefore,
						"gaps_after", result.SegmentsGaps,
						"gap_ratio", gapRatio,
						"max_gap_ratio", cfg.GapPolicy.MaxGapRatio,
						"post_abort", postAbort)
					cfg.OnMidStreamWindowRoll(ev.MediaSeq, skipEnd)
				}
				if authErr != nil || abortErr != nil {
					acceptWindowRoll(true)
				} else if gapErr := evaluateWindowRollGap(&cfg.GapPolicy, result, ev.MediaSeq, skipEnd); gapErr == nil {
					acceptWindowRoll(false)
				} else {
					abortErr = gapErr
					log.Warn("mid-stream window roll aborts job",
						"reason", abortErr.Reason,
						"from", ev.MediaSeq,
						"to", skipEnd,
						"done", result.SegmentsDone,
						"gaps", result.SegmentsGaps)
					cancel()
					continue
				}
			default:
				advanceSkip()
				// Unknown reason — defensive fallback. Log +
				// advance the frontier so we don't stall, but
				// don't apply any policy. Future reasons get an
				// explicit case branch above.
				log.Warn("unknown skip reason; advancing frontier without policy",
					"seq", ev.MediaSeq,
					"reason", ev.Reason)
			}
			emitProgress(cfg.OnProgress, result, segmentsTotal())
		}
	}
	return abortErr, authErr
}

func emitProgress(observe func(Progress), r *JobResult, total int64) {
	if observe == nil {
		return
	}
	observe(Progress{
		SegmentsDone:   r.SegmentsDone,
		SegmentsGaps:   r.SegmentsGaps,
		SegmentsAdGaps: r.SegmentsAdGaps,
		SegmentsTotal:  total,
		BytesWritten:   r.BytesWritten,
		Kind:           r.Kind,
		InitURI:        r.InitURI,
	})
}

func isCanceledSegmentResult(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

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

func evaluateMalformedGap(p *GapPolicy, r *JobResult, seq int64) *GapAbortError {
	reason := fmt.Errorf("malformed segment: EXTINF <= 0")
	if p.Strict {
		return &GapAbortError{
			Reason:  "strict mode (malformed segment)",
			Done:    r.SegmentsDone,
			Gaps:    r.SegmentsGaps,
			LastSeq: seq,
			LastErr: reason,
		}
	}
	if !p.SkipFirstContentGuard && r.SegmentsDone == 0 {
		return &GapAbortError{
			Reason:  "no content segment committed yet (malformed segment)",
			Done:    r.SegmentsDone,
			Gaps:    r.SegmentsGaps,
			LastSeq: seq,
			LastErr: reason,
		}
	}
	gapsAfter := r.SegmentsGaps + 1
	total := gapsAfter + r.SegmentsDone
	if float64(gapsAfter)/float64(total) > p.MaxGapRatio {
		return &GapAbortError{
			Reason:  fmt.Sprintf("gap ratio %.2f%% over ceiling %.2f%% (malformed)", 100*float64(gapsAfter)/float64(total), 100*p.MaxGapRatio),
			Done:    r.SegmentsDone,
			Gaps:    r.SegmentsGaps,
			LastSeq: seq,
			LastErr: reason,
		}
	}
	return nil
}

// evaluateWindowRollGap counts the entire lost range when evaluating the gap ratio.
func evaluateWindowRollGap(p *GapPolicy, r *JobResult, from, to int64) *GapAbortError {
	lostSegments := to - from + 1
	if lostSegments < 1 {
		lostSegments = 1
	}
	reason := fmt.Errorf("playlist window rolled mid-stream")
	if p.Strict {
		return &GapAbortError{
			Reason:  "strict mode (window roll)",
			Done:    r.SegmentsDone,
			Gaps:    r.SegmentsGaps,
			LastSeq: to,
			LastErr: reason,
		}
	}
	if !p.SkipFirstContentGuard && r.SegmentsDone == 0 {
		return &GapAbortError{
			Reason:  "no content segment committed yet (window roll)",
			Done:    r.SegmentsDone,
			Gaps:    r.SegmentsGaps,
			LastSeq: to,
			LastErr: reason,
		}
	}
	gapsAfter := r.SegmentsGaps + lostSegments
	total := gapsAfter + r.SegmentsDone
	if float64(gapsAfter)/float64(total) > p.MaxGapRatio {
		return &GapAbortError{
			Reason:  fmt.Sprintf("gap ratio %.2f%% over ceiling %.2f%% (window roll)", 100*float64(gapsAfter)/float64(total), 100*p.MaxGapRatio),
			Done:    r.SegmentsDone,
			Gaps:    r.SegmentsGaps,
			LastSeq: to,
			LastErr: reason,
		}
	}
	return nil
}

// fetchInit must succeed before fMP4 media can be decoded.
func fetchInit(ctx context.Context, f *Fetcher, workDir, url string, writeFile func(context.Context, *os.File, []byte) (int, error)) error {
	w, err := NewPartWriter(workDir, "init.mp4")
	if err != nil {
		return err
	}
	w.ctx, w.writeFile = ctx, writeFile
	defer w.Abort()
	if _, err := f.Fetch(ctx, url, w, 0); err != nil {
		return err
	}
	return w.Commit()
}

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
