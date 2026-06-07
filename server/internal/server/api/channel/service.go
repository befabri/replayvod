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
	ListChannelsPage(ctx context.Context, limit int, sort string, filter string, userID string, cursor *repository.ChannelPageCursor) (*repository.ChannelPage, error)
	ListUserFollows(ctx context.Context, userID string) ([]repository.Channel, error)
	SearchChannels(ctx context.Context, query string, limit int) ([]repository.Channel, error)
	ListLatestLivePerChannel(ctx context.Context, limit int) ([]repository.LatestLiveStream, error)
	GetChannelUserState(ctx context.Context, userID string, broadcasterID string) (*repository.ChannelUserState, error)
	ListChannelUserStatesForChannels(ctx context.Context, userID string, broadcasterIDs []string) ([]repository.ChannelUserState, error)
	SetChannelFavorite(ctx context.Context, userID string, broadcasterID string, favorite bool) (*repository.ChannelUserState, error)
	UpsertChannel(ctx context.Context, c *repository.Channel) (*repository.Channel, error)
}

type Service struct {
	repo   channelRepo
	twitch *twitch.Client
	log    *slog.Logger
}

func New(repo channelRepo, tc *twitch.Client, log *slog.Logger) *Service {
	return &Service{repo: repo, twitch: tc, log: log.With("domain", "channel")}
}

func (s *Service) GetByID(ctx context.Context, broadcasterID string) (*repository.Channel, error) {
	return s.repo.GetChannel(ctx, broadcasterID)
}

func (s *Service) GetByLogin(ctx context.Context, login string) (*repository.Channel, error) {
	return s.repo.GetChannelByLogin(ctx, login)
}

func (s *Service) List(ctx context.Context) ([]repository.Channel, error) {
	return s.repo.ListChannels(ctx)
}

func (s *Service) ListPage(ctx context.Context, limit int, sort string, filter string, userID string, cursor *repository.ChannelPageCursor) (*repository.ChannelPage, error) {
	return s.repo.ListChannelsPage(ctx, limit, sort, normalizeChannelFilter(filter), userID, cursor)
}

func normalizeChannelFilter(filter string) string {
	switch filter {
	case repository.ChannelFilterLive, repository.ChannelFilterDownloaded, repository.ChannelFilterFavorites:
		return filter
	default:
		return repository.ChannelFilterAll
	}
}

func (s *Service) ListFollowedByUser(ctx context.Context, userID string) ([]repository.Channel, error) {
	return s.repo.ListUserFollows(ctx, userID)
}

// Search returns channels matching query (empty matches everything),
// ranked by match quality and capped at limit.
func (s *Service) Search(ctx context.Context, query string, limit int) ([]repository.Channel, error) {
	return s.repo.SearchChannels(ctx, query, limit)
}

func (s *Service) UserState(ctx context.Context, userID string, broadcasterID string) (*repository.ChannelUserState, error) {
	return s.repo.GetChannelUserState(ctx, userID, broadcasterID)
}

func (s *Service) UserStatesByBroadcasterID(ctx context.Context, userID string, channels []repository.Channel) map[string]*repository.ChannelUserState {
	out := make(map[string]*repository.ChannelUserState)
	if userID == "" || len(channels) == 0 {
		return out
	}
	ids := make([]string, 0, len(channels))
	seen := make(map[string]struct{}, len(channels))
	for _, channel := range channels {
		if _, ok := seen[channel.BroadcasterID]; ok {
			continue
		}
		seen[channel.BroadcasterID] = struct{}{}
		ids = append(ids, channel.BroadcasterID)
	}
	states, err := s.repo.ListChannelUserStatesForChannels(ctx, userID, ids)
	if err != nil {
		s.log.Warn("resolve channel user states", "error", err)
		return out
	}
	for i := range states {
		state := states[i]
		out[state.BroadcasterID] = &state
	}
	return out
}

func (s *Service) SetFavorite(ctx context.Context, userID string, broadcasterID string, favorite bool) (*repository.ChannelUserState, error) {
	if _, err := s.repo.GetChannel(ctx, broadcasterID); err != nil {
		return nil, err
	}
	return s.repo.SetChannelFavorite(ctx, userID, broadcasterID, favorite)
}

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
