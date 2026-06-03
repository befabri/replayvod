// Package videorequest tracks which users asked for which videos.
// Simple join-table model for now; Phase 5+ may grow this into a full
// request workflow (pending → approved → downloaded).
package videorequest

import (
	"context"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
)

// Service is the video-request domain service.
type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

// New builds the service.
func New(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log.With("domain", "videorequest")}
}

// ListForUser returns the videos the given user has requested.
func (s *Service) ListForUser(ctx context.Context, userID string, limit, offset int) ([]repository.Video, error) {
	return s.repo.ListVideoRequestsForUser(ctx, userID, limit, offset)
}

// Request registers the user as someone who wanted this video.
// Idempotent. A non-existent video_id is reported as ErrNotFound (→ 404)
// rather than letting the AddVideoRequest foreign-key violation surface as a
// generic 500.
func (s *Service) Request(ctx context.Context, userID string, videoID int64) error {
	if _, err := s.repo.GetVideo(ctx, videoID); err != nil {
		return err
	}
	return s.repo.AddVideoRequest(ctx, videoID, userID)
}
