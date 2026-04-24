// Package channel owns the channel domain: reading mirrored channel +
// follow data, and the Twitch-side sync that upserts a channel from
// /users + /channels endpoints.
package channel

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/ptr"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
)

type channelRepo interface {
	GetChannel(ctx context.Context, broadcasterID string) (*repository.Channel, error)
	GetChannelByLogin(ctx context.Context, login string) (*repository.Channel, error)
	ListChannels(ctx context.Context) ([]repository.Channel, error)
	ListChannelsPage(ctx context.Context, limit int, sort string, liveOnly bool, cursor *repository.ChannelPageCursor) (*repository.ChannelPage, error)
	ListUserFollows(ctx context.Context, userID string) ([]repository.Channel, error)
	SearchChannels(ctx context.Context, query string, limit int) ([]repository.Channel, error)
	ListLatestLivePerChannel(ctx context.Context, limit int) ([]repository.LatestLiveStream, error)
	UpsertChannel(ctx context.Context, c *repository.Channel) (*repository.Channel, error)
}

// Service is the channel domain service.
type Service struct {
	repo   channelRepo
	twitch *twitch.Client
	log    *slog.Logger
}

// New builds the service.
func New(repo channelRepo, tc *twitch.Client, log *slog.Logger) *Service {
	return &Service{repo: repo, twitch: tc, log: log.With("domain", "channel")}
}

// GetByID returns the mirrored channel row, if any. Returns
// repository.ErrNotFound if the channel isn't mirrored.
func (s *Service) GetByID(ctx context.Context, broadcasterID string) (*repository.Channel, error) {
	return s.repo.GetChannel(ctx, broadcasterID)
}

// GetByLogin returns the mirrored channel row by login name.
func (s *Service) GetByLogin(ctx context.Context, login string) (*repository.Channel, error) {
	return s.repo.GetChannelByLogin(ctx, login)
}

// List returns every mirrored channel.
func (s *Service) List(ctx context.Context) ([]repository.Channel, error) {
	return s.repo.ListChannels(ctx)
}

// ListPage returns a cursor-paginated slice of mirrored channels.
func (s *Service) ListPage(ctx context.Context, limit int, sort string, liveOnly bool, cursor *repository.ChannelPageCursor) (*repository.ChannelPage, error) {
	return s.repo.ListChannelsPage(ctx, limit, sort, liveOnly, cursor)
}

// ListFollowedByUser returns the channels the given user follows.
func (s *Service) ListFollowedByUser(ctx context.Context, userID string) ([]repository.Channel, error) {
	return s.repo.ListUserFollows(ctx, userID)
}

// Search returns channels matching query (empty matches everything),
// ranked by match quality and capped at limit.
func (s *Service) Search(ctx context.Context, query string, limit int) ([]repository.Channel, error) {
	return s.repo.SearchChannels(ctx, query, limit)
}

// LatestLive returns the most recent stream per broadcaster (flattened
// with channel display metadata), newest first, up to limit rows.
func (s *Service) LatestLive(ctx context.Context, limit int) ([]repository.LatestLiveStream, error) {
	return s.repo.ListLatestLivePerChannel(ctx, limit)
}

// SyncInput carries the caller identity for SyncFromTwitch. The
// service attaches both to the Twitch context so rate limiting and
// fetch-log attribution see the right actor.
type SyncInput struct {
	BroadcasterID string
	UserID        string
}

// SyncFromTwitch fetches the given broadcaster's Helix user data and
// upserts a mirrored channel row. Channel-information lookup is
// best-effort — a failure there logs and continues with the basic
// user upsert; callers need to tolerate BroadcasterLanguage being
// nil even on success.
func (s *Service) SyncFromTwitch(ctx context.Context, input SyncInput) (*repository.Channel, error) {
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
		language = ptr.StringOrNil(chans[0].BroadcasterLanguage)
	}

	return s.repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID:       u.ID,
		BroadcasterLogin:    u.Login,
		BroadcasterName:     u.DisplayName,
		BroadcasterLanguage: language,
		ProfileImageURL:     ptr.StringOrNil(u.ProfileImageURL),
		OfflineImageURL:     ptr.StringOrNil(u.OfflineImageURL),
		Description:         ptr.StringOrNil(u.Description),
		BroadcasterType:     ptr.StringOrNil(u.BroadcasterType),
		ViewCount:           0,
	})
}
