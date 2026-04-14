// Package category owns the category domain: business logic (Service)
// and the tRPC adapter (Handler). Categories are populated by stream
// enrichment on stream.online; this surface is read-only from the UI's
// perspective.
package category

import (
	"context"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
)

// Service is the category domain service.
type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

// New builds the service.
func New(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log.With("domain", "category")}
}

// GetByID returns a category by Twitch game_id, or ErrNotFound.
func (s *Service) GetByID(ctx context.Context, id string) (*repository.Category, error) {
	return s.repo.GetCategory(ctx, id)
}

// List returns every mirrored category ordered by the repo's list query.
func (s *Service) List(ctx context.Context) ([]repository.Category, error) {
	return s.repo.ListCategories(ctx)
}

// Search returns categories matching query (empty matches everything),
// ranked by match quality and capped at limit. Mirrors channel.Service.Search
// so the schedule form's category picker shares ranking semantics with
// the broadcaster picker.
func (s *Service) Search(ctx context.Context, query string, limit int) ([]repository.Category, error) {
	return s.repo.SearchCategories(ctx, query, limit)
}
