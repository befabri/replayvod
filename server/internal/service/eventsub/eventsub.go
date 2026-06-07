// Package eventsub wraps the generated Twitch EventSub client with
// local-mirror bookkeeping: every successful subscription create is
// reflected in the subscriptions table, snapshots record quota usage
// over time, and revocations soft-delete rather than drop.
//
// Shared across transports: the tRPC handler in api/eventsub calls
// Subscribe/Unsubscribe/Snapshot; the scheduler cron task calls
// Snapshot. Domain logic lives here, not under api/.
package eventsub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// ErrCallbackURLNotUsable is returned when subscribe is called with a
// callback URL Twitch will reject (non-HTTPS, missing host, etc.).
// Surfaced as a clean error so the reconcile loop can early-out with
// a single "skipping, bad URL" log instead of hammering Twitch and
// producing one 400 per channel. Typical homelab cause: running in
// dev mode with http://localhost:8080 configured.
var ErrCallbackURLNotUsable = errors.New("eventsub: callback URL is not a valid HTTPS endpoint")

// Service manages EventSub subscriptions and snapshots. All EventSub
// calls use the app access token (client_credentials) — EventSub is
// app-scoped, not user-scoped.
type Service struct {
	repo        repository.Repository
	twitch      *twitch.Client
	callbackURL string
	secret      string
	log         *slog.Logger
}

func New(repo repository.Repository, tc *twitch.Client, callbackURL, secret string, log *slog.Logger) *Service {
	return &Service{
		repo:        repo,
		twitch:      tc,
		callbackURL: callbackURL,
		secret:      secret,
		log:         log.With("domain", "eventsub"),
	}
}

// SubscribeStreamOnline creates a stream.online v1 webhook subscription
// for the given broadcaster, or returns the existing active one when the
// (broadcaster, stream.online) pair already has a non-revoked sub. Twitch
// rejects duplicates server-side with 409, so the pre-check also avoids
// burning the rate limit on predictable failures.
func (s *Service) SubscribeStreamOnline(ctx context.Context, broadcasterID string) (*repository.Subscription, error) {
	return s.subscribe(ctx, "stream.online", "1", twitch.StreamOnlineCondition{BroadcasterUserID: broadcasterID}, broadcasterID)
}

// SubscribeStreamOffline creates a stream.offline v1 webhook
// subscription for the given broadcaster. Pairs with
// SubscribeStreamOnline: the two together make the SSE live-status
// delta feed authoritative for a channel — without .offline, the
// frontend's Set of live broadcasters would grow monotonically
// until the next full refetch.
func (s *Service) SubscribeStreamOffline(ctx context.Context, broadcasterID string) (*repository.Subscription, error) {
	return s.subscribe(ctx, "stream.offline", "1", twitch.StreamOfflineCondition{BroadcasterUserID: broadcasterID}, broadcasterID)
}

// SubscribeChannelUpdate creates a channel.update v2 webhook subscription
// for the given broadcaster, or returns the existing active one. Used by
// the downloader's webhook mode to get push-based title changes instead
// of polling. Idempotent via the existing-active-sub pre-check in
// subscribe().
func (s *Service) SubscribeChannelUpdate(ctx context.Context, broadcasterID string) (*repository.Subscription, error) {
	return s.subscribe(ctx, "channel.update", "2", twitch.ChannelUpdateCondition{BroadcasterUserID: broadcasterID}, broadcasterID)
}

// isSubAlive returns true for Twitch sub statuses where events will
// still be delivered. Anything else is a terminal-failure state that
// looks active in our mirror (revoked_at IS NULL) but delivers zero
// events — a "zombie" sub. The reconcile loop treats zombies as
// absent: it deletes the dead Twitch row + local mirror entry and
// creates a fresh sub in its place.
//
// Statuses that keep a sub alive:
//   - enabled: healthy, receiving events
//   - webhook_callback_verification_pending: transient, will become
//     enabled once Twitch's handshake completes
//
// Everything else (verification_failed, notification_failures_exceeded,
// authorization_revoked, moderator_removed, user_removed, version_removed)
// is effectively dead.
func isSubAlive(status string) bool {
	return status == "enabled" || status == "webhook_callback_verification_pending"
}

