// Package stream exposes tRPC procedures for Twitch broadcast session data.
// These are read-mostly — writes happen via EventSub webhooks (Phase 5) and
// the scheduler's viewer-count poller.
package stream

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// Service handles tRPC stream procedures.
type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

// NewService creates the stream tRPC service.
func NewService(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With("domain", "stream"),
	}
}

// StreamResponse is the wire shape for a stream record.
type StreamResponse struct {
	ID            string     `json:"id"`
	BroadcasterID string     `json:"broadcaster_id"`
	Type          string     `json:"type"`
	Language      string     `json:"language"`
	ThumbnailURL  *string    `json:"thumbnail_url,omitempty"`
	ViewerCount   int64      `json:"viewer_count"`
	IsMature      *bool      `json:"is_mature,omitempty"`
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
}

func toResponse(s *repository.Stream) StreamResponse {
	return StreamResponse{
		ID:            s.ID,
		BroadcasterID: s.BroadcasterID,
		Type:          s.Type,
		Language:      s.Language,
		ThumbnailURL:  s.ThumbnailURL,
		ViewerCount:   s.ViewerCount,
		IsMature:      s.IsMature,
		StartedAt:     s.StartedAt,
		EndedAt:       s.EndedAt,
	}
}

// Active returns all currently-live streams (ended_at IS NULL).
func (s *Service) Active(ctx context.Context) ([]StreamResponse, error) {
	streams, err := s.repo.ListActiveStreams(ctx)
	if err != nil {
		s.log.Error("list active streams failed", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list active streams")
	}
	out := make([]StreamResponse, len(streams))
	for i := range streams {
		out[i] = toResponse(&streams[i])
	}
	return out, nil
}

// ByBroadcasterInput paginates past streams for a broadcaster.
type ByBroadcasterInput struct {
	BroadcasterID string `json:"broadcaster_id" validate:"required"`
	Limit         int    `json:"limit" validate:"min=0,max=100"`
	Offset        int    `json:"offset" validate:"min=0"`
}

// ByBroadcaster returns a broadcaster's stream history.
func (s *Service) ByBroadcaster(ctx context.Context, input ByBroadcasterInput) ([]StreamResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 25
	}
	streams, err := s.repo.ListStreamsByBroadcaster(ctx, input.BroadcasterID, limit, input.Offset)
	if err != nil {
		s.log.Error("list streams by broadcaster failed", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list streams")
	}
	out := make([]StreamResponse, len(streams))
	for i := range streams {
		out[i] = toResponse(&streams[i])
	}
	return out, nil
}

// LastLiveInput picks the most recent stream for a broadcaster.
type LastLiveInput struct {
	BroadcasterID string `json:"broadcaster_id" validate:"required"`
}

// LastLive returns the most recent stream (active or ended). 404 if none exist.
func (s *Service) LastLive(ctx context.Context, input LastLiveInput) (StreamResponse, error) {
	stream, err := s.repo.GetLastLiveStream(ctx, input.BroadcasterID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return StreamResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "no streams for broadcaster")
		}
		s.log.Error("get last live stream failed", "error", err)
		return StreamResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load stream")
	}
	return toResponse(stream), nil
}
