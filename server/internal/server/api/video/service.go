package video

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

var errVideoNotBookmarkable = errors.New("video: recording cannot be saved")

func New(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log.With("domain", "video")}
}

func (s *Service) List(ctx context.Context, opts repository.ListVideosOpts) ([]repository.Video, error) {
	return s.repo.ListVideos(ctx, opts)
}

func (s *Service) ListPage(ctx context.Context, opts repository.ListVideosOpts, cursor *repository.VideoListPageCursor) (*repository.VideoListPage, error) {
	return s.repo.ListVideosPage(ctx, opts, cursor)
}

func (s *Service) Search(ctx context.Context, query string, limit int) ([]repository.Video, error) {
	return s.repo.SearchVideos(ctx, query, limit)
}

func (s *Service) GetByID(ctx context.Context, id int64) (*repository.Video, error) {
	return s.repo.GetVideo(ctx, id)
}

func (s *Service) ListByBroadcaster(ctx context.Context, broadcasterID string, limit int, cursor *repository.VideoPageCursor) (*repository.VideoPage, error) {
	return s.repo.ListVideosByBroadcaster(ctx, broadcasterID, limit, cursor)
}

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
func (s *Service) Stats(ctx context.Context, userID string) (*Statistics, error) {
	totals, err := s.repo.VideoStatsTotals(ctx, userID)
	if err != nil {
		return nil, err
	}
	buckets, err := s.repo.VideoStatsByStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &Statistics{Totals: totals, ByStatus: buckets}, nil
}

func (s *Service) StatsByBroadcaster(ctx context.Context, broadcasterID string) (*repository.VideoStatsTotals, error) {
	return s.repo.VideoStatsTotalsByBroadcaster(ctx, broadcasterID)
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

// Timeline returns the merged title + category change events for a
// video, in chronological order. Each row is one channel.update
// observation; title or category may be nil when only one dimension
// was carried by the originating event. Empty result for recordings
// predating migration 031.
func (s *Service) Timeline(ctx context.Context, videoID int64) ([]repository.VideoMetadataChange, error) {
	return s.repo.ListVideoMetadataChanges(ctx, videoID)
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

// PartsForVideos batches part lookups for a set of videos into one query,
// grouped by video ID. Used by the active-downloads snapshot so it doesn't
// fan out one Parts query per running recording on every dashboard poll.
func (s *Service) PartsForVideos(ctx context.Context, videoIDs []int64) (map[int64][]repository.VideoPart, error) {
	parts, err := s.repo.ListVideoPartsForVideos(ctx, videoIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[int64][]repository.VideoPart, len(videoIDs))
	for _, part := range parts {
		out[part.VideoID] = append(out[part.VideoID], part)
	}
	return out, nil
}

func (s *Service) PlaybackAsset(ctx context.Context, videoID int64) (*repository.VideoPlaybackAsset, error) {
	return s.repo.GetVideoPlaybackAsset(ctx, videoID)
}

func (s *Service) UserState(ctx context.Context, userID string, videoID int64) (*repository.VideoUserState, error) {
	return s.repo.GetVideoUserState(ctx, userID, videoID)
}

func (s *Service) UserStatesByVideoID(ctx context.Context, userID string, videos []repository.Video) map[int64]*repository.VideoUserState {
	out := make(map[int64]*repository.VideoUserState)
	if userID == "" || len(videos) == 0 {
		return out
	}
	ids := make([]int64, 0, len(videos))
	seen := make(map[int64]struct{}, len(videos))
	for _, v := range videos {
		if _, dup := seen[v.ID]; dup {
			continue
		}
		seen[v.ID] = struct{}{}
		ids = append(ids, v.ID)
	}
	rows, err := s.repo.ListVideoUserStatesForVideos(ctx, userID, ids)
	if err != nil {
		s.log.Warn("resolve video user states", "error", err)
		return out
	}
	for i := range rows {
		out[rows[i].VideoID] = &rows[i]
	}
	return out
}

func (s *Service) SetWatchLater(ctx context.Context, userID string, videoID int64, watchLater bool) (*repository.VideoUserState, error) {
	if err := s.requireBookmarkableVideo(ctx, videoID); err != nil {
		return nil, err
	}
	return s.repo.SetVideoWatchLater(ctx, userID, videoID, watchLater)
}

func (s *Service) UpdateWatchProgress(ctx context.Context, userID string, videoID int64, positionSeconds float64, completed bool, observedAtMs int64) (*repository.VideoUserState, error) {
	return s.repo.UpdateVideoWatchProgress(ctx, userID, videoID, positionSeconds, completed, observedAtMs)
}

func (s *Service) requireBookmarkableVideo(ctx context.Context, videoID int64) error {
	v, err := s.repo.GetVideo(ctx, videoID)
	if err != nil {
		return err
	}
	if v == nil {
		return repository.ErrNotFound
	}
	if v.DeletedAt != nil {
		return errVideoNotBookmarkable
	}
	return nil
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
// (recording predates snapshot capture, storage had no captures, or
// recording shorter than one tick).
func (s *Service) ListSnapshots(ctx context.Context, store storage.Storage, videoID int64) ([]string, error) {
	if store == nil {
		return nil, nil
	}
	v, err := s.repo.GetVideo(ctx, videoID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, 24)
	for i := range maxSnapshotsPerVideo {
		path := storagekeys.Snapshot(v.Filename, i)
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
