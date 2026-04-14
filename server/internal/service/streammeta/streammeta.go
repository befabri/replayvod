// Package streammeta is the shared "hydrate live-stream metadata" surface.
// Both the schedule processor (stream.online webhook path) and the manual
// download trigger (tRPC path) need the same thing: take a broadcaster
// ID, call Helix GetStreams, persist the streams / categories / tags /
// titles rows, and hand back a snapshot the caller can reason about.
//
// Before this extraction the schedule processor owned the full enrichment
// flow and manual triggers did nothing. That meant manual downloads had
// empty categories and no title on the library page, even though the
// schema + Helix data carried them. Consolidating here keeps both paths
// consistent and gives the title poller a single hook to reuse during
// recording.
//
// Design notes:
//
//   - Best-effort throughout: Helix failure returns nil without error.
//     The caller decides whether "no snapshot" means "degrade gracefully"
//     (scheduler: still fire unfiltered schedules) or "proceed with empty
//     title" (manual trigger: still start the recording). Neither wants
//     a Helix hiccup to fail the primary flow.
//
//   - Child-row failures (category/tag/title upsert or link) log and
//     continue. The stream row is the critical one — without it FK-linked
//     children can't land. If stream upsert itself fails we still return a
//     partial Snapshot so the caller gets viewer_count / language / title
//     from the live Helix data.
//
//   - Retries live here. Spec calls for 3 attempts with 1s gaps because
//     stream.online races ahead of Helix reflecting the live state by a
//     few hundred ms. Manual triggers rarely hit the race but the retry
//     is harmless there.
package streammeta

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// DefaultRetries + DefaultRetryDelay are the values the schedule processor
// used to carry inline; exposed as defaults so tests can override without
// waiting out full seconds on the retry loop.
const (
	DefaultRetries    = 3
	DefaultRetryDelay = time.Second

	// defaultEnrichTimeout caps the eager category-art fetch.
	// Hydrate runs inside the stream.online webhook handler, which
	// has a ~10s Twitch budget. /streams retries already consume up
	// to 3s (DefaultRetries × DefaultRetryDelay + RTT); capping the
	// /games follow-up at 2s keeps ample headroom. Callers who want
	// a different value construct a context.WithTimeout of their
	// own before calling Hydrate — this constant governs the inner
	// Enrich call only.
	defaultEnrichTimeout = 2 * time.Second
)

// Snapshot is the hydrated view a caller gets back. Zero values for each
// field mean "Twitch didn't return it" — never construe them as "was set
// to zero." A nil *Snapshot means "we didn't hydrate at all" (Helix
// unreachable, channel offline, client not configured).
type Snapshot struct {
	// StreamID is the Twitch stream ID once the streams row is
	// upserted. Empty when the stream row couldn't be persisted;
	// callers relying on FK-safe use should check this before
	// passing StreamID to videos.CreateVideo.
	StreamID string

	// Title is the stream title at the moment of hydration. Empty
	// means Twitch had no title set or the live response came back
	// without one.
	Title string

	// Language + ViewerCount + GameID/GameName come straight from
	// the Helix live response.
	Language    string
	ViewerCount int64
	GameID      string
	GameName    string

	// CategoryIDs + TagIDs are what the schedule matcher reads. A
	// category is always GameID when non-empty; tags are whichever
	// tag names Helix returned, upserted through the titles table
	// and resolved to their numeric IDs.
	CategoryIDs []string
	TagIDs      []int64

	// StartedAt is the stream's start timestamp from Helix.
	StartedAt time.Time
}

// CategoryArtEnricher is the subset of categoryart.Service the
// Hydrator needs. Kept as an interface so tests can fake it and so
// Config's zero-value (nil enricher) is still a valid Hydrator.
type CategoryArtEnricher interface {
	Enrich(ctx context.Context, categoryID string) error
}

// Hydrator fetches + persists live-stream metadata. Shared across
// recording jobs; holds no per-call state.
type Hydrator struct {
	repo     repository.Repository
	twitch   *twitch.Client
	art      CategoryArtEnricher
	log      *slog.Logger
	retries  int
	delay    time.Duration
}

// Config carries the tunables. All fields have zero-value-safe defaults;
// tests override HTTPClient-side via the twitch client.
type Config struct {
	// Retries is how many GetStreams attempts before giving up.
	// Default 3.
	Retries int

	// RetryDelay is the spacing between attempts. Default 1s.
	RetryDelay time.Duration

	// CategoryArt, when set, is called after a category row is
	// observed for the first time (returned row has nil
	// BoxArtURL). Intended for *categoryart.Service; nil disables
	// eager enrichment and leaves the scheduled backfill task as
	// the only filler.
	CategoryArt CategoryArtEnricher
}

