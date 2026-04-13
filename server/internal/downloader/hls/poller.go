package hls

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// segmentJob is the unit of work produced by the poller and
// consumed by the worker pool. One per segment published by the
// media playlist, deduped by MediaSeq across poll cycles.
//
// Kind + Part name are baked in so the worker doesn't re-derive
// them; they're stable for the life of a part (changing them is
// a part-boundary event handled at the orchestrator level).
type segmentJob struct {
	Segment   Segment
	Kind      SegmentKind
	FinalName string // e.g. "42.ts" or "105.m4s"
}

// Poller polls a media playlist URL on a target-duration tick,
// diffs the returned segments against the highest mediaSeq it
// has already enqueued, and sends new segments to out. Termina-
// tion: playlist EndList → close(out) + return nil. ctx cancel →
// close(out) + return ctx.Err().
//
// Auth errors (401/403 on the playlist fetch) bubble up as the
// return value so the orchestrator can trigger auth refresh at
// the master-playlist level. Transient network + server errors
// are retried in-place with full-jitter backoff for a bounded
// number of attempts before escalating — a transient edge blip
// during a live stream shouldn't kill the job.
type Poller struct {
	// URL is the signed media-playlist URL. Owned by the
	// orchestrator; a new Poller is constructed if the URL
	// changes (e.g. after auth refresh or variant switch).
	URL string

	// HTTPClient is the http.Client used to fetch the playlist.
	// Distinct from the Fetcher's client so playlist calls can
	// carry different header defaults and timeouts without
	// affecting segment throughput.
	HTTPClient *http.Client

	// Log is the per-job logger; fields added by the poller
	// live under domain=hls.poller.
	Log *slog.Logger

	// MaxAttempts caps the in-place retry budget for transient
	// playlist fetch failures. Default 5.
	MaxAttempts int

	// MinTick is the lower bound applied to the observed
	// TargetDuration. Prevents a pathological manifest with a
	// 1-second TargetDuration from pounding the CDN. Default
	// 1 second.
	MinTick time.Duration

	// BackoffBase + BackoffMax control the full-jitter
	// exponential sleep between playlist-retry attempts.
	// Default 200ms / 5s — tuned for playlist fetches which
	// are small JSON-ish bodies, not for the segment-retry
	// path (which lives in FetcherConfig). Exposed so Phase
	// 4d's resume-only Poller path can use a tighter window.
	BackoffBase time.Duration
	BackoffMax  time.Duration

	// StartMediaSeq optionally skips segments whose MediaSeq is
	// below this threshold. Used by the auth-refresh and
	// resume-on-restart paths: after a fresh signed URL is
	// obtained, the Poller should pick up where the previous
	// attempt left off rather than re-emit already-committed
	// segments.
	//
	// Zero (the default) means "emit everything the playlist
	// publishes." Set to last_seen_mediaSeq + 1 to resume.
	//
	// Window-roll detection: if the playlist's first segment
	// has MediaSeq > StartMediaSeq the poller logs a warn and
	// continues from whatever the playlist exposes — those
	// segments are lost. Full gap-tracking on window roll is a
	// later phase.
	StartMediaSeq int64

	// AdSkips, when non-nil, receives the MediaSeq of each
	// stitched-ad segment the poller filters. The orchestrator
	// creates the channel, consumes events alongside worker
	// results, and closes its ownership via the same defer path
	// that closes the jobs channel. Sequence-level events (not a
	// counter) so resume-on-restart can record frontier advances
	// with typed reasons rather than reconstructing from a delta.
	//
	// Writes use the same select-with-ctx-cancel pattern as jobs:
	// a slow drain won't wedge the poll loop.
	AdSkips chan<- int64

	// ClassifyAuth, when non-nil, inspects a 401/403 response body
	// and reports whether the failure is permanent (entitlement
	// code, subscriber-only stream, geoblock). Permanent results
	// fail the job fast via ErrPlaylistAuthPermanent rather than
	// burning the auth-refresh budget on a stream we'll never be
	// allowed to watch. Nil means "always treat as retryable" —
	// behavior before the hook existed.
	ClassifyAuth func(status int, body []byte) (permanent bool)
}

