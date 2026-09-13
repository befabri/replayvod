// Package followsync owns demand-driven imports of a user's Twitch follows.
package followsync

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/befabri/replayvod/server/internal/background"
	"github.com/befabri/replayvod/server/internal/ptr"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
)

const (
	syncConcurrency = 2
	syncTimeout     = 30 * time.Second
)

// Service owns imports beyond the initiating HTTP request. Its owner must call
// Stop and Wait before releasing the repository or Twitch client.
type Service struct {
	repo   repository.Repository
	twitch *twitch.Client
	log    *slog.Logger
	work   *background.Runner
}

// New creates an idle service that admits imports until Stop is called.
func New(repo repository.Repository, client *twitch.Client, log *slog.Logger) *Service {
	return &Service{repo: repo, twitch: client, log: log.With("domain", "follow-sync"),
		work: background.New(map[string]int{"follows": syncConcurrency})}
}

// Request queues an import independently of the HTTP request and coalesces logins
// for the same user; credentials stay in memory until the bounded import settles.
func (s *Service) Request(userID, accessToken string) error {
	if userID == "" || accessToken == "" || s.twitch == nil {
		return fmt.Errorf("follow sync requires a user and Twitch credentials")
	}
	err := s.work.StartQueued("follows", userID, func(parent context.Context) error {
		ctx, cancel := context.WithTimeout(parent, syncTimeout)
		defer cancel()
		return s.sync(ctx, userID, accessToken)
	}, func(err error) {
		if err != nil && !errors.Is(err, context.Canceled) {
			s.log.Warn("sync user follows failed", "user_id", userID, "error", err)
		}
	})
	if errors.Is(err, background.ErrBusy) {
		return nil
	}
	return err
}

// Stop closes admission and cancels queued and running imports.
func (s *Service) Stop() { s.work.Stop() }

// Wait joins network calls, database writes, and error reporting after Stop.
func (s *Service) Wait(ctx context.Context) error { return s.work.Wait(ctx) }

func (s *Service) sync(ctx context.Context, userID, accessToken string) error {
	authCtx := twitch.WithUserToken(ctx, accessToken)
	follows, _, err := s.twitch.GetFollowedChannelsAll(authCtx, &twitch.GetFollowedChannelsParams{UserID: userID})
	if err != nil {
		return fmt.Errorf("fetch followed channels: %w", err)
	}
	if len(follows) == 0 {
		return nil
	}
	ids := make([]string, len(follows))
	for i, follow := range follows {
		ids[i] = follow.BroadcasterID
	}
	users := make(map[string]twitch.User, len(follows))
	for start := 0; start < len(ids); start += 100 {
		batch, err := s.twitch.GetUsers(authCtx, &twitch.GetUsersParams{ID: ids[start:min(start+100, len(ids))]})
		if err != nil {
			return fmt.Errorf("enrich users: %w", err)
		}
		for _, user := range batch {
			users[user.ID] = user
		}
	}
	// Concurrent users can share channels; acquire their row locks in the same order.
	slices.SortFunc(follows, func(a, b twitch.FollowedChannel) int {
		return cmp.Compare(a.BroadcasterID, b.BroadcasterID)
	})
	// Commit the full import so a failed or cancelled write cannot leave partial follows.
	if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		for _, follow := range follows {
			channel := &repository.Channel{BroadcasterID: follow.BroadcasterID,
				BroadcasterLogin: follow.BroadcasterLogin, BroadcasterName: follow.BroadcasterName}
			if user, ok := users[follow.BroadcasterID]; ok {
				channel.ProfileImageURL = ptr.StringOrNil(user.ProfileImageURL)
				channel.OfflineImageURL = ptr.StringOrNil(user.OfflineImageURL)
				channel.Description = ptr.StringOrNil(user.Description)
				channel.BroadcasterType = ptr.StringOrNil(user.BroadcasterType)
			}
			if _, err := tx.UpsertChannel(ctx, channel); err != nil {
				return fmt.Errorf("upsert channel %s: %w", follow.BroadcasterID, err)
			}
			if err := tx.UpsertUserFollow(ctx, &repository.UserFollow{UserID: userID,
				BroadcasterID: follow.BroadcasterID, FollowedAt: follow.FollowedAt, Followed: true}); err != nil {
				return fmt.Errorf("upsert user follow %s: %w", follow.BroadcasterID, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	s.log.Info("synced user follows", "user_id", userID, "count", len(follows))
	return nil
}
