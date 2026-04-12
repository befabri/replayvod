// Package videorequest is the tRPC-transport wrapper around
// videorequestservice.
package videorequest

import (
	"context"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/service/videorequestservice"
	"github.com/befabri/trpcgo"
)

type Service struct {
	svc *videorequestservice.Service
	log *slog.Logger
}

func NewService(svc *videorequestservice.Service, log *slog.Logger) *Service {
	return &Service{svc: svc, log: log.With("domain", "videorequest-api")}
}

type ListInput struct {
	Limit  int `json:"limit" validate:"min=0,max=200"`
	Offset int `json:"offset" validate:"min=0"`
}

// VideoSummary is a trimmed video view for the request list.
type VideoSummary struct {
	ID          int64  `json:"id"`
	Filename    string `json:"filename"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
}

func (s *Service) Mine(ctx context.Context, input ListInput) ([]VideoSummary, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	videos, err := s.svc.ListForUser(ctx, user.ID, limit, input.Offset)
	if err != nil {
		s.log.Error("list video requests", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list requests")
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

func (s *Service) Request(ctx context.Context, input RequestInput) (OK, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return OK{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	if err := s.svc.Request(ctx, user.ID, input.VideoID); err != nil {
		s.log.Error("add video request", "error", err)
		return OK{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to request video")
	}
	return OK{OK: true}, nil
}
