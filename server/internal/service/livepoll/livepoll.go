// Package livepoll detects Twitch live/offline transitions by polling Helix.
package livepoll

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
)

const maxGetStreamsUserIDs = 100

// seedFailureEscalation is how many consecutive seed failures we tolerate while
// logging at warn before escalating to error. Seeding reads the local DB; a few
// transient misses are unremarkable, but a persistently unreadable store stalls
// all live/offline detection and must not stay buried in warn-level noise.
const seedFailureEscalation = 3

type Repository interface {
	ListActiveStreams(ctx context.Context) ([]repository.Stream, error)
	ListChannels(ctx context.Context) ([]repository.Channel, error)
}

type TwitchClient interface {
	GetStreams(ctx context.Context, params *twitch.GetStreamsParams) ([]twitch.Stream, twitch.Pagination, error)
}

type Processor interface {
	// DispatchStreamOnlineFromStream takes the full polled stream so the
	// processor enriches from it directly instead of re-fetching Helix.
	DispatchStreamOnlineFromStream(ctx context.Context, stream twitch.Stream) error
	DispatchStreamOffline(ctx context.Context, event twitch.StreamOfflineEvent) error
	// CloseStaleStream ends the broadcaster's open stream row without emitting
	// an SSE offline, for the case where the broadcaster stays live under a new
	// stream ID and we only need to retire the superseded row.
	CloseStaleStream(ctx context.Context, broadcasterID string) error
}

// liveStream is the per-broadcaster live state we carry between ticks: the
// stream ID (to detect a stream-ID change without an offline in between) plus
// the broadcaster identity learned from the live Helix stream, so an offline
// event can still name the channel if its mirror row has since been removed.
type liveStream struct {
	streamID string
	login    string
	name     string
}

type Service struct {
	repo      Repository
	twitch    TwitchClient
	processor Processor
	interval  time.Duration
	log       *slog.Logger

	seeded       bool
	seedFailures int
	lastLive     map[string]liveStream
}

func New(repo Repository, twitch TwitchClient, processor Processor, interval time.Duration, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	if interval <= 0 {
		interval = time.Minute
	}
	return &Service{
		repo:      repo,
		twitch:    twitch,
		processor: processor,
		interval:  interval,
		log:       log.With("domain", "livepoll"),
		lastLive:  make(map[string]liveStream),
	}
}

