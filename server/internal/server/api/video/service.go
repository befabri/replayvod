// Package video owns the video domain: metadata reads (Service),
// download control plane (DownloadService), and the HTTP streaming
// handler for playback. The domain co-locates the tRPC handler, the
// Chi byte-range streaming handler, and the two domain services.
package video

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
)

// Service owns video-domain reads: pagination, filtering by
// status/broadcaster/category, and the dashboard home page's
// aggregate statistics.
//
// Writes belong to DownloadService (trigger/cancel) and the webhook
// processor (downloader completion).
type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

// New builds the video read service.
func New(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log.With("domain", "video")}
}

// List returns a paginated page of videos, optionally filtered by
// status and sorted per opts.Sort/Order. Empty values fall back to
// created-desc at the SQL layer.
func (s *Service) List(ctx context.Context, opts repository.ListVideosOpts) ([]repository.Video, error) {
	return s.repo.ListVideos(ctx, opts)
}

// ListPage returns a cursor-paginated page of videos for the main library view.
func (s *Service) ListPage(ctx context.Context, opts repository.ListVideosOpts, cursor *repository.VideoListPageCursor) (*repository.VideoListPage, error) {
	return s.repo.ListVideosPage(ctx, opts, cursor)
}

// GetByID returns a single video row or repository.ErrNotFound.
func (s *Service) GetByID(ctx context.Context, id int64) (*repository.Video, error) {
	return s.repo.GetVideo(ctx, id)
}

// ListByBroadcaster returns a cursor-paginated page of videos for a channel.
func (s *Service) ListByBroadcaster(ctx context.Context, broadcasterID string, limit int, cursor *repository.VideoPageCursor) (*repository.VideoPage, error) {
	return s.repo.ListVideosByBroadcaster(ctx, broadcasterID, limit, cursor)
}

// ListByCategory returns a cursor-paginated page of videos tagged with a category.
func (s *Service) ListByCategory(ctx context.Context, categoryID string, limit int, cursor *repository.VideoPageCursor) (*repository.VideoPage, error) {
	return s.repo.ListVideosByCategory(ctx, categoryID, limit, cursor)
}

// ChannelsByBroadcasterIDs resolves the display metadata the dashboard
// needs to render a video card (login, name, profile_image_url) with
// exactly one repo call, no matter how many videos the caller is about
// to turn into responses. Without this, each card's Avatar triggers a
// separate channel.getById over tRPC — easy to blow past the batching
// middleware's 10-procedure limit on a video grid of 10+ rows.
//
// Returns an empty map on zero input or a DB error (logged) so the
// caller can always range over it safely. Missing broadcasters (the
// user follows a channel that was never synced locally) simply drop
// out of the map; the handler falls back to the Video's own
// DisplayName and nil profile image in that case.
func (s *Service) ChannelsByBroadcasterIDs(ctx context.Context, videos []repository.Video) map[string]*repository.Channel {
	out := make(map[string]*repository.Channel)
	if len(videos) == 0 {
		return out
	}
	seen := make(map[string]struct{}, len(videos))
	ids := make([]string, 0, len(videos))
	for _, v := range videos {
		if v.BroadcasterID == "" {
			continue
		}
		if _, dup := seen[v.BroadcasterID]; dup {
			continue
		}
		seen[v.BroadcasterID] = struct{}{}
		ids = append(ids, v.BroadcasterID)
	}
	if len(ids) == 0 {
		return out
	}
	channels, err := s.repo.ListChannelsByIDs(ctx, ids)
	if err != nil {
		// Best-effort — a channel-mirror hiccup shouldn't fail
		// video.list. Handler falls back to DisplayName for each row.
		s.log.Warn("resolve broadcaster channels for video response", "error", err)
		return out
	}
	for i := range channels {
		out[channels[i].BroadcasterID] = &channels[i]
	}
	return out
}

