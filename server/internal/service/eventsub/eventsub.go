// Package eventsub mirrors Twitch subscriptions, revocations, and quota snapshots locally.
package eventsub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync"

	"github.com/befabri/replayvod/server/internal/background"
	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// ErrCallbackURLNotUsable means Twitch would reject the configured webhook callback URL.
var ErrCallbackURLNotUsable = errors.New("eventsub: callback URL is not a valid HTTPS endpoint")

// Service manages EventSub subscriptions and quota snapshots with an app access token.
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

// SubscribeStreamOnline creates a stream.online subscription or returns its active mirror.
func (s *Service) SubscribeStreamOnline(ctx context.Context, broadcasterID string) (*repository.Subscription, error) {
	return s.subscribe(ctx, "stream.online", "1", twitch.StreamOnlineCondition{BroadcasterUserID: broadcasterID}, broadcasterID)
}

// SubscribeStreamOffline creates a stream.offline subscription or returns its active mirror.
func (s *Service) SubscribeStreamOffline(ctx context.Context, broadcasterID string) (*repository.Subscription, error) {
	return s.subscribe(ctx, "stream.offline", "1", twitch.StreamOfflineCondition{BroadcasterUserID: broadcasterID}, broadcasterID)
}

// SubscribeChannelUpdate creates a channel.update subscription or returns its active mirror.
func (s *Service) SubscribeChannelUpdate(ctx context.Context, broadcasterID string) (*repository.Subscription, error) {
	return s.subscribe(ctx, "channel.update", "2", twitch.ChannelUpdateCondition{BroadcasterUserID: broadcasterID}, broadcasterID)
}

// isSubAlive reports whether Twitch can still deliver events, including a pending handshake.
func isSubAlive(status string) bool {
	return status == "enabled" || status == "webhook_callback_verification_pending"
}

// ReconcileChannelSubs maintains stream.online and stream.offline subscriptions for channelIDs.
// It defers replacements whose deletion fails and stops creates after consecutive failures.
func (s *Service) ReconcileChannelSubs(ctx context.Context, channelIDs map[string]bool) error {
	// Reject unusable callbacks before issuing a failing Twitch request for every channel.
	if !isCallbackURLUsable(s.callbackURL) {
		s.log.Info("skip channel-sub reconcile: callback URL is not a usable HTTPS endpoint",
			"callback_host", urlHost(s.callbackURL))
		return nil
	}
	// Read mirrors before Twitch so a concurrent create is not mistaken for an absent subscription.
	localOnline, err := s.repo.ListSubscriptionsByType(ctx, "stream.online")
	if err != nil {
		return err
	}
	localOffline, err := s.repo.ListSubscriptionsByType(ctx, "stream.offline")
	if err != nil {
		return err
	}
	onlineSubs, _, err := s.twitch.GetEventSubSubscriptionsAll(ctx, &twitch.GetEventSubSubscriptionsParams{Type: "stream.online"})
	if err != nil {
		return fmt.Errorf("eventsub reconcile: list stream.online: %w", err)
	}
	offlineSubs, _, err := s.twitch.GetEventSubSubscriptionsAll(ctx, &twitch.GetEventSubSubscriptionsParams{Type: "stream.offline"})
	if err != nil {
		return fmt.Errorf("eventsub reconcile: list stream.offline: %w", err)
	}

	if err := s.retireMissingMirrors(ctx, localOnline, onlineSubs); err != nil {
		return err
	}
	if err := s.retireMissingMirrors(ctx, localOffline, offlineSubs); err != nil {
		return err
	}
	// Failed deletions leave remote keys occupied and must block their replacements.
	onlineZombies, blockedOnlineZombies := s.sweepZombies(ctx, onlineSubs)
	offlineZombies, blockedOfflineZombies := s.sweepZombies(ctx, offlineSubs)
	zombiesSwept := onlineZombies + offlineZombies
	staleOnlineSwept, staleOnlineBlocked := s.sweepStaleCallbacks(ctx, onlineSubs)
	staleOfflineSwept, staleOfflineBlocked := s.sweepStaleCallbacks(ctx, offlineSubs)
	staleCallbacksSwept := staleOnlineSwept + staleOfflineSwept
	maps.Copy(staleOnlineBlocked, blockedOnlineZombies)
	maps.Copy(staleOfflineBlocked, blockedOfflineZombies)

	// The source slices still contain deleted subscriptions, so exclude them before planning creates.
	haveOnline := subSetByBroadcasterAliveForCallback(onlineSubs, s.callbackURL, staleOnlineBlocked)
	haveOffline := subSetByBroadcasterAliveForCallback(offlineSubs, s.callbackURL, staleOfflineBlocked)

	created, err := s.createChannelSubs(ctx, planChannelSubCreates(channelIDs, haveOnline, haveOffline))

	// Delete sequentially to avoid competing with creates for Twitch rate limits.
	deleted := s.deleteOrphanedSubs(ctx, haveOnline, channelIDs, "stream.online") +
		s.deleteOrphanedSubs(ctx, haveOffline, channelIDs, "stream.offline")

	if created > 0 || deleted > 0 || zombiesSwept > 0 || staleCallbacksSwept > 0 {
		s.log.Info("reconciled channel subs",
			"created", created, "deleted", deleted, "zombies_swept", zombiesSwept,
			"stale_callbacks_swept", staleCallbacksSwept,
			"channels", len(channelIDs))
	}
	return err
}

