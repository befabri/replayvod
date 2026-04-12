package category

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/service/categoryservice"
	"github.com/befabri/trpcgo"
)

// Service is the tRPC-transport wrapper around categoryservice.
type Service struct {
	svc *categoryservice.Service
	log *slog.Logger
}

func NewService(svc *categoryservice.Service, log *slog.Logger) *Service {
	return &Service{svc: svc, log: log.With("domain", "category-api")}
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

type GetByIDInput struct {
	ID string `json:"id" validate:"required"`
}

func (s *Service) GetByID(ctx context.Context, input GetByIDInput) (CategoryResponse, error) {
	c, err := s.svc.GetByID(ctx, input.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return CategoryResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "category not found")
		}
		s.log.Error("get category", "error", err)
		return CategoryResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to get category")
	}
	return toResponse(c), nil
}

func (s *Service) List(ctx context.Context) ([]CategoryResponse, error) {
	rows, err := s.svc.List(ctx)
	if err != nil {
		s.log.Error("list categories", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list categories")
	}
	out := make([]CategoryResponse, len(rows))
	for i := range rows {
		out[i] = toResponse(&rows[i])
	}
	return out, nil
}
