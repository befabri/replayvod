// Package video owns the video domain: metadata reads (Service),
// download control plane (DownloadService), and the HTTP streaming
// handler for playback. The domain co-locates the tRPC handler, the
// Chi byte-range streaming handler, and the two domain services.
package video

import (
	"context"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
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
// status. Empty status returns all.
func (s *Service) List(ctx context.Context, status string, limit, offset int) ([]repository.Video, error) {
	if status != "" {
		return s.repo.ListVideosByStatus(ctx, status, limit, offset)
	}
	return s.repo.ListVideos(ctx, limit, offset)
}

// GetByID returns a single video row or repository.ErrNotFound.
func (s *Service) GetByID(ctx context.Context, id int64) (*repository.Video, error) {
	return s.repo.GetVideo(ctx, id)
}

// ListByBroadcaster returns a paginated page of videos for a channel.
func (s *Service) ListByBroadcaster(ctx context.Context, broadcasterID string, limit, offset int) ([]repository.Video, error) {
	return s.repo.ListVideosByBroadcaster(ctx, broadcasterID, limit, offset)
}

// ListByCategory returns a paginated page of videos tagged with a category.
func (s *Service) ListByCategory(ctx context.Context, categoryID string, limit, offset int) ([]repository.Video, error) {
	return s.repo.ListVideosByCategory(ctx, categoryID, limit, offset)
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
