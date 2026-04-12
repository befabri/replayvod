package category

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// Service handles tRPC category procedures.
type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

// NewService creates a new category tRPC service.
func NewService(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With("domain", "category"),
	}
}

// CategoryResponse is the wire shape for a category.
type CategoryResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	BoxArtURL *string   `json:"box_art_url,omitempty"`
	IGDBID    *string   `json:"igdb_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toResponse(c *repository.Category) CategoryResponse {
	return CategoryResponse{
		ID:        c.ID,
		Name:      c.Name,
		BoxArtURL: c.BoxArtURL,
		IGDBID:    c.IGDBID,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

// GetByIDInput for category.getById.
type GetByIDInput struct {
	ID string `json:"id" validate:"required"`
}

// GetByID fetches a category by ID.
func (s *Service) GetByID(ctx context.Context, input GetByIDInput) (CategoryResponse, error) {
	c, err := s.repo.GetCategory(ctx, input.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return CategoryResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "category not found")
		}
		s.log.Error("failed to get category", "error", err)
		return CategoryResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to get category")
	}
	return toResponse(c), nil
}

// List returns all categories.
func (s *Service) List(ctx context.Context) ([]CategoryResponse, error) {
	categories, err := s.repo.ListCategories(ctx)
	if err != nil {
		s.log.Error("failed to list categories", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list categories")
	}
	out := make([]CategoryResponse, len(categories))
	for i, c := range categories {
		out[i] = toResponse(&c)
	}
	return out, nil
}
