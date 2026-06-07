package tag

import (
	"context"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/server/api/apierr"
)

type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log.With("domain", "tag-api")}
}

type TagResponse struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func (h *Handler) List(ctx context.Context) ([]TagResponse, error) {
	rows, err := h.svc.List(ctx)
	if err != nil {
		return nil, apierr.Map(h.log, err, "list tags")
	}
	out := make([]TagResponse, len(rows))
	for i, r := range rows {
		out[i] = TagResponse{ID: r.ID, Name: r.Name, CreatedAt: r.CreatedAt}
	}
	return out, nil
}