// PollResult carries metadata observed on the first successful
// poll — the orchestrator needs Kind + Init to fetch the init
// segment (fmp4) before any media segment is fetched, and
// MediaSequenceBase to seed any downstream accounting that can't
// wait for the first segment outcome (e.g. resume-state frontier
// bootstrap, where using the first committed seq as the anchor
// would drop earlier out-of-order completions).
type PollResult struct {
	Kind              SegmentKind
	Init              *InitSegment
	TargetDuration    time.Duration
	MediaSequenceBase int64

	// WindowRollFrom / WindowRollTo report the range of MediaSeqs
	// the playlist has already rolled past since the caller's
	// StartMediaSeq. Populated only when both StartMediaSeq > 0
	// (i.e. a resume attempt) and the first segment's MediaSeq
	// exceeds it. The lost range is inclusive:
	// [StartMediaSeq, firstSegmentMediaSeq-1].
	//
	// Consumers record this as a restart_window_rolled gap so
	// the resume frontier can advance past the loss rather than
	// waiting forever for segments that will never be fetched.
	// Zero values (both) mean "no window roll observed."
	WindowRollFrom int64
	WindowRollTo   int64
}

// ErrPlaylistAuth signals a 401/403 on the playlist fetch. The
// orchestrator catches this and triggers an auth refresh + new
// playlist URL, then reconstructs the Poller. Wrapped via
// errors.Is on the sentinel; errors.As on *FetchError gives the
// status.
var ErrPlaylistAuth = errors.New("hls poller: playlist auth error")

// ErrPlaylistAuthPermanent signals a 401/403 the ClassifyAuth hook
// flagged as permanent — an entitlement restriction no amount of
// refreshing will fix (subscriber-only, geoblock, VOD manifest
// restriction). Distinct from ErrPlaylistAuth so the refresh loop
// in downloader.fetchWithAuthRefresh bails immediately instead of
// burning its budget. Deliberately not wrapped around
// ErrPlaylistAuth: callers that `errors.Is(err, ErrPlaylistAuth)`
// must not accidentally catch the permanent case.
var ErrPlaylistAuthPermanent = errors.New("hls poller: playlist auth permanent")