// ReconcileChannelSubs ensures every broadcaster in `channelIDs` has a
// live stream.online and stream.offline sub on Twitch, and deletes any
// sub whose broadcaster is no longer in the set. Also sweeps zombie
// subs — terminal-failure statuses that look active in our local
// mirror but deliver zero events — so the next create path produces a
// working replacement.
//
// Called on boot + periodically so the SSE live-dot feed stays
// authoritative for the curated channel list — without this the
// frontend has to choose between a polling fallback and a
// potentially-drifting cache.
//
// channel.update subs are NOT touched here: those are per-recording
// and reconciled separately via ReconcileChannelUpdateSubs.
//
// Best-effort per sub: a failed create/delete logs a warning and the
// sweep continues so one bad row doesn't block the rest of the
// reconciliation.
func (s *Service) ReconcileChannelSubs(ctx context.Context, channelIDs map[string]bool) error {
	// Early-out when the callback URL can't be used: without this,
	// the create loop produces one Helix 400 per channel ×2 sub
	// types — a ~100-channel dev setup on http://localhost:8080
	// produces 200+ error log lines per reconcile tick. One info
	// log makes the misconfig obvious without the spam.
	if !isCallbackURLUsable(s.callbackURL) {
		s.log.Info("skip channel-sub reconcile: callback URL is not a usable HTTPS endpoint",
			"callback_host", urlHost(s.callbackURL))
		return nil
	}
	// Two sub types, fetched separately so we don't have to filter
	// Twitch's mixed list client-side.
	onlineSubs, _, err := s.twitch.GetEventSubSubscriptionsAll(ctx, &twitch.GetEventSubSubscriptionsParams{Type: "stream.online"})
	if err != nil {
		return fmt.Errorf("eventsub reconcile: list stream.online: %w", err)
	}
	offlineSubs, _, err := s.twitch.GetEventSubSubscriptionsAll(ctx, &twitch.GetEventSubSubscriptionsParams{Type: "stream.offline"})
	if err != nil {
		return fmt.Errorf("eventsub reconcile: list stream.offline: %w", err)
	}

	// First pass: delete zombies and alive subs that point at an old
	// callback. After this, any sub still on Twitch's side for one of our
	// broadcasters is alive and uses this process's callback; missing means
	// we need to create.
	zombiesSwept := s.sweepZombies(ctx, onlineSubs) + s.sweepZombies(ctx, offlineSubs)
	staleOnlineSwept, staleOnlineBlocked := s.sweepStaleCallbacks(ctx, onlineSubs)
	staleOfflineSwept, staleOfflineBlocked := s.sweepStaleCallbacks(ctx, offlineSubs)
	staleCallbacksSwept := staleOnlineSwept + staleOfflineSwept

	// Re-index using only ALIVE subs on the current callback. The sweep calls
	// mutated nothing on the source slices directly, so we filter here. Stale
	// subs whose delete failed stay in the set so we do not create duplicates
	// while Twitch still has the old transport.
	haveOnline := subSetByBroadcasterAliveForCallback(onlineSubs, s.callbackURL, staleOnlineBlocked)
	haveOffline := subSetByBroadcasterAliveForCallback(offlineSubs, s.callbackURL, staleOfflineBlocked)

	created, err := s.createChannelSubs(ctx, planChannelSubCreates(channelIDs, haveOnline, haveOffline))
	if err != nil {
		return err
	}

	// Delete orphans: broadcasters we had a sub for but are no longer in the
	// channel set (row removed from the channels table). Sequential because
	// deletes are cheap and shouldn't contend with creates on the rate limit.
	deleted := s.deleteOrphanedSubs(ctx, haveOnline, channelIDs, "stream.online") +
		s.deleteOrphanedSubs(ctx, haveOffline, channelIDs, "stream.offline")

	if created > 0 || deleted > 0 || zombiesSwept > 0 || staleCallbacksSwept > 0 {
		s.log.Info("reconciled channel subs",
			"created", created, "deleted", deleted, "zombies_swept", zombiesSwept,
			"stale_callbacks_swept", staleCallbacksSwept,
			"channels", len(channelIDs))
	}
	return nil
}

// createReq is one planned subscription create: a (broadcaster, sub type) pair
// the reconcile pass found missing on Twitch.
type createReq struct {
	broadcasterID string
	subType       string
}

