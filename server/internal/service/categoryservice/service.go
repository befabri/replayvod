// Package categoryservice owns category-domain reads. Categories are
// populated by stream enrichment on stream.online; this service is
// read-only from the UI's perspective.
package categoryservice

import (
	"context"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
)

// Service handles category business logic.
type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

// New builds the service.
func New(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With("domain", "category"),
	}
}

// GetByID returns a category by Twitch game_id, or ErrNotFound.
func (s *Service) GetByID(ctx context.Context, id string) (*repository.Category, error) {
	return s.repo.GetCategory(ctx, id)
}

// List returns every mirrored category ordered by the repo's list query.
func (s *Service) List(ctx context.Context) ([]repository.Category, error) {
	return s.repo.ListCategories(ctx)
}
