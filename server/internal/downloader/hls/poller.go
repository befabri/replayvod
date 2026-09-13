package hls

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"sync/atomic"
	"time"
)

// segmentJob fixes the container and filename for one sequence; changing either
// requires a new recording part.
type segmentJob struct {
	Segment   Segment
	Kind      SegmentKind
	FinalName string // e.g. "42.ts" or "105.m4s"

	// TargetDuration is the observed EXT-X-TARGETDURATION for this job.
	// CDN-lag retries use half of it without mutating the shared Fetcher.
	TargetDuration time.Duration
}

// Poller acquires media-playlist snapshots, emitting each eligible sequence once.
// Callers own authorization renewal; transient playlist failures retry in place.
type Poller struct {
	// URL must include the playlist signature; changing it requires a new Poller.
	URL string

	// HTTPClient defaults to http.DefaultClient and may differ from the segment client.
	HTTPClient *http.Client

	Log *slog.Logger

	// MaxAttempts defaults to five when nonpositive.
	MaxAttempts int

	// MinTick bounds the poll interval and defaults to one second when nonpositive.
	MinTick time.Duration

	// BackoffBase and BackoffMax default to 200ms and 5s for playlist retries.
	BackoffBase time.Duration
	BackoffMax  time.Duration

	// StartMediaSeq excludes earlier sequences except RefetchSeqs; zero starts at the playlist head.
	StartMediaSeq int64

	// SkipEvents reports filtered sequences and later window/refetch losses.
	// The caller owns closure; sends stop on cancellation when the consumer is blocked.
	SkipEvents chan<- SkipEvent

	// ClassifyAuth identifies permanent 401/403 restrictions; nil requests token renewal.
	ClassifyAuth func(status int, body []byte) (permanent bool)

	// RefetchSeqs bypasses StartMediaSeq for unresolved retries; nil disables refetching.
	// Run owns and mutates this map, retaining future sequences until visible or expired.
	// Callers must supply a fresh map and avoid accessing it until Run returns.
	RefetchSeqs map[int64]bool

	// ResolvedSeqs identifies durable media or accepted gaps, taking precedence over RefetchSeqs.
	// The caller must not change this map while Run is active.
	ResolvedSeqs map[int64]bool

	// endListSeen is valid after the poller joins; the orchestrator must also verify worker completion.
	endListSeen bool

	// totalSegments is the number of eligible segments across this run,
	// published as soon as ENDLIST is observed, before sending the remaining
	// jobs to workers. Live playlists keep an unknown total (zero).
	totalSegments atomic.Int64
}

// PollResult supplies initialization and initial losses before any media acquisition.
// Empty live polls defer it so a later window advance cannot lose its recovery anchor.
type PollResult struct {
	Kind              SegmentKind
	Init              *InitSegment
	TargetDuration    time.Duration
	MediaSequenceBase int64

	// WindowRollFrom and WindowRollTo delimit the inclusive loss before the first playlist head.
	// Both are zero when StartMediaSeq is zero or the head has not advanced past it.
	WindowRollFrom int64
	WindowRollTo   int64

	// ExpiredRefetchSeqs lists unavailable retries once each, in ascending sequence order.
	ExpiredRefetchSeqs []int64
}

// ErrPlaylistAuth requests token renewal after a playlist 401/403.
var ErrPlaylistAuth = errors.New("hls poller: playlist auth error")

// ErrPlaylistAuthPermanent identifies an entitlement or geographic restriction
// that renewal cannot fix; it does not wrap ErrPlaylistAuth.
var ErrPlaylistAuthPermanent = errors.New("hls poller: playlist auth permanent")

// ErrPlaylistGone identifies a playlist 404/410; Twitch uses it when a rendition
// disappears, so callers can split the recording and select another rendition.
var ErrPlaylistGone = errors.New("hls poller: media playlist gone")

