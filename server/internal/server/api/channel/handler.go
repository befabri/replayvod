package channel

import (
	"context"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/apierr"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/befabri/trpcgo"
)

type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log.With("domain", "channel-api")}
}

type ChannelResponse struct {
	BroadcasterID       string                    `json:"broadcaster_id"`
	BroadcasterLogin    string                    `json:"broadcaster_login"`
	BroadcasterName     string                    `json:"broadcaster_name"`
	BroadcasterLanguage *string                   `json:"broadcaster_language,omitempty"`
	ProfileImageURL     *string                   `json:"profile_image_url,omitempty"`
	OfflineImageURL     *string                   `json:"offline_image_url,omitempty"`
	Description         *string                   `json:"description,omitempty"`
	BroadcasterType     *string                   `json:"broadcaster_type,omitempty"`
	ViewCount           int64                     `json:"view_count"`
	CreatedAt           time.Time                 `json:"created_at"`
	UpdatedAt           time.Time                 `json:"updated_at"`
	UserState           *ChannelUserStateResponse `json:"user_state,omitempty"`
}

type ChannelUserStateResponse struct {
	Favorite  bool      `json:"favorite"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toChannelUserStateResponse(state *repository.ChannelUserState) *ChannelUserStateResponse {
	if state == nil {
		return nil
	}
	return &ChannelUserStateResponse{
		Favorite:  state.Favorite,
		UpdatedAt: state.UpdatedAt,
	}
}

func toResponse(c *repository.Channel, state *repository.ChannelUserState) ChannelResponse {
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
		UserState:           toChannelUserStateResponse(state),
	}
}

func (h *Handler) toChannelResponses(ctx context.Context, channels []repository.Channel) []ChannelResponse {
	var userID string
	if user := middleware.GetUser(ctx); user != nil {
		userID = user.ID
	}
	states := h.svc.UserStatesByBroadcasterID(ctx, userID, channels)
	out := make([]ChannelResponse, len(channels))
	for i := range channels {
		out[i] = toResponse(&channels[i], states[channels[i].BroadcasterID])
	}
	return out
}

type GetByIDInput struct {
	BroadcasterID string `json:"broadcaster_id" validate:"required"`
}

func (h *Handler) GetByID(ctx context.Context, input GetByIDInput) (ChannelResponse, error) {
	c, err := h.svc.GetByID(ctx, input.BroadcasterID)
	if err != nil {
		return ChannelResponse{}, apierr.Map(h.log, err, "get channel")
	}
	return h.toChannelResponses(ctx, []repository.Channel{*c})[0], nil
}

type GetByLoginInput struct {
	Login string `json:"login" validate:"required"`
}

func (h *Handler) GetByLogin(ctx context.Context, input GetByLoginInput) (ChannelResponse, error) {
	c, err := h.svc.GetByLogin(ctx, input.Login)
	if err != nil {
		return ChannelResponse{}, apierr.Map(h.log, err, "get channel by login")
	}
	return h.toChannelResponses(ctx, []repository.Channel{*c})[0], nil
}

func (h *Handler) List(ctx context.Context) ([]ChannelResponse, error) {
	channels, err := h.svc.List(ctx)
	if err != nil {
		return nil, apierr.Map(h.log, err, "list channels")
	}
	return h.toChannelResponses(ctx, channels), nil
}

type ChannelPageCursor struct {
	BroadcasterName string `json:"broadcaster_name" validate:"required"`
	BroadcasterID   string `json:"broadcaster_id" validate:"required"`
}

type ChannelPageResponse struct {
	Items      []ChannelResponse  `json:"items"`
	NextCursor *ChannelPageCursor `json:"next_cursor,omitempty"`
}

type ListPageInput struct {
	Limit     int                `json:"limit,omitempty" validate:"min=0,max=200"`
	Sort      string             `json:"sort,omitempty" validate:"omitempty,oneof=name_asc name_desc"`
	Filter    string             `json:"filter,omitempty" validate:"omitempty,oneof=all live downloaded favorites"`
	LiveOnly  bool               `json:"live_only,omitempty"`
	Cursor    *ChannelPageCursor `json:"cursor,omitempty" validate:"omitempty"`
	Direction string             `json:"direction,omitempty" validate:"omitempty,oneof=forward backward"`
}

func (h *Handler) ListPage(ctx context.Context, input ListPageInput) (ChannelPageResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 60
	}
	sort := input.Sort
	if sort == "" {
		sort = "name_asc"
	}
	filter := input.Filter
	if filter == "" {
		if input.LiveOnly {
			filter = repository.ChannelFilterLive
		} else {
			filter = repository.ChannelFilterAll
		}
	}
	var userID string
	if user := middleware.GetUser(ctx); user != nil {
		userID = user.ID
	}
	page, err := h.svc.ListPage(ctx, limit, sort, filter, userID, toRepositoryChannelPageCursor(input.Cursor))
	if err != nil {
		return ChannelPageResponse{}, apierr.Map(h.log, err, "list channels page")
	}
	out := h.toChannelResponses(ctx, page.Items)
	return ChannelPageResponse{Items: out, NextCursor: toChannelPageCursor(page.NextCursor)}, nil
}

func (h *Handler) ListFollowed(ctx context.Context) ([]ChannelResponse, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return nil, err
	}
	channels, err := h.svc.ListFollowedByUser(ctx, user.ID)
	if err != nil {
		return nil, apierr.Map(h.log, err, "list followed channels")
	}
	return h.toChannelResponses(ctx, channels), nil
}

func toRepositoryChannelPageCursor(cursor *ChannelPageCursor) *repository.ChannelPageCursor {
	if cursor == nil {
		return nil
	}
	return &repository.ChannelPageCursor{
		BroadcasterName: cursor.BroadcasterName,
		BroadcasterID:   cursor.BroadcasterID,
	}
}

func toChannelPageCursor(cursor *repository.ChannelPageCursor) *ChannelPageCursor {
	if cursor == nil {
		return nil
	}
	return &ChannelPageCursor{
		BroadcasterName: cursor.BroadcasterName,
		BroadcasterID:   cursor.BroadcasterID,
	}
}

// SearchInput drives channel.search. Empty Query returns everything up
// to Limit — the same endpoint backs the combobox "show all" state.
// Query is capped so a malicious caller can't feed a 1 MB ILIKE
// pattern; 100 chars comfortably covers Twitch logins (max 25) and
// display names (max 25) with headroom.
type SearchInput struct {
	Query string `json:"query" validate:"max=100"`
	Limit int    `json:"limit,omitempty" validate:"min=0,max=200"`
}

func (h *Handler) Search(ctx context.Context, input SearchInput) ([]ChannelResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	channels, err := h.svc.Search(ctx, input.Query, limit)
	if err != nil {
		return nil, apierr.Map(h.log, err, "search channels")
	}
	return h.toChannelResponses(ctx, channels), nil
}

type SetFavoriteInput struct {
	BroadcasterID string `json:"broadcaster_id" validate:"required"`
	Favorite      bool   `json:"favorite"`
}

func (h *Handler) SetFavorite(ctx context.Context, input SetFavoriteInput) (ChannelUserStateResponse, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return ChannelUserStateResponse{}, err
	}
	state, err := h.svc.SetFavorite(ctx, user.ID, input.BroadcasterID, input.Favorite)
	if err != nil {
		return ChannelUserStateResponse{}, apierr.Map(h.log, err, "set channel favorite")
	}
	return *toChannelUserStateResponse(state), nil
}

// LatestLiveResponse is the wire shape for one row of channel.latestLive:
// stream snapshot + flattened broadcaster display info, so the dashboard
// can render the card without a follow-up channel.getById per row.
type LatestLiveResponse struct {
	StreamID         string     `json:"stream_id"`
	BroadcasterID    string     `json:"broadcaster_id"`
	BroadcasterLogin string     `json:"broadcaster_login"`
	BroadcasterName  string     `json:"broadcaster_name"`
	ProfileImageURL  *string    `json:"profile_image_url,omitempty"`
	Type             string     `json:"type"`
	Language         string     `json:"language"`
	ThumbnailURL     *string    `json:"thumbnail_url,omitempty"`
	ViewerCount      int64      `json:"viewer_count"`
	IsMature         *bool      `json:"is_mature,omitempty"`
	StartedAt        time.Time  `json:"started_at"`
	EndedAt          *time.Time `json:"ended_at,omitempty"`
}

// LatestLiveInput caps result rows. Zero Limit uses a sensible default
// (8) — enough for a dashboard card without scrolling.
type LatestLiveInput struct {
	Limit int `json:"limit,omitempty" validate:"min=0,max=100"`
}

func (h *Handler) LatestLive(ctx context.Context, input LatestLiveInput) ([]LatestLiveResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 8
	}
	rows, err := h.svc.LatestLive(ctx, limit)
	if err != nil {
		return nil, apierr.Map(h.log, err, "load latest live streams")
	}
	out := make([]LatestLiveResponse, len(rows))
	for i, r := range rows {
		out[i] = LatestLiveResponse{
			StreamID:         r.ID,
			BroadcasterID:    r.BroadcasterID,
			BroadcasterLogin: r.BroadcasterLogin,
			BroadcasterName:  r.BroadcasterName,
			ProfileImageURL:  r.ProfileImageURL,
			Type:             r.Type,
			Language:         r.Language,
			ThumbnailURL:     r.ThumbnailURL,
			ViewerCount:      r.ViewerCount,
			IsMature:         r.IsMature,
			StartedAt:        r.StartedAt,
			EndedAt:          r.EndedAt,
		}
	}
	return out, nil
}

type SyncFromTwitchInput struct {
	BroadcasterID string `json:"broadcaster_id" validate:"required"`
}

// SyncFromTwitch uses the caller's user access token so rate-limit +
// fetch-log attribution stays accurate.
func (h *Handler) SyncFromTwitch(ctx context.Context, input SyncFromTwitchInput) (ChannelResponse, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return ChannelResponse{}, err
	}
	c, err := h.svc.SyncFromTwitch(ctx, SyncInput{
		BroadcasterID: input.BroadcasterID,
		UserID:        user.ID,
	})
	if err != nil {
		if twitch.IsUserAuthError(err) {
			h.log.Warn("sync channel", "error", err)
			return ChannelResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "twitch session expired; sign in again")
		}
		return ChannelResponse{}, apierr.Map(h.log, err, "sync channel")
	}
	return h.toChannelResponses(ctx, []repository.Channel{*c})[0], nil
}
