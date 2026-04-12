// Package tagservice owns tag-domain reads. Tags are populated by
// stream enrichment on stream.online; this service is read-only from
// the UI's perspective.
package tagservice

import (
	"context"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
)

// Service handles tag business logic.
type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

// New builds the service.
func New(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With("domain", "tag"),
	}
}

// List returns every tag ordered by the repo's list query.
func (s *Service) List(ctx context.Context) ([]repository.Tag, error) {
	return s.repo.ListTags(ctx)
}
