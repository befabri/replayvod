package category

import (
	"context"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/apierr"
	"github.com/befabri/trpcgo"
)

type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log.With("domain", "category-api")}
}

type CategoryResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	BoxArtURL   *string   `json:"box_art_url,omitempty"`
	IGDBID      *string   `json:"igdb_id,omitempty"`
	Description *string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toResponse(c *repository.Category) CategoryResponse {
	return CategoryResponse{
		ID:          c.ID,
		Name:        c.Name,
		BoxArtURL:   c.BoxArtURL,
		IGDBID:      c.IGDBID,
		Description: c.Description,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
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

type CategoryDetailResponse struct {
	CategoryResponse
	VideoCount int64 `json:"video_count"`
	TotalSize  int64 `json:"total_size"`
}

func toDetailResponse(d *repository.CategoryDetail) CategoryDetailResponse {
	return CategoryDetailResponse{
		CategoryResponse: toResponse(&d.Category),
		VideoCount:       d.VideoCount,
		TotalSize:        d.TotalSize,
	}
}

func (h *Handler) GetByID(ctx context.Context, input GetByIDInput) (CategoryResponse, error) {
	c, err := h.svc.GetByID(ctx, input.ID)
	if err != nil {
		return CategoryResponse{}, apierr.Map(h.log, err, "get category")
	}
	return toResponse(c), nil
}

func (h *Handler) GetDetail(ctx context.Context, input GetByIDInput) (CategoryDetailResponse, error) {
	detail, err := h.svc.GetDetail(ctx, input.ID)
	if err != nil {
		return CategoryDetailResponse{}, apierr.Map(h.log, err, "get category detail")
	}
	return toDetailResponse(detail), nil
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

type CategoryPageCursor struct {
	Name          string     `json:"name" validate:"required"`
	ID            string     `json:"id" validate:"required"`
	LatestVideoAt *time.Time `json:"latest_video_at,omitempty"`
	VideoCount    int64      `json:"video_count,omitempty"`
}

type CategoryPageResponse struct {
	Items      []CategoryResponse  `json:"items"`
	NextCursor *CategoryPageCursor `json:"next_cursor,omitempty"`
}

type ListPageInput struct {
	Limit     int                 `json:"limit,omitempty" validate:"min=0,max=200"`
	Sort      string              `json:"sort,omitempty" validate:"omitempty,oneof=name_asc latest_video_desc video_count_desc"`
	Cursor    *CategoryPageCursor `json:"cursor,omitempty" validate:"omitempty"`
	Direction string              `json:"direction,omitempty" validate:"omitempty,oneof=forward backward"`
}

func (h *Handler) ListPage(ctx context.Context, input ListPageInput) (CategoryPageResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 60
	}
	sort := repository.NormalizeCategoryPageSort(input.Sort)
	cursor, err := toRepositoryCategoryPageCursor(input.Cursor, sort)
	if err != nil {
		return CategoryPageResponse{}, trpcgo.NewError(trpcgo.CodeBadRequest, "invalid category list cursor")
	}
	page, err := h.svc.ListPage(ctx, limit, sort, cursor)
	if err != nil {
		return CategoryPageResponse{}, apierr.Map(h.log, err, "list categories page")
	}
	return CategoryPageResponse{
		Items:      toResponses(page.Items),
		NextCursor: toCategoryPageCursor(page.NextCursor),
	}, nil
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

func toRepositoryCategoryPageCursor(cursor *CategoryPageCursor, sort string) (*repository.CategoryPageCursor, error) {
	if cursor == nil {
		return nil, nil
	}
	if cursor.Name == "" || cursor.ID == "" {
		return nil, trpcgo.NewError(trpcgo.CodeBadRequest, "invalid category list cursor")
	}
	switch repository.NormalizeCategoryPageSort(sort) {
	case "latest_video_desc":
		if cursor.LatestVideoAt == nil {
			return nil, trpcgo.NewError(trpcgo.CodeBadRequest, "invalid category list cursor")
		}
	case "video_count_desc":
		if cursor.VideoCount <= 0 {
			return nil, trpcgo.NewError(trpcgo.CodeBadRequest, "invalid category list cursor")
		}
	}
	return &repository.CategoryPageCursor{
		Name:          cursor.Name,
		ID:            cursor.ID,
		LatestVideoAt: cursor.LatestVideoAt,
		VideoCount:    cursor.VideoCount,
	}, nil
}

func toCategoryPageCursor(cursor *repository.CategoryPageCursor) *CategoryPageCursor {
	if cursor == nil {
		return nil
	}
	return &CategoryPageCursor{
		Name:          cursor.Name,
		ID:            cursor.ID,
		LatestVideoAt: cursor.LatestVideoAt,
		VideoCount:    cursor.VideoCount,
	}
}