// planChannelSubCreates returns the creates needed to bring Twitch in line with
// channelIDs: a stream.online and/or stream.offline create for every broadcaster
// missing a live sub of that type. Pure given the have-sets, so the desired-vs-
// actual diff is unit-testable without touching Twitch.
func planChannelSubCreates(channelIDs map[string]bool, haveOnline, haveOffline map[string]twitch.EventSubSubscription) []createReq {
	reqs := make([]createReq, 0, len(channelIDs)*2)
	for bid := range channelIDs {
		if _, ok := haveOnline[bid]; !ok {
			reqs = append(reqs, createReq{broadcasterID: bid, subType: "stream.online"})
		}
		if _, ok := haveOffline[bid]; !ok {
			reqs = append(reqs, createReq{broadcasterID: bid, subType: "stream.offline"})
		}
	}
	return reqs
}

// createChannelSubs issues the planned subscribe calls concurrently and returns
// the number created.
//
// Parallelize creates: N channels × 2 sub types = 2N sequential POSTs would
// block boot for 10+ seconds on 50-channel setups. Cap concurrency at 10 so a
// large channel list can't swamp the Twitch rate limit (800 req/min = ~13
// concurrent is safe; 10 leaves headroom for other callers).
//
// Circuit breaker: after breakerThreshold consecutive non-transient failures we
// stop the reconcile and return an error. The failure modes we want to bail on:
//   - Helix 400 bad callback URL (config issue; retrying never helps — covered
//     by the pre-check but belt-and-suspenders catches a runtime scheme change)
//   - Helix 401/403 app-token rejection (token expired or revoked; burning
//     through N channels won't auth it)
//   - Helix 409 unexpected (our dedup missed something; safer to stop and let
//     the operator investigate)
//
// Transient 429 rate-limits retry with bounded backoff in twitch.Client; a 5xx
// on the create POST is surfaced rather than auto-retried (so a retry can't
// duplicate a subscription) and counts toward the breaker. We cancel the outer
// context to propagate stop to any in-flight goroutines.
func (s *Service) createChannelSubs(ctx context.Context, reqs []createReq) (int, error) {
	if len(reqs) == 0 {
		return 0, nil
	}
	const (
		createConcurrency = 10
		breakerThreshold  = 3
	)
	breakerCtx, breakerCancel := context.WithCancel(ctx)
	defer breakerCancel()

	sem := make(chan struct{}, createConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var created, consecutiveFailures int
	var breakerTripped bool

	for _, r := range reqs {
		if breakerCtx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(req createReq) {
			defer wg.Done()
			defer func() { <-sem }()
			err := s.subscribeByType(breakerCtx, req)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				s.log.Warn("reconcile: subscribe failed",
					"type", req.subType, "broadcaster_id", req.broadcasterID, "error", err)
				consecutiveFailures++
				if consecutiveFailures >= breakerThreshold && !breakerTripped {
					breakerTripped = true
					s.log.Error("reconcile: circuit breaker tripped; aborting remaining subscribes",
						"threshold", breakerThreshold,
						"remaining", len(reqs)-created-consecutiveFailures)
					breakerCancel()
				}
				return
			}
			created++
			consecutiveFailures = 0
		}(r)
	}
	wg.Wait()
	if breakerTripped {
		return created, fmt.Errorf("eventsub reconcile: %d consecutive subscribe failures, aborted", breakerThreshold)
	}
	return created, nil
}

// subscribeByType dispatches a planned create to the matching Subscribe* call.
func (s *Service) subscribeByType(ctx context.Context, req createReq) error {
	switch req.subType {
	case "stream.online":
		_, err := s.SubscribeStreamOnline(ctx, req.broadcasterID)
		return err
	case "stream.offline":
		_, err := s.SubscribeStreamOffline(ctx, req.broadcasterID)
		return err
	default:
		return fmt.Errorf("eventsub reconcile: unknown sub type %q", req.subType)
	}
}

// deleteOrphanedSubs revokes every sub in have whose broadcaster is no longer in
// channelIDs. Best-effort: a failed delete logs a warning and the sweep
// continues so one bad row doesn't block the rest. Returns the number deleted.
func (s *Service) deleteOrphanedSubs(ctx context.Context, have map[string]twitch.EventSubSubscription, channelIDs map[string]bool, subType string) int {
	var deleted int
	for bid, sub := range have {
		if channelIDs[bid] {
			continue
		}
		if err := s.Unsubscribe(ctx, sub.ID, "reconcile: broadcaster no longer in channels table"); err != nil {
			s.log.Warn("reconcile: delete orphan sub failed",
				"type", subType, "sub_id", sub.ID, "broadcaster_id", bid, "error", err)
			continue
		}
		deleted++
	}
	return deleted
}

