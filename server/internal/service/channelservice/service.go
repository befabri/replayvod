// Package channelservice owns channel domain business logic:
// reading mirrored channel + follow data, and the Twitch-side sync
// that upserts a channel from /users + /channels endpoints.
//
// Transport-agnostic: no tRPC or HTTP types cross this boundary.
package channelservice

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// Service handles channel business logic.
type Service struct {
	repo   repository.Repository
	twitch *twitch.Client
	log    *slog.Logger
}

// New builds the service.
func New(repo repository.Repository, tc *twitch.Client, log *slog.Logger) *Service {
	return &Service{
		repo:   repo,
		twitch: tc,
		log:    log.With("domain", "channel"),
	}
}

// GetByID returns the mirrored channel row, if any. Returns
// repository.ErrNotFound if the channel isn't mirrored — transport
// layer maps to 404.
func (s *Service) GetByID(ctx context.Context, broadcasterID string) (*repository.Channel, error) {
	return s.repo.GetChannel(ctx, broadcasterID)
}

// GetByLogin returns the mirrored channel row, if any, by login name.
func (s *Service) GetByLogin(ctx context.Context, login string) (*repository.Channel, error) {
	return s.repo.GetChannelByLogin(ctx, login)
}

// List returns every mirrored channel.
func (s *Service) List(ctx context.Context) ([]repository.Channel, error) {
	return s.repo.ListChannels(ctx)
}

// ListFollowedByUser returns the channels the given user follows.
func (s *Service) ListFollowedByUser(ctx context.Context, userID string) ([]repository.Channel, error) {
	return s.repo.ListUserFollows(ctx, userID)
}

// SyncInput is the input to SyncFromTwitch. userID + userAccessToken
// are the caller's identity; the service attaches both to the Twitch
// context so rate limiting and fetch-log attribution see the right
// actor.
type SyncInput struct {
	BroadcasterID   string
	UserID          string
	UserAccessToken string
}

// SyncFromTwitch fetches the given broadcaster's Helix user data and
// upserts a mirrored channel row. Channel-information lookup is
// best-effort — a failure there logs and continues with the basic
// user upsert; callers need to tolerate BroadcasterLanguage being
// nil even on success.
func (s *Service) SyncFromTwitch(ctx context.Context, input SyncInput) (*repository.Channel, error) {
	ctx = twitch.WithUserToken(ctx, input.UserAccessToken)
	ctx = twitch.WithUserID(ctx, input.UserID)

	users, err := s.twitch.GetUsers(ctx, &twitch.GetUsersParams{ID: []string{input.BroadcasterID}})
	if err != nil {
		return nil, fmt.Errorf("fetch twitch user: %w", err)
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("twitch user not found")
	}
	u := users[0]

	var language *string
	chans, err := s.twitch.GetChannelInformation(ctx, &twitch.GetChannelInformationParams{BroadcasterID: []string{u.ID}})
	if err != nil {
		s.log.Warn("fetch channel information; continuing with user-only upsert",
			"broadcaster_id", u.ID, "error", err)
	} else if len(chans) > 0 {
		language = stringOrNil(chans[0].BroadcasterLanguage)
	}

	return s.repo.UpsertChannel(ctx, &repository.Channel{
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
}

func stringOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
