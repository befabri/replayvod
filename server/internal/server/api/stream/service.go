// Package stream owns the stream domain. Writes happen via EventSub
// webhooks (stream.online / stream.offline) and the scheduled
// viewer-count poller; this read-only surface is what the dashboard +
// public API share.
package stream

import (
	"context"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
)

// Service is the stream domain service.
type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

// New builds the service.
func New(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log.With("domain", "stream")}
}

// ListActive returns every currently-live stream (ended_at IS NULL).
func (s *Service) ListActive(ctx context.Context) ([]repository.Stream, error) {
	return s.repo.ListActiveStreams(ctx)
}

// ListByBroadcaster returns a broadcaster's stream history, paginated.
func (s *Service) ListByBroadcaster(ctx context.Context, broadcasterID string, limit, offset int) ([]repository.Stream, error) {
	return s.repo.ListStreamsByBroadcaster(ctx, broadcasterID, limit, offset)
}

// GetLastLive returns the most recent stream (active or ended), or
// repository.ErrNotFound if the broadcaster has no stream history.
func (s *Service) GetLastLive(ctx context.Context, broadcasterID string) (*repository.Stream, error) {
	return s.repo.GetLastLiveStream(ctx, broadcasterID)
}
