package stream

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// Handler is the tRPC adapter for the stream domain.
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler wires a handler around a stream Service.
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log.With("domain", "stream-api")}
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

func (h *Handler) Active(ctx context.Context) ([]StreamResponse, error) {
	streams, err := h.svc.ListActive(ctx)
	if err != nil {
		h.log.Error("list active streams", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list active streams")
	}
	out := make([]StreamResponse, len(streams))
	for i := range streams {
		out[i] = toResponse(&streams[i])
	}
	return out, nil
}

type ByBroadcasterInput struct {
	BroadcasterID string `json:"broadcaster_id" validate:"required"`
	Limit         int    `json:"limit" validate:"min=0,max=100"`
	Offset        int    `json:"offset" validate:"min=0"`
}

func (h *Handler) ByBroadcaster(ctx context.Context, input ByBroadcasterInput) ([]StreamResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 25
	}
	streams, err := h.svc.ListByBroadcaster(ctx, input.BroadcasterID, limit, input.Offset)
	if err != nil {
		h.log.Error("list streams by broadcaster", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list streams")
	}
	out := make([]StreamResponse, len(streams))
	for i := range streams {
		out[i] = toResponse(&streams[i])
	}
	return out, nil
}

type LastLiveInput struct {
	BroadcasterID string `json:"broadcaster_id" validate:"required"`
}

func (h *Handler) LastLive(ctx context.Context, input LastLiveInput) (StreamResponse, error) {
	stream, err := h.svc.GetLastLive(ctx, input.BroadcasterID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return StreamResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "no streams for broadcaster")
		}
		h.log.Error("get last live stream", "error", err)
		return StreamResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load stream")
	}
	return toResponse(stream), nil
}