func (s *Service) retireMissingMirrors(ctx context.Context, local []repository.Subscription, remote []twitch.EventSubSubscription) error {
	present := make(map[string]bool, len(remote))
	for _, sub := range remote {
		present[sub.ID] = true
	}
	for _, sub := range local {
		if !present[sub.ID] {
			if err := s.repo.MarkSubscriptionRevoked(ctx, sub.ID, "reconcile: subscription absent from Twitch"); err != nil {
				return err
			}
		}
	}
	return nil
}

type createReq struct {
	broadcasterID string
	subType       string
}

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

// createChannelSubs joins bounded workers and aborts after consecutive failures.
// Twitch retries rate limits, but does not retry create failures that could duplicate subscriptions.
func (s *Service) createChannelSubs(ctx context.Context, reqs []createReq) (int, error) {
	if len(reqs) == 0 {
		return 0, nil
	}
	const (
		createConcurrency = 10
		breakerThreshold  = 3
	)
	children := background.NewScope(ctx)
	defer children.Join()
	jobs := make(chan createReq)
	var mu sync.Mutex
	var created, consecutiveFailures int
	for worker := range min(createConcurrency, len(reqs)) {
		_ = children.Go(fmt.Sprintf("subscription worker %d", worker), true, func(childCtx context.Context) error {
			for {
				var req createReq
				select {
				case <-childCtx.Done():
					return childCtx.Err()
				case next, ok := <-jobs:
					if !ok {
						return nil
					}
					req = next
				}
				if err := childCtx.Err(); err != nil {
					return err
				}
				err := s.subscribeByType(childCtx, req)
				if childCtx.Err() != nil {
					return childCtx.Err()
				}
				mu.Lock()
				if err == nil {
					created++
					consecutiveFailures = 0
					mu.Unlock()
					continue
				}
				consecutiveFailures++
				tripped := consecutiveFailures >= breakerThreshold
				mu.Unlock()
				s.log.Warn("reconcile: subscribe failed", "type", req.subType,
					"broadcaster_id", req.broadcasterID, "error", err)
				if tripped {
					return fmt.Errorf("eventsub reconcile: %d consecutive subscribe failures, aborted", breakerThreshold)
				}
			}
		})
	}
enqueue:
	for _, req := range reqs {
		select {
		case <-children.Context().Done():
			break enqueue
		case jobs <- req:
		}
	}
	close(jobs)
	err := children.Wait()
	return created, errors.Join(err, ctx.Err())
}

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

// deleteOrphanedSubs attempts every orphan and returns the number successfully revoked.
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

