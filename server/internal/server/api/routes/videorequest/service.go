// Package videorequest tracks which users asked for which videos. Simple
// join-table model in Phase 4; Phase 5+ may grow this into a full request
// workflow (pending → approved → downloaded).
package videorequest

import (
	"context"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/trpcgo"
)

// Service handles tRPC videorequest procedures.
type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

// NewService creates the videorequest tRPC service.
func NewService(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With("domain", "videorequest"),
	}
}

// ListInput paginates requests for the current user.
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

// Mine returns the videos the current user has requested.
func (s *Service) Mine(ctx context.Context, input ListInput) ([]VideoSummary, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	videos, err := s.repo.ListVideoRequestsForUser(ctx, user.ID, limit, input.Offset)
	if err != nil {
		s.log.Error("list video requests failed", "error", err)
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

// RequestInput links the current user to a video.
type RequestInput struct {
	VideoID int64 `json:"video_id" validate:"required"`
}

// OK is a minimal ack response.
type OK struct {
	OK bool `json:"ok"`
}

// Request registers the caller as someone who wants this video (used for
// tracking who asked for each download). Idempotent.
func (s *Service) Request(ctx context.Context, input RequestInput) (OK, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return OK{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	if err := s.repo.AddVideoRequest(ctx, input.VideoID, user.ID); err != nil {
		s.log.Error("add video request failed", "error", err)
		return OK{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to request video")
	}
	return OK{OK: true}, nil
}