func (s *Service) Run(ctx context.Context) {
	if s.repo == nil || s.twitch == nil || s.processor == nil {
		s.log.Warn("live poller disabled; missing dependency")
		return
	}

	s.log.Info("live poller started", "interval", s.interval)
	defer s.log.Info("live poller stopped")

	s.runOnce(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

func (s *Service) runOnce(ctx context.Context) {
	if err := s.tick(ctx); err != nil {
		s.log.Warn("live poll tick failed", "error", err)
	}
}

// tick is one poll iteration: make sure we have a live baseline, fetch who is
// live now, then diff that against lastLive to emit the went-live and
// went-offline transitions. Each step is its own method so this stays a flat
// orchestrator rather than a single high-complexity loop nest.
func (s *Service) tick(ctx context.Context) error {
	if err := s.ensureSeeded(ctx); err != nil {
		return err
	}

	channels, err := s.repo.ListChannels(ctx)
	if err != nil {
		return fmt.Errorf("list channels: %w", err)
	}
	channelByID, ids := collectBroadcasters(channels)

	liveNow, err := s.fetchLive(ctx, ids)
	if err != nil {
		return err
	}

	// Online must run before offline: it mutates lastLive (closing stale rows,
	// recording newly-live broadcasters), and the offline pass reads the result
	// to decide who has truly left the live set.
	return errors.Join(
		s.dispatchOnline(ctx, liveNow),
		s.dispatchOffline(ctx, liveNow, channelByID),
	)
}

// ensureSeeded loads the baseline of already-live broadcasters once, before the
// first diff. Until it succeeds tick must not proceed: without a baseline we
// cannot tell "still live" from "newly live", so every live broadcaster would
// get a spurious stream.online. A seed failure is returned so the tick aborts
// and retries next interval; repeated failures escalate past warn so a
// persistently unreadable store is not silent.
func (s *Service) ensureSeeded(ctx context.Context) error {
	if s.seeded {
		return nil
	}
	if err := s.seedActiveStreams(ctx); err != nil {
		s.seedFailures++
		if s.seedFailures >= seedFailureEscalation {
			s.log.Error("live poller cannot seed active streams; live/offline detection is stalled until the store recovers",
				"consecutive_failures", s.seedFailures, "error", err)
		}
		return err
	}
	s.seedFailures = 0
	return nil
}

// collectBroadcasters builds the broadcaster-ID lookup and the deduplicated ID
// list to query Helix with, skipping rows without a broadcaster ID and keeping
// the first channel seen per ID.
func collectBroadcasters(channels []repository.Channel) (map[string]repository.Channel, []string) {
	channelByID := make(map[string]repository.Channel, len(channels))
	ids := make([]string, 0, len(channels))
	for _, ch := range channels {
		if ch.BroadcasterID == "" {
			continue
		}
		if _, exists := channelByID[ch.BroadcasterID]; exists {
			continue
		}
		channelByID[ch.BroadcasterID] = ch
		ids = append(ids, ch.BroadcasterID)
	}
	return channelByID, ids
}

// dispatchOnline emits stream.online for broadcasters that are live now under a
// stream ID we have not recorded, recording each into lastLive. Errors are
// joined rather than fatal so one broadcaster's failure does not skip the rest.
func (s *Service) dispatchOnline(ctx context.Context, liveNow map[string]twitch.Stream) error {
	var dispatchErr error
	for broadcasterID, stream := range liveNow {
		prev, wasLive := s.lastLive[broadcasterID]
		if wasLive && prev.streamID == stream.ID {
			continue
		}
		// The broadcaster is live under a stream ID we have not recorded. If a
		// different stream was live before (a rerun, or an offline/online blip
		// that spanned a poll interval), retire the old streams row so it gets
		// ended_at instead of leaking as perpetually live and polluting the next
		// restart's seed. CloseStaleStream (not DispatchStreamOffline) because the
		// broadcaster never left the live set, so the live-dot must not flicker.
		if wasLive {
			if err := s.processor.CloseStaleStream(ctx, broadcasterID); err != nil {
				dispatchErr = errors.Join(dispatchErr, fmt.Errorf("close stale stream for %s: %w", broadcasterID, err))
				continue
			}
			delete(s.lastLive, broadcasterID)
		}
		if err := s.processor.DispatchStreamOnlineFromStream(ctx, stream); err != nil {
			dispatchErr = errors.Join(dispatchErr, fmt.Errorf("dispatch stream.online for %s: %w", broadcasterID, err))
			continue
		}
		s.lastLive[broadcasterID] = liveStream{streamID: stream.ID, login: stream.UserLogin, name: stream.UserName}
	}
	return dispatchErr
}

// dispatchOffline emits stream.offline for broadcasters in lastLive that are no
// longer live, removing each from lastLive. Errors are joined so one failure
// does not skip the rest.
func (s *Service) dispatchOffline(ctx context.Context, liveNow map[string]twitch.Stream, channelByID map[string]repository.Channel) error {
	var dispatchErr error
	for broadcasterID, prev := range s.lastLive {
		if _, stillLive := liveNow[broadcasterID]; stillLive {
			continue
		}
		// Prefer the channel mirror for the offline event's login/name (it
		// tracks renames), but fall back to the identity cached when the
		// broadcaster went live so a since-removed channel row doesn't blank the
		// SSE delta. Broadcaster ID is always correct; warn only when we have
		// neither name source.
		login, name := prev.login, prev.name
		if ch, ok := channelByID[broadcasterID]; ok {
			login, name = ch.BroadcasterLogin, ch.BroadcasterName
		}
		if login == "" && name == "" {
			s.log.Warn("dispatching stream.offline with broadcaster ID only; no channel row or cached identity",
				"broadcaster_id", broadcasterID)
		}
		if err := s.processor.DispatchStreamOffline(ctx, streamOfflineEvent(broadcasterID, login, name)); err != nil {
			dispatchErr = errors.Join(dispatchErr, fmt.Errorf("dispatch stream.offline for %s: %w", broadcasterID, err))
			continue
		}
		delete(s.lastLive, broadcasterID)
	}
	return dispatchErr
}

func (s *Service) seedActiveStreams(ctx context.Context) error {
	streams, err := s.repo.ListActiveStreams(ctx)
	if err != nil {
		return fmt.Errorf("seed active streams: %w", err)
	}
	for _, stream := range streams {
		if stream.BroadcasterID == "" || stream.ID == "" {
			continue
		}
		// repository.Stream carries no login/name; identity is filled in once
		// the broadcaster is observed live via Helix (or resolved from the
		// channel mirror at offline time).
		if _, exists := s.lastLive[stream.BroadcasterID]; !exists {
			s.lastLive[stream.BroadcasterID] = liveStream{streamID: stream.ID}
		}
	}
	s.seeded = true
	return nil
}

func (s *Service) fetchLive(ctx context.Context, broadcasterIDs []string) (map[string]twitch.Stream, error) {
	live := make(map[string]twitch.Stream)
	for start := 0; start < len(broadcasterIDs); start += maxGetStreamsUserIDs {
		end := start + maxGetStreamsUserIDs
		if end > len(broadcasterIDs) {
			end = len(broadcasterIDs)
		}
		// Page through each batch. Helix's get-streams returns at most `first`
		// items per page (default 20, max 100) even when every result is pinned
		// by an explicit user_id. Without First=100 plus cursor draining, a batch
		// with more than 20 simultaneously-live broadcasters would silently drop
		// the tail, and the offline loop would then dispatch spurious
		// stream.offline events for the dropped (still-live) channels.
		cursor := ""
		for {
			streams, pagination, err := s.twitch.GetStreams(ctx, &twitch.GetStreamsParams{
				UserID: broadcasterIDs[start:end],
				Type:   "live",
				First:  maxGetStreamsUserIDs,
				After:  cursor,
			})
			if err != nil {
				return nil, fmt.Errorf("get streams: %w", err)
			}
			for _, stream := range streams {
				if stream.UserID == "" || stream.ID == "" {
					continue
				}
				live[stream.UserID] = stream
			}
			if pagination.Cursor == "" {
				break
			}
			cursor = pagination.Cursor
		}
	}
	return live, nil
}

func streamOfflineEvent(broadcasterID, login, name string) twitch.StreamOfflineEvent {
	return twitch.StreamOfflineEvent{
		BroadcasterUserID:    broadcasterID,
		BroadcasterUserLogin: login,
		BroadcasterUserName:  name,
	}
}