// sweepZombies deletes any sub in a dead status from both Twitch and
// the local mirror so the next create path produces a working
// replacement. Returns the count deleted for observability.
func (s *Service) sweepZombies(ctx context.Context, subs []twitch.EventSubSubscription) int {
	var swept int
	for _, sub := range subs {
		if isSubAlive(sub.Status) {
			continue
		}
		if err := s.Unsubscribe(ctx, sub.ID, "reconcile: zombie sub: status="+sub.Status); err != nil {
			s.log.Warn("reconcile: delete zombie sub failed",
				"sub_id", sub.ID, "status", sub.Status, "error", err)
			continue
		}
		swept++
	}
	return swept
}

func (s *Service) sweepStaleCallbacks(ctx context.Context, subs []twitch.EventSubSubscription) (int, map[string]bool) {
	blocked := make(map[string]bool)
	var swept int
	for _, sub := range subs {
		if !isSubAlive(sub.Status) || subUsesCallback(&sub, s.callbackURL) {
			continue
		}
		if err := s.Unsubscribe(ctx, sub.ID, "reconcile: callback URL changed"); err != nil {
			s.log.Warn("reconcile: delete stale-callback sub failed",
				"sub_id", sub.ID, "callback_host", urlHost(subCallbackURL(&sub)), "error", err)
			blocked[sub.ID] = true
			continue
		}
		swept++
	}
	return swept, blocked
}

// subSetByBroadcasterAliveForCallback indexes a subscription list by
// broadcaster ID, keeping only entries in a live status and on this process's
// callback URL. Zombies (verification failed, notification failures exceeded,
// etc.) and stale callback transports are excluded so the reconcile caller treats
// them as absent and creates replacements. The separate sweep passes handle the
// Twitch-side delete.
func subSetByBroadcasterAliveForCallback(subs []twitch.EventSubSubscription, callbackURL string, keepStale map[string]bool) map[string]twitch.EventSubSubscription {
	out := make(map[string]twitch.EventSubSubscription, len(subs))
	for _, sub := range subs {
		if !isSubAlive(sub.Status) {
			continue
		}
		bid := broadcasterIDFromSub(&sub)
		if bid == "" {
			continue
		}
		if !subUsesCallback(&sub, callbackURL) && !keepStale[sub.ID] {
			continue
		}
		out[bid] = sub
	}
	return out
}

// UnsubscribeChannelUpdate revokes the channel.update sub for a
// broadcaster. Called when a recording ends. No-op when no active
// channel.update sub exists for the broadcaster (e.g. subscription
// failed at record start, or already cleaned up by boot reconcile).
func (s *Service) UnsubscribeChannelUpdate(ctx context.Context, broadcasterID, reason string) error {
	sub, err := s.repo.GetActiveSubscriptionForBroadcasterType(ctx, broadcasterID, "channel.update")
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("eventsub: lookup channel.update sub: %w", err)
	}
	return s.Unsubscribe(ctx, sub.ID, reason)
}

// ReconcileChannelUpdateSubs sweeps Twitch-side channel.update subs, deletes
// any that don't match the provided set of broadcasters with active recordings,
// and recreates active-recording subs that were tied to an old callback. Called
// at boot to clean up orphans left by a previous crash before the unsubscribe
// call landed.
//
// The sweep only touches channel.update subs — stream.online /
// stream.offline subs are managed elsewhere (schedule / EventSub
// service's own lifecycle) and would be catastrophic to revoke here.
func (s *Service) ReconcileChannelUpdateSubs(ctx context.Context, activeBroadcasterIDs map[string]bool) error {
	if !isCallbackURLUsable(s.callbackURL) {
		// No point listing + diffing if we can't re-create. The
		// service returns nil cleanly so main.go's boot reconcile
		// doesn't log as a failure.
		s.log.Info("skip channel.update reconcile: callback URL is not a usable HTTPS endpoint",
			"callback_host", urlHost(s.callbackURL))
		return nil
	}
	all, _, err := s.twitch.GetEventSubSubscriptionsAll(ctx, &twitch.GetEventSubSubscriptionsParams{Type: "channel.update"})
	if err != nil {
		return fmt.Errorf("eventsub reconcile: list twitch subs: %w", err)
	}

	current, blocked, deleted := s.reconcileExistingChannelUpdateSubs(ctx, all, activeBroadcasterIDs)
	created := s.createMissingChannelUpdateSubs(ctx, activeBroadcasterIDs, current, blocked)

	if deleted > 0 || created > 0 {
		s.log.Info("reconciled channel.update subscriptions", "deleted", deleted, "created", created)
	}
	return nil
}