// sweepZombies retires dead mirrors and returns occupied remote keys whose deletion failed.
func (s *Service) sweepZombies(ctx context.Context, subs []twitch.EventSubSubscription) (int, map[string]bool) {
	var swept int
	blocked := make(map[string]bool)
	for _, sub := range subs {
		if isSubAlive(sub.Status) {
			continue
		}
		reason := "reconcile: zombie sub: status=" + sub.Status
		if err := s.Unsubscribe(ctx, sub.ID, reason); err != nil {
			// Duplicate creates can trip the breaker and starve healthy channels.
			blocked[sub.ID] = true
			s.log.Warn("reconcile: delete zombie sub failed; deferring its replacement",
				"sub_id", sub.ID, "status", sub.Status, "error", err)
			if err := s.repo.MarkSubscriptionRevoked(ctx, sub.ID, reason+" (twitch delete failed)"); err != nil {
				s.log.Warn("reconcile: retire zombie mirror failed", "sub_id", sub.ID, "error", err)
				continue
			}
		}
		swept++
	}
	return swept, blocked
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

// subSetByBroadcasterAliveForCallback includes usable subscriptions and keys whose deletion failed.
func subSetByBroadcasterAliveForCallback(subs []twitch.EventSubSubscription, callbackURL string, keepStale map[string]bool) map[string]twitch.EventSubSubscription {
	out := make(map[string]twitch.EventSubSubscription, len(subs))
	for _, sub := range subs {
		if !isSubAlive(sub.Status) && !keepStale[sub.ID] {
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

// UnsubscribeChannelUpdate revokes the broadcaster's active subscription, if any.
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

// ReconcileChannelUpdateSubs maintains channel.update subscriptions for active recordings.
// It leaves stream.online and stream.offline subscriptions to ReconcileChannelSubs.
func (s *Service) ReconcileChannelUpdateSubs(ctx context.Context, activeBroadcasterIDs map[string]bool) error {
	if !isCallbackURLUsable(s.callbackURL) {
		// An unusable callback prevents replacement of any subscription deleted here.
		s.log.Info("skip channel.update reconcile: callback URL is not a usable HTTPS endpoint",
			"callback_host", urlHost(s.callbackURL))
		return nil
	}
	local, err := s.repo.ListSubscriptionsByType(ctx, "channel.update")
	if err != nil {
		return err
	}
	all, _, err := s.twitch.GetEventSubSubscriptionsAll(ctx, &twitch.GetEventSubSubscriptionsParams{Type: "channel.update"})
	if err != nil {
		return fmt.Errorf("eventsub reconcile: list twitch subs: %w", err)
	}
	if err := s.retireMissingMirrors(ctx, local, all); err != nil {
		return err
	}

	current, blocked, deleted := s.reconcileExistingChannelUpdateSubs(ctx, all, activeBroadcasterIDs)
	created := s.createMissingChannelUpdateSubs(ctx, activeBroadcasterIDs, current, blocked)

	if deleted > 0 || created > 0 {
		s.log.Info("reconciled channel.update subscriptions", "deleted", deleted, "created", created)
	}
	return nil
}

// cuDecision classifies a channel.update subscription; an empty bid means skip it.
type cuDecision struct {
	bid         string
	isKeep      bool
	isDelete    bool
	reason      string
	blockOnFail bool
}

// classifyChannelUpdateSub blocks recreation until Twitch releases a zombie or stale callback key.
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

func (s *Service) revokeReconciledSub(ctx context.Context, id string, d cuDecision) bool {
	if err := s.Unsubscribe(ctx, id, d.reason); err != nil {
		s.log.Warn("reconcile: failed to delete channel.update sub",
			"sub_id", id, "broadcaster_id", d.bid, "reason", d.reason, "error", err)
		return false
	}
	return true
}

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

// isCallbackURLUsable reports whether Twitch accepts the callback under startup validation rules.
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

// subscribe returns an active mirror or creates a subscription on Twitch.
// Snapshot repairs missing mirrors when a successful create cannot be persisted.
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

	// A NULL broadcaster ID represents subscription types that are not scoped to a broadcaster.
	var bidPtr *string
	if broadcasterID != "" {
		bidPtr = &broadcasterID
	}

	// Twitch may complete verification during the create request, so retain its returned status.
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
		s.log.Error("twitch accepted subscription but local mirror failed",
			"sub_id", sub.ID, "type", sub.Type, "error", err)
		return nil, fmt.Errorf("eventsub: mirror subscription: %w", err)
	}
	return mirror, nil
}

// Unsubscribe revokes the local mirror after Twitch confirms deletion or returns not found.
func (s *Service) Unsubscribe(ctx context.Context, id, reason string) error {
	if err := s.twitch.DeleteEventSubSubscription(ctx, &twitch.DeleteEventSubSubscriptionParams{ID: id}); err != nil {
		// Twitch reports an already deleted subscription as 404.
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

// RevokeAllActive attempts every active subscription and returns the count revoked and joined errors.
func (s *Service) RevokeAllActive(ctx context.Context, reason string) (int, error) {
	const batchSize = 100

	// Read IDs before revoking to avoid shifting offsets or repeatedly retrying failed deletions.
	// Concurrent inserts or revokes can still skip a row; a later startup retries those rows.
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

// Snapshot records Twitch quota usage and subscription status, repairing missing local mirrors.
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
		// A remote create can succeed without a local mirror; repair it before linking the snapshot.
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

// ListActiveSubscriptions returns a page of nonrevoked subscriptions and their total count.
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

// ListSnapshots returns quota snapshots, newest first.
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

// LatestSnapshot returns the most recent poll, or (nil, nil) if none exists.
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

// broadcasterIDFromSub returns an empty string for subscription types without a broadcaster.
func broadcasterIDFromSub(sub *twitch.EventSubSubscription) string {
	if b, ok := sub.Condition.(twitch.BroadcasterScopedCondition); ok {
		return b.GetBroadcasterUserID()
	}
	return ""
}

// transportFields returns the method and callback URL, session ID, or conduit ID.
func transportFields(t twitch.EventSubTransport) (method, callback string) {
	switch v := t.(type) {
	case twitch.WebhookTransport:
		return v.Method, v.Callback
	case twitch.WebsocketTransport:
		// The callback column stores the session ID for WebSocket transports.
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