// Run sends one PollResult before jobs, deferring empty live playlists.
// It closes out on every return; callers own first and SkipEvents.
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

	for seq := range p.ResolvedSeqs {
		delete(p.RefetchSeqs, seq)
	}

	// Starting below the cursor preserves sequence zero on fresh recordings.
	lastSeq := p.StartMediaSeq - 1
	var firstSent bool
	var warnedWindowRoll bool
	var emitted int64

	for attempt := 0; ; {
		pl, err := p.fetchAndParse(ctx)
		if err != nil {
			if isCanceled(ctx, err) {
				return err
			}
			// The signed URL must change before another authorization attempt can succeed.
			if errors.Is(err, ErrPlaylistAuth) {
				return err
			}
			// Twitch removes dropped renditions permanently at this URL.
			if errors.Is(err, ErrPlaylistGone) {
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

		// Initial loss must reach the caller before an accounting anchor can advance past it.
		var windowRollFrom, windowRollTo int64
		if p.StartMediaSeq > 0 && !warnedWindowRoll && (len(pl.Segments) > 0 || pl.EndList) {
			warnedWindowRoll = true
			if pl.MediaSequenceBase > p.StartMediaSeq {
				windowRollFrom = p.StartMediaSeq
				windowRollTo = pl.MediaSequenceBase - 1
				log.Warn("playlist window rolled past resume point",
					"resume_from", p.StartMediaSeq,
					"playlist_head", pl.MediaSequenceBase,
					"lost_segments", pl.MediaSequenceBase-p.StartMediaSeq)
			}
		}

		expiredRefetches := p.expiredRefetches(pl)
		hadFirstPoll := firstSent

		// Publish PollResult once, before any segment leaves the poller. Empty
		// first polls defer this until segments or ENDLIST, otherwise a resume
		// window roll discovered on the next non-empty poll would be dropped.
		if !firstSent && (len(pl.Segments) > 0 || pl.EndList) {
			select {
			case first <- PollResult{
				Kind:               pl.Kind,
				Init:               pl.Init,
				TargetDuration:     pl.TargetDuration,
				MediaSequenceBase:  pl.MediaSequenceBase,
				WindowRollFrom:     windowRollFrom,
				WindowRollTo:       windowRollTo,
				ExpiredRefetchSeqs: expiredRefetches,
			}:
			case <-ctx.Done():
				return ctx.Err()
			}
			firstSent = true
			for _, seq := range expiredRefetches {
				delete(p.RefetchSeqs, seq)
			}
			expiredRefetches = nil
		}

		// Once the initial snapshot is reported, later head advances are live loss,
		// even when earlier snapshots contained no sequences eligible for acquisition.
		if hadFirstPoll && (len(pl.Segments) > 0 || pl.EndList) {
			if from, to, ok := midStreamRollRange(pl.MediaSequenceBase, lastSeq); ok {
				log.Warn("playlist window rolled mid-stream; segments lost",
					"frontier", lastSeq,
					"playlist_head", pl.MediaSequenceBase,
					"from", from,
					"to", to,
					"lost_segments", to-from+1)
				if p.SkipEvents != nil {
					select {
					case p.SkipEvents <- SkipEvent{MediaSeq: from, EndMediaSeq: to, Reason: SkipReasonWindowRolled}:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				lastSeq = to
			}
		}

		for _, seq := range expiredRefetches {
			if p.SkipEvents != nil {
				select {
				case p.SkipEvents <- SkipEvent{MediaSeq: seq, Reason: SkipReasonRefetchExpired}:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			delete(p.RefetchSeqs, seq)
		}

		if pl.EndList {
			// Publish before sending to the bounded worker channel. Otherwise
			// a VOD has no total until virtually all of it has downloaded.
			total := emitted
			for _, seg := range pl.Segments {
				if !p.ResolvedSeqs[seg.MediaSeq] && (seg.MediaSeq > lastSeq || p.RefetchSeqs[seg.MediaSeq]) && !seg.IsAd && seg.Duration > 0 {
					total++
				}
			}
			p.totalSegments.Store(total)
		}
		ext := segmentExt(pl.Kind)
		for _, seg := range pl.Segments {
			// Unresolved retries bypass the forward cursor until fetched or permanently lost.
			refetch := p.RefetchSeqs[seg.MediaSeq]
			if !refetch && seg.MediaSeq <= lastSeq {
				continue
			}
			if p.ResolvedSeqs[seg.MediaSeq] {
				if p.SkipEvents != nil {
					select {
					case p.SkipEvents <- SkipEvent{MediaSeq: seg.MediaSeq, Reason: SkipReasonResolved}:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				lastSeq = max(lastSeq, seg.MediaSeq)
				continue
			}
			if seg.IsAd {
				// Skipped advertisements still need durable sequence accounting.
				log.Debug("skipping stitched-ad segment", "seq", seg.MediaSeq)
				if p.SkipEvents != nil {
					select {
					case p.SkipEvents <- SkipEvent{MediaSeq: seg.MediaSeq, Reason: SkipReasonStitchedAd}:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				if seg.MediaSeq > lastSeq {
					lastSeq = seg.MediaSeq
				}
				delete(p.RefetchSeqs, seg.MediaSeq)
				continue
			}
			if seg.Duration <= 0 {
				// Nonpositive EXTINF silently shortens output, which remux duration checks
				// cannot detect; report the loss before acquisition so gap policy can reject it.
				log.Warn("skipping malformed segment (EXTINF <= 0)",
					"seq", seg.MediaSeq,
					"duration", seg.Duration)
				if p.SkipEvents != nil {
					select {
					case p.SkipEvents <- SkipEvent{MediaSeq: seg.MediaSeq, Reason: SkipReasonMalformed}:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				if seg.MediaSeq > lastSeq {
					lastSeq = seg.MediaSeq
				}
				delete(p.RefetchSeqs, seg.MediaSeq)
				continue
			}
			job := segmentJob{
				Segment:        seg,
				Kind:           pl.Kind,
				FinalName:      fmt.Sprintf("%d%s", seg.MediaSeq, ext),
				TargetDuration: pl.TargetDuration,
			}
			select {
			case out <- job:
				emitted++
				if seg.MediaSeq > lastSeq {
					lastSeq = seg.MediaSeq
				}
				delete(p.RefetchSeqs, seg.MediaSeq)
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		if pl.EndList {
			log.Debug("playlist endlist — poller done", "segments", emitted)
			p.totalSegments.Store(emitted)
			p.endListSeen = true
			return nil
		}

		tick := max(pl.TargetDuration, minTick)
		if sleepErr := sleepCtx(ctx, tick); sleepErr != nil {
			return sleepErr
		}
	}
}

// expiredRefetches retains future sequences while a live playlist can still expose them.
func (p *Poller) expiredRefetches(pl *MediaPlaylist) []int64 {
	if len(p.RefetchSeqs) == 0 || (len(pl.Segments) == 0 && !pl.EndList) {
		return nil
	}
	present := make(map[int64]bool, len(pl.Segments))
	for _, seg := range pl.Segments {
		present[seg.MediaSeq] = true
	}
	var expired []int64
	for seq := range p.RefetchSeqs {
		if !present[seq] && (seq < pl.MediaSequenceBase || pl.EndList) {
			expired = append(expired, seq)
		}
	}
	slices.Sort(expired)
	return expired
}

// midStreamRollRange reports [lastSeq+1, headSeq-1] when a refreshed playlist
// has jumped past the next contiguous segment.
func midStreamRollRange(headSeq, lastSeq int64) (from, to int64, ok bool) {
	if headSeq > lastSeq+1 {
		return lastSeq + 1, headSeq - 1, true
	}
	return 0, 0, false
}

// fetchAndParse resolves all media and initialization URIs against the playlist URL.
// Playlist authorization and disappearance retain their sentinel errors.
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
		// Subscriber and geographic restrictions cannot be fixed by token renewal.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		if p.ClassifyAuth != nil && p.ClassifyAuth(resp.StatusCode, body) {
			return nil, fmt.Errorf("%w: status %d: %s", ErrPlaylistAuthPermanent, resp.StatusCode, truncateForLog(body))
		}
		return nil, fmt.Errorf("%w: status %d", ErrPlaylistAuth, resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		// A dropped rendition requires reselection, even if its playback token is still valid.
		return nil, fmt.Errorf("%w: status %d", ErrPlaylistGone, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Bound error previews because CDN failures can return large HTML bodies.
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

// truncateForLog bounds authorization-body previews to keep CDN error pages out of logs.
func truncateForLog(b []byte) string {
	const limit = 200
	if len(b) <= limit {
		return string(b)
	}
	return string(b[:limit]) + "…"
}

// resolveURIs replaces relative media and initialization URIs in place;
// HLS permits both absolute URLs and references relative to the playlist.
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

func segmentExt(k SegmentKind) string {
	if k == SegmentKindFMP4 {
		return ".m4s"
	}
	return ".ts"
}

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

// isCanceled also treats request failures after context cancellation as terminal.
func isCanceled(ctx context.Context, err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return ctx.Err() != nil
}
