package stream

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/twitch"
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

// FollowedStreamResponse is the wire shape for one row of
// stream.followed. Most fields come straight from Helix's Stream type;
// ProfileImageURL comes from the local channels mirror so the
// dashboard's avatar render doesn't need a second useChannels() fetch.
// Nil when the broadcaster isn't locally synced — the frontend's
// Avatar falls back to initials in that case.
type FollowedStreamResponse struct {
	StreamID         string    `json:"stream_id"`
	BroadcasterID    string    `json:"broadcaster_id"`
	BroadcasterLogin string    `json:"broadcaster_login"`
	BroadcasterName  string    `json:"broadcaster_name"`
	ProfileImageURL  *string   `json:"profile_image_url,omitempty"`
	GameID           string    `json:"game_id,omitempty"`
	GameName         string    `json:"game_name,omitempty"`
	Type             string    `json:"type"`
	Title            string    `json:"title"`
	Language         string    `json:"language"`
	ViewerCount      int64     `json:"viewer_count"`
	StartedAt        time.Time `json:"started_at"`
	ThumbnailURL     string    `json:"thumbnail_url,omitempty"`
	Tags             []string  `json:"tags,omitempty"`
}

func toFollowedStreamResponse(f *FollowedStream) FollowedStreamResponse {
	s := &f.Stream
	// Normalize Tags to an empty slice so the JSON marshals to [] rather
	// than null when Helix omits the field. Consumers that iterate the
	// array don't need to null-check.
	tags := s.Tags
	if tags == nil {
		tags = []string{}
	}
	return FollowedStreamResponse{
		StreamID:         s.ID,
		BroadcasterID:    s.UserID,
		BroadcasterLogin: s.UserLogin,
		BroadcasterName:  s.UserName,
		ProfileImageURL:  f.ProfileImageURL,
		GameID:           s.GameID,
		GameName:         s.GameName,
		Type:             s.Type,
		Title:            s.Title,
		Language:         s.Language,
		ViewerCount:      int64(s.ViewerCount),
		StartedAt:        s.StartedAt,
		ThumbnailURL:     s.ThumbnailURL,
		Tags:             tags,
	}
}

// Followed returns the caller's followed channels that are currently
// live, sourced from Twitch Helix GET /streams/followed. Requires the
// user:read:follows scope on the session's access token, which is the
// default scope set on OAuth. Side effect: mirrors each stream into
// the local streams table so channel.latestLive accumulates history.
func (h *Handler) Followed(ctx context.Context) ([]FollowedStreamResponse, error) {
	followed, err := h.followedFromSession(ctx, "list followed live streams")
	if err != nil {
		return nil, err
	}
	out := make([]FollowedStreamResponse, len(followed))
	for i := range followed {
		out[i] = toFollowedStreamResponse(&followed[i])
	}
	return out, nil
}

// LiveIds returns just the broadcaster_ids of the caller's followed
// channels that are currently live — the lean cousin of Followed for
// UI paths that only need a membership Set (e.g., a live-dot indicator
// on the channels list). Backed by the same Helix call + mirror side
// effect as Followed; callers should pick one or the other, not both.
func (h *Handler) LiveIds(ctx context.Context) ([]string, error) {
	followed, err := h.followedFromSession(ctx, "list live followed ids")
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(followed))
	for i := range followed {
		ids[i] = followed[i].Stream.UserID
	}
	return ids, nil
}

// followedFromSession pulls session identity + Helix payload. Extracted
// so Followed and LiveIds share the auth check and the error envelope
// exactly — if one changes, both change.
func (h *Handler) followedFromSession(ctx context.Context, logTag string) ([]FollowedStream, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	followed, err := h.svc.Followed(ctx, FollowedInput{
		UserID: user.ID,
	})
	if err != nil {
		if twitch.IsUserAuthError(err) {
			h.log.Warn(logTag, "error", err)
			return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "twitch session expired; sign in again")
		}
		h.log.Error(logTag, "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load live streams")
	}
	return followed, nil
}
