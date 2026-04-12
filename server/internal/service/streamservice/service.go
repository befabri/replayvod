// Package streamservice owns stream-domain reads. Writes happen via
// EventSub webhooks (stream.online / stream.offline) and the
// scheduled viewer-count poller; this read-only surface is what the
// dashboard + public API share.
package streamservice

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
	return &Service{
		repo: repo,
		log:  log.With("domain", "stream"),
	}
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
