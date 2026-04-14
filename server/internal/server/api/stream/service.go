// Package stream owns the stream domain. Writes happen via EventSub
// webhooks (stream.online / stream.offline), the scheduled viewer-count
// poller, and (for currently-live reads) Followed's side-effect mirror
// of Helix snapshots; this read-mostly surface is what the dashboard +
// public API share.
package stream

import (
	"context"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/ptr"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// maxFollowedPages caps the cursor loop in Followed. Twitch returns up
// to 100 rows per page, so 10 pages = 1000 currently-live followed
// channels — far above any realistic human follow-graph. The cap is a
// safety net against a runaway cursor (API bug, compromised token
// hitting a bot account), not a typical-case limit.
const maxFollowedPages = 10

// followedStreamsSource is the narrow slice of the Twitch client that
// Followed needs. Kept private to this package so tests can supply a
// fake without pulling in httptest; *twitch.Client satisfies it in
// production, so callers of New pass the concrete client unchanged.
type followedStreamsSource interface {
	GetFollowedStreams(ctx context.Context, params *twitch.GetFollowedStreamsParams) ([]twitch.Stream, twitch.Pagination, error)
}

// Service is the stream domain service.
type Service struct {
	repo   repository.Repository
	twitch followedStreamsSource
	log    *slog.Logger
}

// New builds the service.
func New(repo repository.Repository, tc followedStreamsSource, log *slog.Logger) *Service {
	return &Service{repo: repo, twitch: tc, log: log.With("domain", "stream")}
}

// ListActive returns every currently-live stream (ended_at IS NULL).
func (s *Service) ListActive(ctx context.Context) ([]repository.Stream, error) {
	return s.repo.ListActiveStreams(ctx)
}

// ListByBroadcaster returns a broadcaster's stream history, paginated.
func (s *Service) ListByBroadcaster(ctx context.Context, broadcasterID string, limit, offset int) ([]repository.Stream, error) {
	return s.repo.ListStreamsByBroadcaster(ctx, broadcasterID, limit, offset)
}

// GetLastLive returns the most recent stream (active or ended), or
// repository.ErrNotFound if the broadcaster has no stream history.
func (s *Service) GetLastLive(ctx context.Context, broadcasterID string) (*repository.Stream, error) {
	return s.repo.GetLastLiveStream(ctx, broadcasterID)
}

// FollowedInput carries the caller identity for Followed. Kept separate
// from the tRPC Input struct so the service is reusable from non-tRPC
// entry points (e.g., a scheduled "poll live channels" task).
type FollowedInput struct {
	UserID          string
	UserAccessToken string
}

// FollowedStream is the service-layer shape for a currently-live
// followed channel: the Helix snapshot plus local metadata that isn't
// in Helix's /streams response. Today that's only profile_image_url —
// served from the local channels mirror so the frontend doesn't have
// to burn a second round-trip (or fetch the entire channel list) just
// to render an avatar next to the stream card. Channels not present
// in the local mirror have ProfileImageURL == nil; the frontend's
// Avatar component already falls back to initials in that case.
type FollowedStream struct {
	Stream          twitch.Stream
	ProfileImageURL *string
}

// Followed returns every channel the caller follows that is currently
// streaming, sourced from Helix GET /streams/followed. This is distinct
// from ListActive (our local streams table) — Helix is the source of
// truth for "live right now", which matters when the local streams
// table is empty because no EventSub notifications have fired yet.
//
// Side effects (both piggybacked on the same ListChannelsByIDs lookup,
// so there's no per-call cost beyond the Helix round trip):
//
//  1. Streams whose broadcaster exists in our local channels mirror are
//     upserted into the streams table, so channel.latestLive accumulates
//     history without waiting for stream.online webhooks.
//  2. The returned FollowedStream rows are enriched with
//     profile_image_url from the same mirror lookup — the frontend
//     avatar render happens against this single response.
//
// Streams for unsynced broadcasters are returned with nil profile image
// and skipped from the upsert; sync via channel.syncFromTwitch is the
// deliberate path to add a channel to the mirror.
func (s *Service) Followed(ctx context.Context, input FollowedInput) ([]FollowedStream, error) {
	ctx = twitch.WithUserToken(ctx, input.UserAccessToken)
	ctx = twitch.WithUserID(ctx, input.UserID)

	streams, err := s.fetchFollowedStreamsCapped(ctx, input.UserID)
	if err != nil {
		return nil, err
	}

	profileByID, err := s.mirrorAndCollectProfiles(ctx, streams)
	if err != nil {
		// Best-effort mirror: a local-DB hiccup shouldn't fail the API
		// call. Log and fall through with an empty profile map so the
		// Helix payload still flows (just without avatars).
		s.log.Warn("mirror followed live streams to local DB", "error", err)
		profileByID = map[string]*string{}
	}

	out := make([]FollowedStream, len(streams))
	for i := range streams {
		out[i] = FollowedStream{
			Stream:          streams[i],
			ProfileImageURL: profileByID[streams[i].UserID],
		}
	}
	return out, nil
}

// fetchFollowedStreamsCapped walks the Helix cursor up to
// maxFollowedPages and returns the accumulated result. Uses the
// single-page GetFollowedStreams rather than GetFollowedStreamsAll so
// the cap is local to this service.
func (s *Service) fetchFollowedStreamsCapped(ctx context.Context, userID string) ([]twitch.Stream, error) {
	params := &twitch.GetFollowedStreamsParams{UserID: userID}
	var out []twitch.Stream
	for range maxFollowedPages {
		page, pagination, err := s.twitch.GetFollowedStreams(ctx, params)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if pagination.Cursor == "" {
			return out, nil
		}
		params.After = pagination.Cursor
	}
	s.log.Warn("followed-streams pagination hit maxFollowedPages; truncating",
		"user_id", userID, "pages", maxFollowedPages, "returned", len(out))
	return out, nil
}

// mirrorAndCollectProfiles does both jobs that need a local-channels
// lookup keyed by the Helix stream broadcaster_ids: (a) mirrors
// locally-known streams into the streams table, (b) returns a
// broadcaster_id → profile_image_url map for the response enrichment.
// Bundled so we make exactly one ListChannelsByIDs call per Followed
// invocation regardless of how many consumers downstream need the map.
func (s *Service) mirrorAndCollectProfiles(ctx context.Context, streams []twitch.Stream) (map[string]*string, error) {
	if len(streams) == 0 {
		return map[string]*string{}, nil
	}
	ids := make([]string, len(streams))
	for i, st := range streams {
		ids[i] = st.UserID
	}
	known, err := s.repo.ListChannelsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	profileByID := make(map[string]*string, len(known))
	knownSet := make(map[string]struct{}, len(known))
	for i := range known {
		c := &known[i]
		knownSet[c.BroadcasterID] = struct{}{}
		profileByID[c.BroadcasterID] = c.ProfileImageURL
	}
	for i := range streams {
		st := &streams[i]
		if _, ok := knownSet[st.UserID]; !ok {
			continue
		}
		if _, upsertErr := s.repo.UpsertStream(ctx, &repository.StreamInput{
			ID:            st.ID,
			BroadcasterID: st.UserID,
			Type:          st.Type,
			Language:      st.Language,
			ThumbnailURL:  ptr.StringOrNil(st.ThumbnailURL),
			ViewerCount:   int64(st.ViewerCount),
			StartedAt:     st.StartedAt,
		}); upsertErr != nil {
			// With pre-filtering, the only remaining failure modes are
			// driver / infra errors — a broadcaster race-deleted between
			// our ListChannelsByIDs and UpsertStream is theoretically
			// possible but vanishingly rare. Log per row to preserve
			// which stream failed; don't abort the loop.
			s.log.Warn("upsert followed live stream",
				"stream_id", st.ID, "broadcaster_id", st.UserID, "error", upsertErr)
		}
	}
	return profileByID, nil
}
