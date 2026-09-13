// Package streammeta enriches live broadcasts and records opening and changing metadata.
package streammeta

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"time"

	"github.com/befabri/replayvod/server/internal/ptr"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// DefaultRetries and DefaultRetryDelay bound retries while Helix catches up with stream.online.
const (
	DefaultRetries    = 3
	DefaultRetryDelay = time.Second

	// defaultEnrichTimeout leaves room for stream retries within Twitch's webhook response deadline.
	defaultEnrichTimeout = 2 * time.Second

	// categoryGameMetadataRetryInterval mirrors categoryart's retry window for
	// categories Twitch returns without a complete /games metadata payload.
	categoryGameMetadataRetryInterval = 7 * 24 * time.Hour
)

// Snapshot contains observed broadcast metadata even when optional enrichment fails.
// Zero field values mean no value was observed; a nil snapshot means no stream was found.
type Snapshot struct {
	// StreamID is the observed broadcast identity. Admission establishes its
	// minimal database linkage atomically even when optional enrichment fails.
	StreamID string

	Title string

	Language    string
	ViewerCount int64
	GameID      string
	GameName    string

	// TagIDs contains only successfully persisted tags; category identity survives enrichment
	// failures.
	CategoryIDs []string
	TagIDs      []int64

	StartedAt time.Time
}

// CategoryArtEnricher fetches missing category artwork and game metadata.
type CategoryArtEnricher interface {
	Enrich(ctx context.Context, categoryID string) error
}

// RecordingMediaOffsetResolver provides an active recording's playback position when exact.
type RecordingMediaOffsetResolver interface {
	ResolveMediaOffsetSeconds(ctx context.Context, broadcasterID string, videoID int64) (float64, bool)
}

type streamFetcher interface {
	GetStreams(ctx context.Context, params *twitch.GetStreamsParams) ([]twitch.Stream, twitch.Pagination, error)
}

// Hydrator enriches live-stream metadata and can be shared across recordings.
type Hydrator struct {
	repo    repository.Repository
	twitch  streamFetcher
	art     CategoryArtEnricher
	offsets RecordingMediaOffsetResolver
	log     *slog.Logger
	retries int
	delay   time.Duration
	now     func() time.Time
}

// Config sets enrichment retry limits and optional observers; zero values use defaults.
type Config struct {
	// Retries defaults to three when nonpositive.
	Retries int

	// RetryDelay defaults to one second when nonpositive.
	RetryDelay time.Duration

	// CategoryArt may be nil to defer missing artwork and metadata to scheduled backfill.
	CategoryArt CategoryArtEnricher

	// MediaOffsets, when set, provides exact media-time offsets for
	// metadata writes that arrive through push/webhook paths.
	MediaOffsets RecordingMediaOffsetResolver
}

// NewHydrator creates a metadata hydrator; a nil Twitch client disables live lookups.
func NewHydrator(repo repository.Repository, tc *twitch.Client, cfg Config, log *slog.Logger) *Hydrator {
	retries := cfg.Retries
	if retries <= 0 {
		retries = DefaultRetries
	}
	delay := cfg.RetryDelay
	if delay <= 0 {
		delay = DefaultRetryDelay
	}
	var fetcher streamFetcher
	if tc != nil {
		fetcher = tc
	}
	return &Hydrator{
		repo:    repo,
		twitch:  fetcher,
		art:     nilSafeInterface(cfg.CategoryArt),
		offsets: nilSafeInterface(cfg.MediaOffsets),
		log:     log.With("domain", "streammeta"),
		retries: retries,
		delay:   delay,
		now:     time.Now,
	}
}

// SetMediaOffsetResolver connects playback positions to metadata observations.
// Call it before starting metadata observers.
func (h *Hydrator) SetMediaOffsetResolver(resolver RecordingMediaOffsetResolver) {
	h.offsets = nilSafeInterface(resolver)
}

// nilSafeInterface prevents optional typed-nil dependencies from passing nil guards.
func nilSafeInterface[T any](v T) T {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Chan, reflect.Func, reflect.Map, reflect.Slice:
		if rv.IsNil() {
			var zero T
			return zero
		}
	}
	return v
}