// PrimaryCategoriesByVideoIDs resolves the longest-held category per
// video in one repo round-trip so list views can show a single stable
// category label without N+1 history queries.
func (s *Service) PrimaryCategoriesByVideoIDs(ctx context.Context, videos []repository.Video) map[int64]*repository.Category {
	out := make(map[int64]*repository.Category)
	if len(videos) == 0 {
		return out
	}
	seen := make(map[int64]struct{}, len(videos))
	ids := make([]int64, 0, len(videos))
	for _, v := range videos {
		if _, dup := seen[v.ID]; dup {
			continue
		}
		seen[v.ID] = struct{}{}
		ids = append(ids, v.ID)
	}
	cats, err := s.repo.ListPrimaryCategoriesForVideos(ctx, ids)
	if err != nil {
		s.log.Warn("resolve primary categories for video response", "error", err)
		return out
	}
	for id, cat := range cats {
		c := cat
		out[id] = &c
	}
	return out
}

// Statistics are the aggregates shown on the dashboard home page.
// One struct instead of two separate lookups so the transport layer
// doesn't branch on "totals OK, buckets failed."
type Statistics struct {
	Totals   *repository.VideoStatsTotals
	ByStatus []repository.VideoStatsByStatus
}

// Stats runs the two aggregate queries together. If either fails the
// caller gets the error — partial aggregates would be misleading.
func (s *Service) Stats(ctx context.Context) (*Statistics, error) {
	totals, err := s.repo.VideoStatsTotals(ctx)
	if err != nil {
		return nil, err
	}
	buckets, err := s.repo.VideoStatsByStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &Statistics{Totals: totals, ByStatus: buckets}, nil
}

// Titles returns the historical title list for a video, ordered by
// the title_id (which is effectively creation order of the
// deduplicated name). Includes the initial at-download-start title
// plus any title changes captured during the recording.
func (s *Service) Titles(ctx context.Context, videoID int64) ([]repository.TitleSpan, error) {
	return s.repo.ListTitlesForVideo(ctx, videoID)
}

// Categories returns the category history for a video — the at-
// download-start category plus any category changes captured via
// channel.update webhooks / title-watcher polls during the recording.
// Ordered by first-seen (category_id ascending, since the junction
// row creation order maps to the ID sequence). Deduped via the
// underlying SELECT DISTINCT.
func (s *Service) Categories(ctx context.Context, videoID int64) ([]repository.CategorySpan, error) {
	return s.repo.ListCategoriesForVideo(ctx, videoID)
}

// Parts returns the video_parts rows for a video, ordered by
// part_index. Single-part recordings return one row; recordings
// that split on a mid-run variant change return 2..N rows.
//
// Empty (not nil) result is also valid for very old recordings
// that predate the video_parts schema; callers treat empty as
// "single conceptual part, fall back to videos.filename".
func (s *Service) Parts(ctx context.Context, videoID int64) ([]repository.VideoPart, error) {
	return s.repo.ListVideoParts(ctx, videoID)
}

// maxSnapshotsPerVideo is the upper bound on the probe-until-404 loop
// in ListSnapshots. Default title-tracking interval of 1-5 min over
// a 24h recording gives 288-1440 snaps; 500 covers the common case
// and caps the pathological one at ~500 Stat() calls per first
// hover-over-card. After the first fetch tanstack-query caches the
// list, so repeated hovers cost zero.
const maxSnapshotsPerVideo = 500

// ListSnapshots returns the storage paths of every live-snapshot
// image saved during the recording, ordered by index (oldest first).
// Used by the VideoCard's hover preview to cycle through the
// time-lapse.
//
// Probe-based: the Snapshotter writes <base>-snapNN.jpg; we probe
// snap00, snap01, ... via storage.Exists until we hit a gap. A
// missed-write mid-recording would stop the probe early, returning
// a shorter list than the actual count — acceptable for a hover
// UX, and rare in practice since snapshot writes are idempotent
// per-index.
//
// Returns an empty slice (no error) for videos with no snapshots
// (audio-only jobs, title_tracking disabled, recording shorter
// than one tick).
func (s *Service) ListSnapshots(ctx context.Context, store storage.Storage, videoID int64) ([]string, error) {
	if store == nil {
		return nil, nil
	}
	v, err := s.repo.GetVideo(ctx, videoID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, 24)
	base := "thumbnails/" + v.Filename + "-snap"
	for i := 0; i < maxSnapshotsPerVideo; i++ {
		path := fmt.Sprintf("%s%02d.jpg", base, i)
		ok, err := store.Exists(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("probe snapshot %s: %w", path, err)
		}
		if !ok {
			break
		}
		out = append(out, path)
	}
	return out, nil
}