// cuDecision is the reconcile verdict for one Twitch-side channel.update sub.
// A zero value (bid == "") means "not one of ours, skip it". Otherwise exactly
// one of isKeep / isDelete is set.
type cuDecision struct {
	bid         string // broadcaster the sub is scoped to, "" when none
	isKeep      bool   // alive, on the current callback, recording still active
	isDelete    bool   // should be revoked on Twitch
	reason      string // revoke reason, set when isDelete
	blockOnFail bool   // if the delete fails, skip recreating bid this pass
}

// classifyChannelUpdateSub decides what to do with one listed channel.update sub
// without performing any IO, so the branching is unit-testable in isolation:
//
//   - no broadcaster on the condition -> skip (not a per-channel sub)
//   - dead status -> delete as a zombie, block recreate until Twitch confirms
//   - wrong callback -> delete the stale transport, block recreate
//   - active recording on this callback -> keep
//   - otherwise -> delete as an orphan (recording already ended)
//
// blockOnFail is set for zombie/stale deletes because Twitch still holds the old
// sub until the delete lands; recreating in the same pass would 409 or
// duplicate. Orphans don't block: their broadcaster isn't in the active set, so
// the create pass skips it anyway.
func classifyChannelUpdateSub(sub *twitch.EventSubSubscription, callbackURL string, activeBroadcasterIDs map[string]bool) cuDecision {
	bid := broadcasterIDFromSub(sub)
	if bid == "" {
		return cuDecision{}
	}
	if !isSubAlive(sub.Status) {
		return cuDecision{bid: bid, isDelete: true, blockOnFail: true, reason: "boot reconcile: zombie sub: status=" + sub.Status}
	}
	if !subUsesCallback(sub, callbackURL) {
		return cuDecision{bid: bid, isDelete: true, blockOnFail: true, reason: "boot reconcile: callback URL changed"}
	}
	if activeBroadcasterIDs[bid] {
		return cuDecision{bid: bid, isKeep: true}
	}
	return cuDecision{bid: bid, isDelete: true, reason: "boot reconcile: no active recording"}
}

// reconcileExistingChannelUpdateSubs classifies every Twitch-side channel.update
// sub and acts on the verdict: keepers are recorded in current, deletes are
// revoked. A broadcaster whose delete failed is added to blocked so the create
// pass leaves it for the next reconcile. Returns (current, blocked, deleted).
func (s *Service) reconcileExistingChannelUpdateSubs(ctx context.Context, all []twitch.EventSubSubscription, activeBroadcasterIDs map[string]bool) (current, blocked map[string]bool, deleted int) {
	current = make(map[string]bool, len(activeBroadcasterIDs))
	blocked = make(map[string]bool)
	for i := range all {
		d := classifyChannelUpdateSub(&all[i], s.callbackURL, activeBroadcasterIDs)
		switch {
		case d.isKeep:
			current[d.bid] = true
		case d.isDelete:
			if s.revokeReconciledSub(ctx, all[i].ID, d) {
				deleted++
			} else if d.blockOnFail {
				blocked[d.bid] = true
			}
		}
	}
	return current, blocked, deleted
}

// revokeReconciledSub revokes one sub the reconcile pass decided to drop,
// returning whether the Twitch + mirror delete succeeded.
func (s *Service) revokeReconciledSub(ctx context.Context, id string, d cuDecision) bool {
	if err := s.Unsubscribe(ctx, id, d.reason); err != nil {
		s.log.Warn("reconcile: failed to delete channel.update sub",
			"sub_id", id, "broadcaster_id", d.bid, "reason", d.reason, "error", err)
		return false
	}
	return true
}

// createMissingChannelUpdateSubs subscribes every active-recording broadcaster
// that has no current sub and isn't blocked by a failed delete this pass.
// Returns the number created.
func (s *Service) createMissingChannelUpdateSubs(ctx context.Context, activeBroadcasterIDs, current, blocked map[string]bool) int {
	var created int
	for bid := range activeBroadcasterIDs {
		if current[bid] || blocked[bid] {
			continue
		}
		if _, err := s.SubscribeChannelUpdate(ctx, bid); err != nil {
			s.log.Warn("reconcile: failed to create missing channel.update sub",
				"broadcaster_id", bid, "error", err)
			continue
		}
		created++
	}
	return created
}

