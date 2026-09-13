package video

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

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

// ChannelsByBroadcasterIDs resolves display metadata in one query to avoid
// per-card HTTP requests exceeding the batch limit. Missing channels and
// read failures produce missing map entries; callers fall back to DisplayName.
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
		s.log.Warn("resolve broadcaster channels for video response", "error", err)
		return out
	}
	for i := range channels {
		out[channels[i].BroadcasterID] = &channels[i]
	}
	return out
}

// PrimaryCategoriesByVideoIDs resolves each video's longest-held category in
// one query, avoiding per-video history lookups.
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

type Statistics struct {
	Totals   *repository.VideoStatsTotals
	ByStatus []repository.VideoStatsByStatus
}

// Stats returns an error if either aggregate query fails; partial totals are misleading.
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

type HistoryCount struct {
	OnDisk      int64
	Removed     int64
	Unavailable int64
}

// HistoryCounts tallies terminal outcomes by media scope; All is their sum.
type HistoryCounts struct {
	All       HistoryCount
	Completed HistoryCount
	Failed    HistoryCount
	Cancelled HistoryCount
}

func (s *Service) HistoryCounts(ctx context.Context) (*HistoryCounts, error) {
	buckets, err := s.repo.VideoStatsHistory(ctx)
	if err != nil {
		return nil, err
	}
	out := &HistoryCounts{}
	for _, b := range buckets {
		var outcome *HistoryCount
		switch repository.ClassifyVideoOutcome(b.Status, b.CompletionKind) {
		case repository.VideoOutcomeFailed:
			outcome = &out.Failed
		case repository.VideoOutcomeCancelled:
			outcome = &out.Cancelled
		default:
			outcome = &out.Completed
		}
		for _, c := range []*HistoryCount{&out.All, outcome} {
			switch {
			case !b.Removed:
				c.OnDisk += b.Count
			case b.DeletionKind == repository.DeletionKindMissing:
				c.Removed += b.Count
				c.Unavailable += b.Count
			default:
				c.Removed += b.Count
			}
		}
	}
	return out, nil
}

func (s *Service) StatsByBroadcaster(ctx context.Context, broadcasterID string) (*repository.VideoStatsTotals, error) {
	return s.repo.VideoStatsTotalsByBroadcaster(ctx, broadcasterID)
}

// Titles returns title spans ordered by their start time.
func (s *Service) Titles(ctx context.Context, videoID int64) ([]repository.TitleSpan, error) {
	return s.repo.ListTitlesForVideo(ctx, videoID)
}

// Categories returns category spans ordered by their start time.
func (s *Service) Categories(ctx context.Context, videoID int64) ([]repository.CategorySpan, error) {
	return s.repo.ListCategoriesForVideo(ctx, videoID)
}

// Timeline returns chronological title and category observations. Either
// dimension may be nil when the observation only changed the other.
func (s *Service) Timeline(ctx context.Context, videoID int64) ([]repository.VideoMetadataChange, error) {
	return s.repo.ListVideoMetadataChanges(ctx, videoID)
}

// Parts returns stored references ordered by part_index. An empty result
// means no part is currently available.
func (s *Service) Parts(ctx context.Context, videoID int64) ([]repository.VideoPart, error) {
	return s.repo.ListVideoParts(ctx, videoID)
}

// PartsForVideos reads parts in one query, grouped by video ID.
func (s *Service) PartsForVideos(ctx context.Context, videoIDs []int64) (map[int64][]repository.VideoPart, error) {
	if len(videoIDs) == 0 {
		return nil, nil
	}
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

func (s *Service) UpdateWatchProgress(ctx context.Context, userID string, videoID int64, positionSeconds float64, completed bool) (*repository.VideoUserState, error) {
	return s.repo.UpdateVideoWatchProgress(ctx, userID, videoID, positionSeconds, completed, time.Now())
}

func (s *Service) ContinueWatching(ctx context.Context, userID string, limit int) ([]repository.Video, error) {
	return s.repo.ListContinueWatchingVideos(ctx, userID, limit)
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

// maxSnapshotsPerVideo bounds storage probes per listing; 500 covers a day
// at the default five-minute capture interval.
const maxSnapshotsPerVideo = 500

// ListSnapshots returns existing snapshots in index order. Journaled missing
// uploads are skipped because later frames may exist; an absent unjournaled
// position ends the bounded scan.
func (s *Service) ListSnapshots(ctx context.Context, store storage.Reader, videoID int64) ([]string, error) {
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
			if _, err := s.repo.GetMediaPublication(ctx, path); errors.Is(err, repository.ErrNotFound) {
				break
			} else if err != nil {
				return nil, err
			}
			continue
		}
		out = append(out, path)
	}
	return out, nil
}
