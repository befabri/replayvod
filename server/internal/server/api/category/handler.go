package category

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// Handler is the tRPC adapter for the category domain.
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler wires a handler around a category Service.
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log.With("domain", "category-api")}
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

func (h *Handler) GetByID(ctx context.Context, input GetByIDInput) (CategoryResponse, error) {
	c, err := h.svc.GetByID(ctx, input.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return CategoryResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "category not found")
		}
		h.log.Error("get category", "error", err)
		return CategoryResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to get category")
	}
	return toResponse(c), nil
}

func (h *Handler) List(ctx context.Context) ([]CategoryResponse, error) {
	rows, err := h.svc.List(ctx)
	if err != nil {
		h.log.Error("list categories", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list categories")
	}
	out := make([]CategoryResponse, len(rows))
	for i := range rows {
		out[i] = toResponse(&rows[i])
	}
	return out, nil
}