// isCallbackURLUsable verifies the callback URL will be accepted by
// Twitch's webhook transport (HTTPS, non-loopback host, standard port).
//
// Without this check, every subscribe call fails with a Helix 400 —
// on reconcile that means one 400 per channel, which we've seen spam
// the log in practice. Catching the most common homelab misconfig
// (running webhook mode on localhost:8080) before the Helix call
// happens keeps the log clean.
//
// The rule itself lives in config.IsUsableWebhookURL so startup
// validation and this runtime guard cannot drift; this is a thin alias
// kept for the local call sites.
func isCallbackURLUsable(raw string) bool {
	return config.IsUsableWebhookURL(raw)
}

func urlHost(raw string) string {
	return config.URLHost(raw)
}

func subUsesCallback(sub *twitch.EventSubSubscription, callbackURL string) bool {
	method, callback := transportFields(sub.Transport)
	return method == "webhook" && config.SameURL(callback, callbackURL)
}

func subCallbackURL(sub *twitch.EventSubSubscription) string {
	_, callback := transportFields(sub.Transport)
	return callback
}

// subscribe is the shared create path. It checks the local mirror first;
// if an active row exists it's returned as-is. Otherwise we create on
// Twitch, then mirror. If Twitch succeeds but the mirror insert fails,
// the next Snapshot() will self-heal by discovering the orphan.
func (s *Service) subscribe(ctx context.Context, subType, version string, cond twitch.EventSubCondition, broadcasterID string) (*repository.Subscription, error) {
	if !isCallbackURLUsable(s.callbackURL) {
		return nil, ErrCallbackURLNotUsable
	}
	existing, err := s.repo.GetActiveSubscriptionForBroadcasterType(ctx, broadcasterID, subType)
	if err == nil {
		if config.SameURL(existing.TransportCallback, s.callbackURL) {
			return existing, nil
		}
		if err := s.Unsubscribe(ctx, existing.ID, "callback URL changed"); err != nil {
			return nil, fmt.Errorf("eventsub: revoke stale callback sub: %w", err)
		}
	}
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("eventsub: lookup active sub: %w", err)
	}

	body := &twitch.CreateEventSubSubscriptionBody{
		Type:      subType,
		Version:   version,
		Condition: cond,
		Transport: twitch.WebhookTransport{
			Method:   "webhook",
			Callback: s.callbackURL,
			Secret:   s.secret,
		},
	}
	created, err := s.twitch.CreateEventSubSubscription(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("eventsub: create on twitch: %w", err)
	}
	if len(created) == 0 {
		return nil, fmt.Errorf("eventsub: twitch returned no subscription")
	}
	sub := created[0]

	condJSON, err := json.Marshal(sub.Condition)
	if err != nil {
		return nil, fmt.Errorf("eventsub: marshal condition: %w", err)
	}

	// broadcasterID is the value we passed INTO the condition — no need to
	// reflect it back out of Twitch's echo. An empty string means this
	// subscription type doesn't key on a broadcaster (e.g. drop grants).
	var bidPtr *string
	if broadcasterID != "" {
		bidPtr = &broadcasterID
	}

	// Mirror what Twitch stored, not what we sent — status in particular
	// transitions from webhook_callback_verification_pending to enabled
	// over the handshake round-trip. CreatedAt comes from Twitch so
	// drift-detection in a future cleanup task can compare against
	// local clock skew.
	method, callback := transportFields(sub.Transport)
	mirror, err := s.repo.CreateSubscription(ctx, &repository.SubscriptionInput{
		ID:                sub.ID,
		Status:            sub.Status,
		Type:              sub.Type,
		Version:           sub.Version,
		Cost:              int64(sub.Cost),
		Condition:         condJSON,
		BroadcasterID:     bidPtr,
		TransportMethod:   method,
		TransportCallback: callback,
		TwitchCreatedAt:   sub.CreatedAt,
	})
	if err != nil {
		// Twitch accepted the sub but we failed to mirror — next Snapshot
		// will rediscover it. Log loudly so operators know to check.
		s.log.Error("twitch accepted subscription but local mirror failed",
			"sub_id", sub.ID, "type", sub.Type, "error", err)
		return nil, fmt.Errorf("eventsub: mirror subscription: %w", err)
	}
	return mirror, nil
}

