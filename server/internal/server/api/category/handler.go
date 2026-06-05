package category

import (
	"context"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/apierr"
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

func toResponses(rows []repository.Category) []CategoryResponse {
	out := make([]CategoryResponse, len(rows))
	for i := range rows {
		out[i] = toResponse(&rows[i])
	}
	return out
}

type GetByIDInput struct {
	ID string `json:"id" validate:"required"`
}

func (h *Handler) GetByID(ctx context.Context, input GetByIDInput) (CategoryResponse, error) {
	c, err := h.svc.GetByID(ctx, input.ID)
	if err != nil {
		return CategoryResponse{}, apierr.Map(h.log, err, "get category")
	}
	return toResponse(c), nil
}

func (h *Handler) List(ctx context.Context) ([]CategoryResponse, error) {
	rows, err := h.svc.List(ctx)
	if err != nil {
		return nil, apierr.Map(h.log, err, "list categories")
	}
	return toResponses(rows), nil
}

func (h *Handler) ListWithVideos(ctx context.Context) ([]CategoryResponse, error) {
	rows, err := h.svc.ListWithVideos(ctx)
	if err != nil {
		return nil, apierr.Map(h.log, err, "list categories with videos")
	}
	return toResponses(rows), nil
}

// SearchInput drives category.search. Empty Query returns everything
// up to Limit — the same endpoint backs the combobox "show all"
// state. Query is capped at 100 chars to bound substring pattern work;
// plenty of headroom for any realistic game title.
type SearchInput struct {
	Query string `json:"query" validate:"max=100"`
	Limit int    `json:"limit,omitempty" validate:"min=0,max=200"`
}

func (h *Handler) Search(ctx context.Context, input SearchInput) ([]CategoryResponse, error) {
	rows, err := h.svc.Search(ctx, input.Query, input.Limit)
	if err != nil {
		return nil, apierr.Map(h.log, err, "search categories")
	}
	return toResponses(rows), nil
}

func (h *Handler) SearchWithVideos(ctx context.Context, input SearchInput) ([]CategoryResponse, error) {
	rows, err := h.svc.SearchWithVideos(ctx, input.Query, input.Limit)
	if err != nil {
		return nil, apierr.Map(h.log, err, "search categories with videos")
	}
	return toResponses(rows), nil
}