// Run executes the poll loop. On the first successful fetch it
// sends one PollResult onto first (buffered cap 1) so the
// orchestrator can bootstrap the init segment before the pool
// starts consuming segment jobs. Subsequent polls only emit
// segmentJobs; the first poll emits both the PollResult and all
// initial segments, in that order.
//
// Closes out on clean termination (ENDLIST or ctx). The orch-
// estrator uses that as the signal to drain the pool and report
// completion.
//
// Zero-value Log or HTTPClient are normalized to discard + the
// default HTTP client — Phase 4d's resume-path uses a stripped-
// down Poller to observe the current playlist head, and we want
// that to not panic on the hot path.
func (p *Poller) Run(ctx context.Context, first chan<- PollResult, out chan<- segmentJob) error {
	if p.Log == nil {
		p.Log = slog.New(slog.DiscardHandler)
	}
	if p.HTTPClient == nil {
		p.HTTPClient = http.DefaultClient
	}
	log := p.Log
	maxAttempts := p.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	minTick := p.MinTick
	if minTick <= 0 {
		minTick = time.Second
	}
	backoffBase := p.BackoffBase
	if backoffBase <= 0 {
		backoffBase = 200 * time.Millisecond
	}
	backoffMax := p.BackoffMax
	if backoffMax <= 0 {
		backoffMax = 5 * time.Second
	}

	defer close(out)

	// lastSeq: highest MediaSeq we've already enqueued. Resume
	// callers pass StartMediaSeq to skip segments already
	// committed in a prior attempt; lastSeq starts at
	// StartMediaSeq-1 so emission begins at StartMediaSeq.
	// For fresh starts (StartMediaSeq=0) this is -1, which
	// lets a seq-0 segment through unchanged.
	lastSeq := p.StartMediaSeq - 1
	var firstSent bool
	var warnedWindowRoll bool

	for attempt := 0; ; {
		pl, err := p.fetchAndParse(ctx)
		if err != nil {
			if isCanceled(ctx, err) {
				return err
			}
			// Auth errors don't retry — orchestrator needs to
			// refresh the playback token + master playlist and
			// hand us a new URL. Retrying the old URL with the
			// old signature won't change the outcome.
			if errors.Is(err, ErrPlaylistAuth) {
				return err
			}
			attempt++
			log.Warn("playlist fetch failed", "attempt", attempt, "error", err)
			if attempt >= maxAttempts {
				return fmt.Errorf("hls poller: playlist fetch exhausted after %d attempts: %w", attempt, err)
			}
			if sleepErr := sleepCtx(ctx, Backoff(attempt-1, backoffBase, backoffMax)); sleepErr != nil {
				return sleepErr
			}
			continue
		}
		attempt = 0

		// Window-roll detection: on the first poll after a
		// resume attempt, if the playlist has already rolled
		// past StartMediaSeq we've irreversibly lost those
		// segments. Flag the range on the first PollResult so
		// the orchestrator can forward it to the resume-state
		// writer; without this the accounted frontier would
		// wait forever for segments the CDN no longer serves.
		var windowRollFrom, windowRollTo int64
		if p.StartMediaSeq > 0 && !warnedWindowRoll && len(pl.Segments) > 0 {
			warnedWindowRoll = true
			if pl.Segments[0].MediaSeq > p.StartMediaSeq {
				windowRollFrom = p.StartMediaSeq
				windowRollTo = pl.Segments[0].MediaSeq - 1
				log.Warn("playlist window rolled past resume point",
					"resume_from", p.StartMediaSeq,
					"playlist_head", pl.Segments[0].MediaSeq,
					"lost_segments", pl.Segments[0].MediaSeq-p.StartMediaSeq)
			}
		}

		// Publish PollResult once, before any segment leaves
		// the poller. Guarantees the orchestrator sees Kind +
		// Init before a worker tries to use them. The window-
		// roll fields ride along on this same first emission
		// so the resume writer records the gap range before
		// the first NoteCommitted lands.
		if !firstSent {
			select {
			case first <- PollResult{
				Kind:              pl.Kind,
				Init:              pl.Init,
				TargetDuration:    pl.TargetDuration,
				MediaSequenceBase: pl.MediaSequenceBase,
				WindowRollFrom:    windowRollFrom,
				WindowRollTo:      windowRollTo,
			}:
			case <-ctx.Done():
				return ctx.Err()
			}
			firstSent = true
		}

		ext := segmentExt(pl.Kind)
		for _, seg := range pl.Segments {
			if seg.MediaSeq <= lastSeq {
				continue
			}
			if seg.IsAd {
				// Skip Twitch stitched-ad content entirely —
				// don't enqueue, don't fetch, don't write.
				// Emit a sequence-level ad-skip event so the
				// orchestrator can advance LastMediaSeq and
				// feed resume/accounted-frontier accounting.
				log.Debug("skipping stitched-ad segment", "seq", seg.MediaSeq)
				if p.AdSkips != nil {
					select {
					case p.AdSkips <- seg.MediaSeq:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				lastSeq = seg.MediaSeq
				continue
			}
			job := segmentJob{
				Segment:   seg,
				Kind:      pl.Kind,
				FinalName: fmt.Sprintf("%d%s", seg.MediaSeq, ext),
			}
			select {
			case out <- job:
				lastSeq = seg.MediaSeq
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		if pl.EndList {
			log.Debug("playlist endlist — poller done")
			return nil
		}

		tick := max(pl.TargetDuration, minTick)
		if sleepErr := sleepCtx(ctx, tick); sleepErr != nil {
			return sleepErr
		}
	}
}

// fetchAndParse performs one playlist GET, parses the body, and
// resolves all URIs (segments + init) against the playlist URL so
// downstream consumers only ever see absolute URLs. Resolution
// here — not in the pure parser — keeps ParseMediaPlaylist a
// file-level function that can be unit-tested without a URL.
//
// 401/403 wraps ErrPlaylistAuth so the orchestrator can branch
// without string-matching. Other non-2xx is returned as-is for
// the caller's retry logic.
func (p *Poller) fetchAndParse(ctx context.Context) (*MediaPlaylist, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer drainAndClose(resp)

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		// Classify before concluding "retryable auth" — a
		// subscriber-only stream returns 401/403 with a JSON
		// body whose error_code is permanent. Refreshing the
		// token won't grant access, so bail fast.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		if p.ClassifyAuth != nil && p.ClassifyAuth(resp.StatusCode, body) {
			return nil, fmt.Errorf("%w: status %d: %s", ErrPlaylistAuthPermanent, resp.StatusCode, truncateForLog(body))
		}
		return nil, fmt.Errorf("%w: status %d", ErrPlaylistAuth, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Truncated body preview, capped at 512B — playlist
		// 4xx/5xx bodies are small JSON/plaintext from the CDN.
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("hls poller: status %d: %s", resp.StatusCode, string(preview))
	}
	pl, err := ParseMediaPlaylist(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := resolveURIs(pl, p.URL); err != nil {
		return nil, fmt.Errorf("hls poller: resolve URIs: %w", err)
	}
	return pl, nil
}

// truncateForLog caps a body preview at 200 bytes + ellipsis so an
// error-wrapped body doesn't explode a log line when the CDN
// returns a verbose HTML page instead of a tight JSON body.
func truncateForLog(b []byte) string {
	const limit = 200
	if len(b) <= limit {
		return string(b)
	}
	return string(b[:limit]) + "…"
}

// resolveURIs mutates pl in place, replacing each relative URI
// (init + segments) with its absolute form resolved against base.
// Absolute URIs pass through untouched. Twitch's master playlist
// tends to emit absolute segment URIs; relative ones still happen
// on some transcode paths and are legal per HLS.
func resolveURIs(pl *MediaPlaylist, base string) error {
	baseURL, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("parse base %q: %w", base, err)
	}
	resolve := func(raw string) (string, error) {
		if raw == "" {
			return "", nil
		}
		u, err := url.Parse(raw)
		if err != nil {
			return "", fmt.Errorf("parse %q: %w", raw, err)
		}
		return baseURL.ResolveReference(u).String(), nil
	}
	if pl.Init != nil {
		resolved, err := resolve(pl.Init.URI)
		if err != nil {
			return err
		}
		pl.Init.URI = resolved
	}
	for i := range pl.Segments {
		resolved, err := resolve(pl.Segments[i].URI)
		if err != nil {
			return err
		}
		pl.Segments[i].URI = resolved
	}
	return nil
}

// segmentExt returns the filename extension the worker writes
// based on the container kind. Keeps the "where does the .ts
// vs .m4s decision live" answer in one place.
func segmentExt(k SegmentKind) string {
	if k == SegmentKindFMP4 {
		return ".m4s"
	}
	return ".ts"
}

// sleepCtx sleeps for d or until ctx is canceled. Returns
// ctx.Err() on cancel, nil otherwise. Kept local rather than
// importing twitch/fetch helpers — the poller has its own
// lifecycle needs and the call site is small.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// isCanceled reports whether err came from ctx cancel/timeout
// rather than a transport-layer problem worth retrying.
func isCanceled(ctx context.Context, err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return ctx.Err() != nil
}