// Unsubscribe deletes a subscription on Twitch and marks the local row
// revoked. Idempotent: if the local row is already revoked or the
// Twitch DELETE returns 404, we continue through the mark step so a
// stale mirror converges on the next call.
func (s *Service) Unsubscribe(ctx context.Context, id, reason string) error {
	if err := s.twitch.DeleteEventSubSubscription(ctx, &twitch.DeleteEventSubSubscriptionParams{ID: id}); err != nil {
		// Helix 404 means Twitch doesn't have it — safe to proceed to
		// local soft-delete. Any other error (401, 5xx) bubbles up.
		var helixErr *twitch.HelixError
		if !errors.As(err, &helixErr) || helixErr.Status != 404 {
			return fmt.Errorf("eventsub: delete on twitch: %w", err)
		}
		s.log.Info("twitch DELETE returned 404; proceeding with local revoke", "sub_id", id)
	}
	if err := s.repo.MarkSubscriptionRevoked(ctx, id, reason); err != nil {
		return fmt.Errorf("eventsub: mark revoked: %w", err)
	}
	return nil
}

// RevokeAllActive deletes every locally active EventSub subscription from
// Twitch and marks the local mirrors revoked. Used after the process boots with
// an EventSub runtime that does not create Twitch subscriptions, so
// subscriptions from a previous enabled runtime do not keep consuming quota or
// delivering webhook notifications to an old callback.
//
// It attempts every subscription exactly once and returns the count revoked
// plus the joined errors for any that failed. A subscription whose revoke keeps
// failing (e.g. a Helix 5xx) therefore does not block the others: cleanup is
// best-effort and the next non-subscription runtime startup can retry the
// stragglers.
func (s *Service) RevokeAllActive(ctx context.Context, reason string) (int, error) {
	const batchSize = 100

	// Snapshot the active IDs first by paging read-only. Revoking while paging
	// would shift offsets, and re-listing from offset 0 each pass would
	// re-encounter any subscription whose revoke keeps failing — one
	// un-revocable sub would then stall cleanup of every other sub.
	//
	// ListActiveSubscriptions uses a stable created_at/id order so tied DB
	// timestamps do not reshuffle page boundaries. The offset paging is still
	// not strictly consistent: a concurrent insert/revoke between page reads
	// could skip a sub. That is acceptable here — disabling is owner-initiated
	// and rare, and the next non-subscription runtime startup can retry any sub
	// this pass misses.
	var ids []string
	for offset := 0; ; offset += batchSize {
		subs, err := s.repo.ListActiveSubscriptions(ctx, batchSize, offset)
		if err != nil {
			return 0, fmt.Errorf("eventsub: list active subscriptions: %w", err)
		}
		for i := range subs {
			ids = append(ids, subs[i].ID)
		}
		if len(subs) < batchSize {
			break
		}
	}

	revoked := 0
	var revokeErrs []error
	for _, id := range ids {
		if err := s.Unsubscribe(ctx, id, reason); err != nil {
			revokeErrs = append(revokeErrs, fmt.Errorf("%s: %w", id, err))
			continue
		}
		revoked++
	}
	if len(revokeErrs) > 0 {
		return revoked, errors.Join(revokeErrs...)
	}
	return revoked, nil
}

