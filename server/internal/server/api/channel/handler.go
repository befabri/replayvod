package channel

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/trpcgo"
)

// Handler is the tRPC adapter for the channel domain.
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler wires a handler around a channel Service.
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log.With("domain", "channel-api")}
}

// ChannelResponse is the wire shape for a channel.
type ChannelResponse struct {
	BroadcasterID       string    `json:"broadcaster_id"`
	BroadcasterLogin    string    `json:"broadcaster_login"`
	BroadcasterName     string    `json:"broadcaster_name"`
	BroadcasterLanguage *string   `json:"broadcaster_language,omitempty"`
	ProfileImageURL     *string   `json:"profile_image_url,omitempty"`
	OfflineImageURL     *string   `json:"offline_image_url,omitempty"`
	Description         *string   `json:"description,omitempty"`
	BroadcasterType     *string   `json:"broadcaster_type,omitempty"`
	ViewCount           int64     `json:"view_count"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func toResponse(c *repository.Channel) ChannelResponse {
	return ChannelResponse{
		BroadcasterID:       c.BroadcasterID,
		BroadcasterLogin:    c.BroadcasterLogin,
		BroadcasterName:     c.BroadcasterName,
		BroadcasterLanguage: c.BroadcasterLanguage,
		ProfileImageURL:     c.ProfileImageURL,
		OfflineImageURL:     c.OfflineImageURL,
		Description:         c.Description,
		BroadcasterType:     c.BroadcasterType,
		ViewCount:           c.ViewCount,
		CreatedAt:           c.CreatedAt,
		UpdatedAt:           c.UpdatedAt,
	}
}

type GetByIDInput struct {
	BroadcasterID string `json:"broadcaster_id" validate:"required"`
}

func (h *Handler) GetByID(ctx context.Context, input GetByIDInput) (ChannelResponse, error) {
	c, err := h.svc.GetByID(ctx, input.BroadcasterID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ChannelResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "channel not found")
		}
		h.log.Error("get channel", "error", err)
		return ChannelResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to get channel")
	}
	return toResponse(c), nil
}

type GetByLoginInput struct {
	Login string `json:"login" validate:"required"`
}

func (h *Handler) GetByLogin(ctx context.Context, input GetByLoginInput) (ChannelResponse, error) {
	c, err := h.svc.GetByLogin(ctx, input.Login)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ChannelResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "channel not found")
		}
		h.log.Error("get channel by login", "error", err)
		return ChannelResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to get channel")
	}
	return toResponse(c), nil
}

func (h *Handler) List(ctx context.Context) ([]ChannelResponse, error) {
	channels, err := h.svc.List(ctx)
	if err != nil {
		h.log.Error("list channels", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list channels")
	}
	out := make([]ChannelResponse, len(channels))
	for i := range channels {
		out[i] = toResponse(&channels[i])
	}
	return out, nil
}

func (h *Handler) ListFollowed(ctx context.Context) ([]ChannelResponse, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	channels, err := h.svc.ListFollowedByUser(ctx, user.ID)
	if err != nil {
		h.log.Error("list followed channels", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list followed channels")
	}
	out := make([]ChannelResponse, len(channels))
	for i := range channels {
		out[i] = toResponse(&channels[i])
	}
	return out, nil
}

type SyncFromTwitchInput struct {
	BroadcasterID string `json:"broadcaster_id" validate:"required"`
}

// SyncFromTwitch uses the caller's user access token so rate-limit +
// fetch-log attribution stays accurate.
func (h *Handler) SyncFromTwitch(ctx context.Context, input SyncFromTwitchInput) (ChannelResponse, error) {
	tokens := middleware.GetTokens(ctx)
	user := middleware.GetUser(ctx)
	if tokens == nil || user == nil {
		return ChannelResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	c, err := h.svc.SyncFromTwitch(ctx, SyncInput{
		BroadcasterID:   input.BroadcasterID,
		UserID:          user.ID,
		UserAccessToken: tokens.AccessToken,
	})
	if err != nil {
		h.log.Error("sync channel", "error", err)
		return ChannelResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to sync channel")
	}
	return toResponse(c), nil
}