// Hydrate enriches a broadcaster's live metadata, returning nil when no stream can be fetched.
// Database enrichment failures return a partial snapshot.
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

// HydrateFromStream enriches an existing observation without fetching a potentially newer
// broadcast.
// It returns nil for a nil stream or an empty broadcaster ID.
func (h *Hydrator) HydrateFromStream(ctx context.Context, stream *twitch.Stream) *Snapshot {
	if stream == nil || stream.UserID == "" {
		return nil
	}
	return h.persist(ctx, stream.UserID, stream)
}

func (h *Hydrator) persist(ctx context.Context, broadcasterID string, stream *twitch.Stream) *Snapshot {
	snap := &Snapshot{
		StreamID:    stream.ID,
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
		ThumbnailURL:  ptr.StringOrNil(thumb),
		ViewerCount:   snap.ViewerCount,
		IsMature:      &isMature,
		StartedAt:     stream.StartedAt,
	})
	if err != nil {
		h.log.Warn("upsert stream", "stream_id", stream.ID, "error", err)
	}
	streamSaved := err == nil && streamPtr != nil

	// Category: one per stream, upsert then link. Without the stream
	// row we can't link; we still report the ID so the matcher can
	// match unfiltered + category-filtered schedules on the Helix
	// response alone.
	//
	// Helix /streams returns (game_id, game_name) but not box_art_url or
	// igdb_id — so a newly-observed category lands with only its name. When
	// an art enricher is wired, kick off the /helix/games lookup right here
	// so the dashboard card sees box art within the same webhook handler and
	// the IGDB description sync has an id to query. Already-filled categories
	// skip the lookup.
	if stream.GameID != "" {
		cat, err := h.repo.UpsertCategory(ctx, &repository.Category{
			ID:   stream.GameID,
			Name: stream.GameName,
		})
		if err != nil {
			h.log.Warn("upsert category", "game_id", stream.GameID, "error", err)
		} else {
			if streamSaved {
				if err := h.repo.LinkStreamCategory(ctx, snap.StreamID, stream.GameID); err != nil {
					h.log.Warn("link stream category",
						"stream_id", snap.StreamID, "game_id", stream.GameID, "error", err)
				}
			}
			snap.CategoryIDs = append(snap.CategoryIDs, stream.GameID)
			if h.art != nil && categoryNeedsGameMetadata(cat) {
				// Capped timeout so a slow /helix/games can't push the
				// outer stream.online webhook handler over Twitch's
				// budget. Inherits ctx cancellation (client drop,
				// parent timeout) and adds a hard ceiling on top.
				enrichCtx, cancel := context.WithTimeout(ctx, defaultEnrichTimeout)
				if err := h.art.Enrich(enrichCtx, stream.GameID); err != nil {
					h.log.Warn("enrich category game metadata",
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
		if streamSaved {
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
		} else if streamSaved {
			if err := h.repo.LinkStreamTitle(ctx, snap.StreamID, title.ID); err != nil {
				h.log.Warn("link stream title",
					"stream_id", snap.StreamID, "title_id", title.ID, "error", err)
			}
		}
	}

	return snap
}

func categoryNeedsGameMetadata(cat *repository.Category) bool {
	if cat == nil {
		return true
	}
	missing := cat.BoxArtURL == nil || *cat.BoxArtURL == "" || cat.IGDBID == nil || *cat.IGDBID == ""
	if !missing {
		return false
	}
	return cat.GameMetadataCheckedAt == nil || cat.GameMetadataCheckedAt.Before(time.Now().Add(-categoryGameMetadataRetryInterval))
}

// fetchWithRetry retries empty responses because stream.online can arrive before Helix shows
// the stream.
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

// ChannelUpdateMeta contains observed title and category changes; empty fields leave links
// unchanged.
type ChannelUpdateMeta struct {
	Title              string
	CategoryID         string
	CategoryName       string
	MediaOffsetSeconds *float64
}

// MediaOffsetProvider returns a playback position and reports whether it is exact.
type MediaOffsetProvider interface {
	MediaOffsetSeconds() (float64, bool)
}

func cleanMediaOffset(seconds *float64) *float64 {
	if seconds == nil || *seconds < 0 || math.IsNaN(*seconds) || math.IsInf(*seconds, 0) {
		return nil
	}
	value := *seconds
	return &value
}

func (h *Hydrator) recordVideoMetadata(ctx context.Context, claim repository.AttemptClaim, initial bool, meta ChannelUpdateMeta, at time.Time) error {
	result, err := h.repo.RecordVideoMetadataChange(ctx, repository.VideoMetadataChangeInput{
		VideoID:            claim.VideoID,
		JobID:              claim.JobID,
		ExecutionID:        claim.ExecutionID,
		Initial:            initial,
		OccurredAt:         at,
		MediaOffsetSeconds: cleanMediaOffset(meta.MediaOffsetSeconds),
		Title:              meta.Title,
		CategoryID:         meta.CategoryID,
		CategoryName:       meta.CategoryName,
	})
	if err != nil {
		// Late observer writes are harmless after their execution loses capture ownership.
		if errors.Is(err, repository.ErrNoMetadataObserved) || (!initial && errors.Is(err, repository.ErrStaleExecution)) {
			return nil
		}
		return fmt.Errorf("record video metadata change: %w", err)
	}
	// Enrichment runs after the transaction so a slow Helix request cannot roll back the observation.
	if h.art != nil && result != nil && result.Category != nil && categoryNeedsGameMetadata(result.Category) {
		if err := h.art.Enrich(ctx, result.Category.ID); err != nil {
			h.log.Warn("enrich category game metadata",
				"game_id", result.Category.ID, "error", err)
		}
	}
	return nil
}

// RecordChannelUpdate records changes for the broadcaster's current live execution.
// No active capture is a no-op; database errors are returned for webhook retry.
func (h *Hydrator) RecordChannelUpdate(ctx context.Context, broadcasterID string, meta ChannelUpdateMeta) error {
	if broadcasterID == "" || (meta.Title == "" && meta.CategoryID == "") {
		return nil
	}
	// A late channel.update can arrive after capture ends or before unsubscribe finishes.
	job, err := h.repo.GetActiveLiveJobByBroadcaster(ctx, broadcasterID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return err
	}
	if meta.MediaOffsetSeconds == nil && h.offsets != nil {
		if seconds, ok := h.offsets.ResolveMediaOffsetSeconds(ctx, broadcasterID, job.VideoID); ok {
			meta.MediaOffsetSeconds = &seconds
		}
	}
	return h.recordVideoMetadata(ctx, repository.AttemptClaim{VideoID: job.VideoID, JobID: job.ID, ExecutionID: job.ExecutionID}, false, meta, h.now().UTC())
}

// LinkInitialVideoMetadata records opening title and category observations before capture starts.
// Repeated calls are safe; a late call cannot reopen running metadata spans.
func (h *Hydrator) LinkInitialVideoMetadata(ctx context.Context, videoID int64, meta ChannelUpdateMeta) error {
	if videoID == 0 || (meta.Title == "" && meta.CategoryID == "") {
		return nil
	}
	v, err := h.repo.GetVideo(ctx, videoID)
	if err != nil {
		return err
	}
	return h.recordVideoMetadata(ctx, repository.AttemptClaim{VideoID: videoID, JobID: v.JobID}, true, meta, h.now().UTC())
}

// CurrentStream returns the current broadcast, or nil without error when the broadcaster is
// offline.
// Lookup failures remain errors so recovery cannot mistake an outage for a broadcast ending.
func (h *Hydrator) CurrentStream(ctx context.Context, broadcasterID string) (*twitch.Stream, error) {
	if h.twitch == nil || broadcasterID == "" {
		return nil, errors.New("stream identity lookup unavailable")
	}
	streams, _, err := h.twitch.GetStreams(ctx, &twitch.GetStreamsParams{UserID: []string{broadcasterID}, First: 1})
	if err != nil {
		return nil, err
	}
	if len(streams) == 0 {
		return nil, nil
	}
	return &streams[0], nil
}
