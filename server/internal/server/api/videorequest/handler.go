package videorequest

import (
	"context"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/server/api/apierr"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
)

type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log.With("domain", "videorequest-api")}
}

type ListInput struct {
	Limit  int `json:"limit" validate:"min=0,max=200"`
	Offset int `json:"offset" validate:"min=0"`
}

type VideoSummary struct {
	ID          int64  `json:"id"`
	Filename    string `json:"filename"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
}

func (h *Handler) Mine(ctx context.Context, input ListInput) ([]VideoSummary, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	videos, err := h.svc.ListForUser(ctx, user.ID, limit, input.Offset)
	if err != nil {
		return nil, apierr.Map(h.log, err, "list requests")
	}
	out := make([]VideoSummary, len(videos))
	for i, v := range videos {
		out[i] = VideoSummary{
			ID:          v.ID,
			Filename:    v.Filename,
			DisplayName: v.DisplayName,
			Status:      v.Status,
		}
	}
	return out, nil
}

type RequestInput struct {
	VideoID int64 `json:"video_id" validate:"required"`
}

type OK struct {
	OK bool `json:"ok"`
}

func (h *Handler) Request(ctx context.Context, input RequestInput) (OK, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return OK{}, err
	}
	if err := h.svc.Request(ctx, user.ID, input.VideoID); err != nil {
		return OK{}, apierr.Map(h.log, err, "request video")
	}
	return OK{OK: true}, nil
}