// Snapshot polls Twitch for all app subscriptions, records an eventsub_snapshots
// row with the quota fields, and links every sub to the snapshot with its
// cost/status AT poll time. Subs Twitch reports but we don't have mirrored
// locally are skipped with a warning — Phase 6 will add a self-heal that
// upserts orphans so historical snapshots remain complete.
func (s *Service) Snapshot(ctx context.Context) (*repository.EventSubSnapshot, error) {
	all, pag, err := s.twitch.GetEventSubSubscriptionsAll(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("eventsub: poll twitch: %w", err)
	}
	snap, err := s.repo.CreateEventSubSnapshot(ctx, int64(pag.Total), int64(pag.TotalCost), int64(pag.MaxCost))
	if err != nil {
		return nil, fmt.Errorf("eventsub: create snapshot: %w", err)
	}

	for _, sub := range all {
		// Self-heal orphans: if Twitch returns a sub we don't mirror
		// locally, upsert it so the junction link succeeds. Matches
		// the plan's Phase 6 self-heal — historical snapshots stay
		// complete instead of silently losing subs we didn't create.
		if _, err := s.repo.GetSubscription(ctx, sub.ID); err != nil {
			if !errors.Is(err, repository.ErrNotFound) {
				s.log.Error("snapshot sub lookup failed", "sub_id", sub.ID, "error", err)
				continue
			}
			s.log.Warn("snapshot self-healing untracked subscription",
				"sub_id", sub.ID, "type", sub.Type)
			condJSON, mErr := json.Marshal(sub.Condition)
			if mErr != nil {
				s.log.Error("marshal orphan condition", "sub_id", sub.ID, "error", mErr)
				continue
			}
			method, callback := transportFields(sub.Transport)
			bid := broadcasterIDFromSub(&sub)
			var bidPtr *string
			if bid != "" {
				bidPtr = &bid
			}
			if _, err := s.repo.UpsertSubscription(ctx, &repository.SubscriptionInput{
				ID:                sub.ID,
				Status:            sub.Status,
				Type:              sub.Type,
				Version:           sub.Version,
				Cost:              int64(sub.Cost),
				Condition:         condJSON,
				BroadcasterID:     bidPtr,
				TransportMethod:   method,
				TransportCallback: callback,
				TwitchCreatedAt:   sub.CreatedAt,
			}); err != nil {
				s.log.Error("self-heal upsert failed", "sub_id", sub.ID, "error", err)
				continue
			}
		}
		if err := s.repo.LinkSnapshotSubscription(ctx, snap.ID, sub.ID, int64(sub.Cost), sub.Status); err != nil {
			s.log.Error("snapshot link failed", "snapshot_id", snap.ID, "sub_id", sub.ID, "error", err)
			continue
		}
	}

	return snap, nil
}

// ListActiveSubscriptions returns non-revoked subscriptions paged for
// the operator dashboard. Shape matches the manager boundary: domain
// types, not tRPC DTOs.
func (s *Service) ListActiveSubscriptions(ctx context.Context, limit, offset int) ([]repository.Subscription, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	subs, err := s.repo.ListActiveSubscriptions(ctx, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list active subscriptions: %w", err)
	}
	total, err := s.repo.CountActiveSubscriptions(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count active subscriptions: %w", err)
	}
	return subs, total, nil
}

// ListSnapshots returns the newest-first window of quota snapshots.
// The dashboard renders a small chart; cap limit defaulting lives here
// so the transport layer doesn't re-derive it.
func (s *Service) ListSnapshots(ctx context.Context, limit, offset int) ([]repository.EventSubSnapshot, error) {
	if limit <= 0 {
		limit = 50
	}
	snaps, err := s.repo.ListEventSubSnapshots(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	return snaps, nil
}

// LatestSnapshot returns the most recent poll or (nil, nil) when none
// exists yet. The ErrNotFound→nil translation is intentional: the
// dashboard renders a "poll now" CTA for the zero state, so a 404
// here would just force the transport layer to do the same mapping.
func (s *Service) LatestSnapshot(ctx context.Context) (*repository.EventSubSnapshot, error) {
	snap, err := s.repo.GetLatestEventSubSnapshot(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("latest snapshot: %w", err)
	}
	return snap, nil
}

// broadcasterIDFromSub pulls the broadcaster_user_id off a condition
// via the scraper-emitted BroadcasterScopedCondition interface — no
// reflection, no JSON reparse. Subscription types without a broadcaster
// (drop.entitlement.grant, user.authorization.*) return empty string;
// the caller stores a NULL broadcaster_id for those.
func broadcasterIDFromSub(sub *twitch.EventSubSubscription) string {
	if b, ok := sub.Condition.(twitch.BroadcasterScopedCondition); ok {
		return b.GetBroadcasterUserID()
	}
	return ""
}

// transportFields extracts method+callback from an EventSubTransport, which
// is a sealed interface. Webhook is the only method v2 uses, but fall back
// gracefully if Twitch/config changes.
func transportFields(t twitch.EventSubTransport) (method, callback string) {
	switch v := t.(type) {
	case twitch.WebhookTransport:
		return v.Method, v.Callback
	case twitch.WebsocketTransport:
		// Session transports have no callback URL; store the session ID
		// under callback so the row is still readable without schema
		// changes. v2 doesn't subscribe via websocket — this is defense
		// in depth in case a future subscription goes through.
		return v.Method, v.SessionID
	case twitch.ConduitTransport:
		return v.Method, v.ConduitID
	default:
		if t != nil {
			return t.TransportMethod(), ""
		}
		return "", ""
	}
}
