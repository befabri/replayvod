// Package tag owns the tag domain. Tags are populated by stream
// enrichment on stream.online; the UI surface is read-only.
package tag

import (
	"context"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
)

// Service is the tag domain service.
type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

// New builds the service.
func New(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log.With("domain", "tag")}
}

// List returns every tag.
func (s *Service) List(ctx context.Context) ([]repository.Tag, error) {
	return s.repo.ListTags(ctx)
}
