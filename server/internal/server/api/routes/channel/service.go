package channel

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

// Service handles tRPC channel procedures.
type Service struct {
	repo   repository.Repository
	twitch *twitch.Client
	log    *slog.Logger
}

// NewService creates a new channel tRPC service.
func NewService(repo repository.Repository, tc *twitch.Client, log *slog.Logger) *Service {
	return &Service{
		repo:   repo,
		twitch: tc,
		log:    log.With("domain", "channel"),
	}
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

// GetByIDInput for channel.getById.
type GetByIDInput struct {
	BroadcasterID string `json:"broadcaster_id" validate:"required"`
}

// GetByID fetches a channel by broadcaster ID (from DB, not Twitch).
func (s *Service) GetByID(ctx context.Context, input GetByIDInput) (ChannelResponse, error) {
	c, err := s.repo.GetChannel(ctx, input.BroadcasterID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ChannelResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "channel not found")
		}
		s.log.Error("failed to get channel", "error", err)
		return ChannelResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to get channel")
	}
	return toResponse(c), nil
}

// GetByLoginInput for channel.getByLogin.
type GetByLoginInput struct {
	Login string `json:"login" validate:"required"`
}

// GetByLogin fetches a channel by login name.
func (s *Service) GetByLogin(ctx context.Context, input GetByLoginInput) (ChannelResponse, error) {
	c, err := s.repo.GetChannelByLogin(ctx, input.Login)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ChannelResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "channel not found")
		}
		s.log.Error("failed to get channel by login", "error", err)
		return ChannelResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to get channel")
	}
	return toResponse(c), nil
}

// List returns all channels.
func (s *Service) List(ctx context.Context) ([]ChannelResponse, error) {
	channels, err := s.repo.ListChannels(ctx)
	if err != nil {
		s.log.Error("failed to list channels", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list channels")
	}
	out := make([]ChannelResponse, len(channels))
	for i, c := range channels {
		out[i] = toResponse(&c)
	}
	return out, nil
}

// ListFollowed returns the current user's followed channels (from DB, not Twitch).
func (s *Service) ListFollowed(ctx context.Context) ([]ChannelResponse, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return nil, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}

	channels, err := s.repo.ListUserFollows(ctx, user.ID)
	if err != nil {
		s.log.Error("failed to list followed channels", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list followed channels")
	}
	out := make([]ChannelResponse, len(channels))
	for i, c := range channels {
		out[i] = toResponse(&c)
	}
	return out, nil
}

// SyncFromTwitchInput for channel.syncFromTwitch.
type SyncFromTwitchInput struct {
	BroadcasterID string `json:"broadcaster_id" validate:"required"`
}

// SyncFromTwitch fetches a channel from Twitch API and upserts it in the DB.
// Uses the user's access token.
func (s *Service) SyncFromTwitch(ctx context.Context, input SyncFromTwitchInput) (ChannelResponse, error) {
	tokens := middleware.GetTokens(ctx)
	user := middleware.GetUser(ctx)
	if tokens == nil || user == nil {
		return ChannelResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	// Attach the user's access token + ID so Helix calls use the user endpoint
	// set and fetch logs are attributed correctly.
	ctx = twitch.WithUserToken(ctx, tokens.AccessToken)
	ctx = twitch.WithUserID(ctx, user.ID)

	users, err := s.twitch.GetUsers(ctx, &twitch.GetUsersParams{ID: []string{input.BroadcasterID}})
	if err != nil {
		s.log.Error("failed to fetch twitch user", "error", err)
		return ChannelResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to fetch channel from twitch")
	}
	if len(users) == 0 {
		return ChannelResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "twitch user not found")
	}
	u := users[0]

	// Channel-information endpoint carries broadcaster_language, title, game.
	// Failure here is non-fatal — we still upsert the user profile.
	var language *string
	chans, err := s.twitch.GetChannelInformation(ctx, &twitch.GetChannelInformationParams{BroadcasterID: []string{u.ID}})
	if err != nil {
		s.log.Warn("failed to fetch channel information; profile sync continuing", "broadcaster_id", u.ID, "error", err)
	} else if len(chans) > 0 {
		language = stringOrNil(chans[0].BroadcasterLanguage)
	}

	c, err := s.repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID:       u.ID,
		BroadcasterLogin:    u.Login,
		BroadcasterName:     u.DisplayName,
		BroadcasterLanguage: language,
		ProfileImageURL:     stringOrNil(u.ProfileImageURL),
		OfflineImageURL:     stringOrNil(u.OfflineImageURL),
		Description:         stringOrNil(u.Description),
		BroadcasterType:     stringOrNil(u.BroadcasterType),
		ViewCount:           0,
	})
	if err != nil {
		s.log.Error("failed to upsert channel", "error", err)
		return ChannelResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to save channel")
	}
	return toResponse(c), nil
}

func stringOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