// NewHydrator builds the shared hydrator. `tc` may be nil for tests —
// Hydrate returns nil cleanly in that case.
func NewHydrator(repo repository.Repository, tc *twitch.Client, cfg Config, log *slog.Logger) *Hydrator {
	retries := cfg.Retries
	if retries <= 0 {
		retries = DefaultRetries
	}
	delay := cfg.RetryDelay
	if delay <= 0 {
		delay = DefaultRetryDelay
	}
	return &Hydrator{
		repo:    repo,
		twitch:  tc,
		art:     nilSafeEnricher(cfg.CategoryArt),
		log:     log.With("domain", "streammeta"),
		retries: retries,
		delay:   delay,
	}
}

// nilSafeEnricher normalizes a typed-nil *categoryart.Service (or any
// nil concrete pointer behind the interface) to a plain interface-nil.
// Without this, `h.art != nil` in persist would evaluate to true for
// an interface wrapping a nil pointer and then panic on method call.
// The pattern also applies to any other optional interface dep we add
// later — callers that pass a conditionally-constructed service are
// the common offenders.
func nilSafeEnricher(v CategoryArtEnricher) CategoryArtEnricher {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Chan, reflect.Func, reflect.Map, reflect.Slice:
		if rv.IsNil() {
			return nil
		}
	}
	return v
}

// Hydrate runs the full enrichment for one broadcaster. Returns nil
// when Helix can't be reached or the channel isn't live. Never returns
// an error — every failure path is best-effort logged and folded into
// a nil or partial Snapshot.
//
// Side effects on success:
//
//   - streams row upserted (or updated if it already exists from an
//     earlier stream.online for the same stream ID)
//   - category row upserted + stream_categories link created
//   - tag rows upserted (1 per name) + stream_tags links created
//   - title row upserted + stream_titles link created
//
// The title write fills the M2M gap the schema carries but no caller
// previously populated. That unblocks a later multi-title history
// feature without touching this function again.
//
// ctx should be a caller context that can survive client disconnect —
// the schedule processor hands us its persistCtx = context.WithoutCancel;
// manual triggers pass the download ctx directly since it already
// outlives the HTTP request.
func (h *Hydrator) Hydrate(ctx context.Context, broadcasterID string) *Snapshot {
	if h.twitch == nil || broadcasterID == "" {
		return nil
	}
	stream, err := h.fetchWithRetry(ctx, broadcasterID)
	if err != nil {
		h.log.Warn("hydrate: fetch failed; caller degrades",
			"broadcaster_id", broadcasterID, "error", err)
		return nil
	}
	return h.persist(ctx, broadcasterID, stream)
}

