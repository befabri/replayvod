// Package eventsubservice wraps the generated Twitch EventSub client with
// local-mirror bookkeeping: every successful subscription create is
// reflected in the subscriptions table, snapshots record quota usage over
// time, and revocations soft-delete rather than drop.
//
// The service uses the app access token (client_credentials) for all
// EventSub calls — EventSub is an app-scoped API, not a user-scoped one.
package eventsubservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// Service manages EventSub subscriptions and snapshots.
type Service struct {
	repo        repository.Repository
	twitch      *twitch.Client
	callbackURL string
	secret      string
	log         *slog.Logger
}

// New builds an EventSub service. callbackURL is the public URL Twitch
// will POST to; secret is the HMAC secret Twitch will sign deliveries with.
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

// subscribe is the shared create path. It checks the local mirror first;
// if an active row exists it's returned as-is. Otherwise we create on
// Twitch, then mirror. If Twitch succeeds but the mirror insert fails,
// the next Snapshot() will self-heal by discovering the orphan.
func (s *Service) subscribe(ctx context.Context, subType, version string, cond twitch.EventSubCondition, broadcasterID string) (*repository.Subscription, error) {
	existing, err := s.repo.GetActiveSubscriptionForBroadcasterType(ctx, broadcasterID, subType)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
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
		// LinkSnapshotSubscription FKs into subscriptions(id). Orphans
		// (subs Twitch has but we never mirrored) would error on FK
		// violation. Skip with a warning so the snapshot as a whole
		// still records — a partial linkage is better than no snapshot.
		if _, err := s.repo.GetSubscription(ctx, sub.ID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				s.log.Warn("snapshot saw untracked subscription, skipping link",
					"sub_id", sub.ID, "type", sub.Type)
				continue
			}
			s.log.Error("snapshot sub lookup failed", "sub_id", sub.ID, "error", err)
			continue
		}
		if err := s.repo.LinkSnapshotSubscription(ctx, snap.ID, sub.ID, int64(sub.Cost), sub.Status); err != nil {
			s.log.Error("snapshot link failed", "snapshot_id", snap.ID, "sub_id", sub.ID, "error", err)
			continue
		}
	}

	return snap, nil
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

