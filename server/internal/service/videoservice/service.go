// Package videoservice owns video-domain reads: pagination, filtering
// by status/broadcaster/category, and the dashboard home page's
// aggregate statistics.
//
// Writes belong to downloadservice (download trigger/cancel) and
// webhook processors (downloader completion). This package stays
// transport-agnostic so the future public-API service can share it.
package videoservice

import (
	"context"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
)

type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

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

// Statistics runs the two aggregate queries together. If either
// fails the caller gets the error — partial aggregates would be
// misleading in the UI.
func (s *Service) Statistics(ctx context.Context) (*Statistics, error) {
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