// persist upserts the stream row and every linked child row, returning
// the Snapshot. Split from Hydrate so tests can feed a synthetic
// *twitch.Stream without stubbing the Helix client.
func (h *Hydrator) persist(ctx context.Context, broadcasterID string, stream *twitch.Stream) *Snapshot {
	snap := &Snapshot{
		Title:       stream.Title,
		Language:    stream.Language,
		ViewerCount: int64(stream.ViewerCount),
		GameID:      stream.GameID,
		GameName:    stream.GameName,
		StartedAt:   stream.StartedAt,
	}

	isMature := stream.IsMature
	thumb := stream.ThumbnailURL
	streamPtr, err := h.repo.UpsertStream(ctx, &repository.StreamInput{
		ID:            stream.ID,
		BroadcasterID: broadcasterID,
		Type:          stream.Type,
		Language:      stream.Language,
		ThumbnailURL:  stringOrNil(thumb),
		ViewerCount:   snap.ViewerCount,
		IsMature:      &isMature,
		StartedAt:     stream.StartedAt,
	})
	if err != nil {
		h.log.Warn("upsert stream", "stream_id", stream.ID, "error", err)
	} else if streamPtr != nil {
		snap.StreamID = streamPtr.ID
	}

	// Category: one per stream, upsert then link. Without the stream
	// row we can't link; we still report the ID so the matcher can
	// match unfiltered + category-filtered schedules on the Helix
	// response alone.
	//
	// Helix /streams returns (game_id, game_name) but not box_art_url —
	// so a newly-observed category lands with no art. When an art
	// enricher is wired, kick off the /helix/games lookup right here
	// so the dashboard card sees box art within the same webhook
	// handler. Already-filled categories skip the lookup.
	if stream.GameID != "" {
		cat, err := h.repo.UpsertCategory(ctx, &repository.Category{
			ID:   stream.GameID,
			Name: stream.GameName,
		})
		if err != nil {
			h.log.Warn("upsert category", "game_id", stream.GameID, "error", err)
		} else {
			if snap.StreamID != "" {
				if err := h.repo.LinkStreamCategory(ctx, snap.StreamID, stream.GameID); err != nil {
					h.log.Warn("link stream category",
						"stream_id", snap.StreamID, "game_id", stream.GameID, "error", err)
				}
			}
			snap.CategoryIDs = append(snap.CategoryIDs, stream.GameID)
			if h.art != nil && (cat == nil || cat.BoxArtURL == nil || *cat.BoxArtURL == "") {
				// Capped timeout so a slow /helix/games can't push the
				// outer stream.online webhook handler over Twitch's
				// budget. Inherits ctx cancellation (client drop,
				// parent timeout) and adds a hard ceiling on top.
				enrichCtx, cancel := context.WithTimeout(ctx, defaultEnrichTimeout)
				if err := h.art.Enrich(enrichCtx, stream.GameID); err != nil {
					h.log.Warn("enrich category box art",
						"game_id", stream.GameID, "error", err)
				}
				cancel()
			}
		}
	}

	// Tags: Helix returns names; we upsert each, collect IDs, link.
	for _, name := range stream.Tags {
		if name == "" {
			continue
		}
		tag, err := h.repo.UpsertTag(ctx, name)
		if err != nil {
			h.log.Warn("upsert tag", "name", name, "error", err)
			continue
		}
		snap.TagIDs = append(snap.TagIDs, tag.ID)
		if snap.StreamID != "" {
			if err := h.repo.LinkStreamTag(ctx, snap.StreamID, tag.ID); err != nil {
				h.log.Warn("link stream tag",
					"stream_id", snap.StreamID, "tag_id", tag.ID, "error", err)
			}
		}
	}

	// Title: closes the long-standing gap where the titles +
	// stream_titles tables existed but no writer ever populated them.
	// Once the stream_titles edge exists, a per-video link (added by
	// the title poller during recording) can carry the same title_id.
	if stream.Title != "" {
		title, err := h.repo.UpsertTitle(ctx, stream.Title)
		if err != nil {
			h.log.Warn("upsert title", "name", stream.Title, "error", err)
		} else if snap.StreamID != "" {
			if err := h.repo.LinkStreamTitle(ctx, snap.StreamID, title.ID); err != nil {
				h.log.Warn("link stream title",
					"stream_id", snap.StreamID, "title_id", title.ID, "error", err)
			}
		}
	}

	return snap
}

// fetchWithRetry is the 3-attempt loop with 1s pacing. Treats "streams
// array empty" as a retryable miss (the broadcaster legitimately could
// have just gone offline, but stream.online specifically races against
// this — spec § stream.online).
func (h *Hydrator) fetchWithRetry(ctx context.Context, broadcasterID string) (*twitch.Stream, error) {
	var lastErr error
	for attempt := 0; attempt < h.retries; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(h.delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		streams, _, err := h.twitch.GetStreams(ctx, &twitch.GetStreamsParams{
			UserID: []string{broadcasterID},
			First:  1,
		})
		if err != nil {
			lastErr = err
			continue
		}
		if len(streams) > 0 {
			return &streams[0], nil
		}
		lastErr = errors.New("twitch returned no streams for broadcaster")
	}
	if lastErr == nil {
		lastErr = errors.New("hydrate retries exhausted")
	}
	return nil, lastErr
}

func stringOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ChannelUpdateMeta is the payload shape RecordChannelUpdate accepts.
// Mirrors the interesting fields of twitch.ChannelUpdateEvent without
// importing the generated type (this package is upstream of the
// webhook layer). Empty fields are no-ops for their respective links.
type ChannelUpdateMeta struct {
	Title        string
	CategoryID   string
	CategoryName string
}

// linkVideoTitle upserts a title and links it to a video. Idempotent
// via ON CONFLICT DO NOTHING on video_titles; the upsert is harmless
// on already-present title rows. Private helper shared by
// RecordChannelUpdate (webhook + poll path) and LinkInitialVideoMetadata
// (download-trigger path) so both write paths use identical shape.
//
// Kept log-free: callers know whether this is "a new change" vs a
// retry/dedup case and log accordingly. Emitting "title change
// recorded" from here would spam on webhook retries where the DB
// state didn't actually move.
func (h *Hydrator) linkVideoTitle(ctx context.Context, videoID int64, title string) error {
	if title == "" {
		return nil
	}
	t, err := h.repo.UpsertTitle(ctx, title)
	if err != nil {
		return fmt.Errorf("upsert title: %w", err)
	}
	if err := h.repo.LinkVideoTitle(ctx, videoID, t.ID); err != nil {
		return fmt.Errorf("link video title: %w", err)
	}
	return nil
}

// linkVideoCategory upserts a category (when name is provided) and
// links it to a video. Idempotent via ON CONFLICT on both writes.
// Name-empty-guard prevents UpsertCategory's SET name = EXCLUDED.name
// from clobbering an existing good name. Also drives the box-art
// enrichment for first-observation games.
func (h *Hydrator) linkVideoCategory(ctx context.Context, videoID int64, categoryID, categoryName string) error {
	if categoryID == "" {
		return nil
	}
	var cat *repository.Category
	if categoryName != "" {
		var err error
		cat, err = h.repo.UpsertCategory(ctx, &repository.Category{
			ID:   categoryID,
			Name: categoryName,
		})
		if err != nil {
			return fmt.Errorf("upsert category: %w", err)
		}
	}
	if err := h.repo.LinkVideoCategory(ctx, videoID, categoryID); err != nil {
		return fmt.Errorf("link video category: %w", err)
	}
	// Eagerly enrich art for newly-observed categories. The enrich
	// call is best-effort — a failure here doesn't break the link.
	if h.art != nil && (cat == nil || cat.BoxArtURL == nil || *cat.BoxArtURL == "") {
		if err := h.art.Enrich(ctx, categoryID); err != nil {
			h.log.Warn("enrich category box art",
				"game_id", categoryID, "error", err)
		}
	}
	return nil
}

// RecordChannelUpdate links title and/or category changes to whichever
// video is currently being recorded for the given broadcaster. Called
// from the channel.update webhook dispatch (webhook mode) and from
// the metadata watcher (poll mode). Best-effort: a change delivered
// when no recording is active is a no-op; DB errors return so the
// webhook handler can decide whether to NACK for Twitch retry.
//
// Title and category writes are independent: errors.Join lets a
// transient failure on one not mask work done (or worth retrying)
// on the other. Callers inspect the joined error to decide whether
// to retry; Twitch's webhook retry policy is at-least-once so a
// return of nil-or-partial is safer than returning early.
//
// Only video_* links are touched here — the stream-level M2M
// (stream_categories, stream_titles) is intentionally a snapshot
// written once by Hydrate at stream.online. Mid-stream changes
// belong on the per-recording timeline, not the stream row.
func (h *Hydrator) RecordChannelUpdate(ctx context.Context, broadcasterID string, meta ChannelUpdateMeta) error {
	if broadcasterID == "" || (meta.Title == "" && meta.CategoryID == "") {
		return nil
	}
	// Find the active recording. ErrNotFound = no recording in
	// flight; the subscription is presumably a stale orphan (boot
	// reconcile will clean it up) or the ending-recording raced
	// the unsubscribe. Either way, nothing to link.
	job, err := h.repo.GetActiveJobByBroadcaster(ctx, broadcasterID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return err
	}
	return errors.Join(
		h.linkVideoTitle(ctx, job.VideoID, meta.Title),
		h.linkVideoCategory(ctx, job.VideoID, meta.CategoryID, meta.CategoryName),
	)
}

// LinkInitialVideoMetadata is the download-trigger companion to
// RecordChannelUpdate. The downloader just created the video row
// and knows its ID, so we skip the active-job lookup and link
// directly. Same idempotent helpers; callers can retry safely.
//
// The title and category fields are the at-download-start snapshot
// from Hydrator.Hydrate; passing them in here is what makes the
// /dashboard/categories page populate on first hover without
// waiting for a webhook / poll tick.
func (h *Hydrator) LinkInitialVideoMetadata(ctx context.Context, videoID int64, meta ChannelUpdateMeta) error {
	if videoID == 0 || (meta.Title == "" && meta.CategoryID == "") {
		return nil
	}
	return errors.Join(
		h.linkVideoTitle(ctx, videoID, meta.Title),
		h.linkVideoCategory(ctx, videoID, meta.CategoryID, meta.CategoryName),
	)
}

