// Package eventsub implements the eventsub.* tRPC procedures. All are
// owner-only: subscription creation burns Twitch quota, snapshots poll
// Helix, and the dashboard surfaces sensitive operational state.
//
// The domain service lives in internal/service/eventsub because the
// scheduler cron task also uses it — it's genuinely cross-domain.
package eventsub

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/service/eventsub"
	"github.com/befabri/trpcgo"
)

// Handler is the tRPC adapter around the eventsub domain service.
type Handler struct {
	svc *eventsub.Service
	log *slog.Logger
}

// NewHandler creates a new eventsub tRPC handler.
func NewHandler(svc *eventsub.Service, log *slog.Logger) *Handler {
	return &Handler{
		svc: svc,
		log: log.With("domain", "eventsub-api"),
	}
}

// SubscriptionResponse is the wire shape for a Subscription row.
type SubscriptionResponse struct {
	ID                string          `json:"id"`
	Status            string          `json:"status"`
	Type              string          `json:"type"`
	Version           string          `json:"version"`
	Cost              int64           `json:"cost"`
	Condition         json.RawMessage `json:"condition"`
	BroadcasterID     *string         `json:"broadcaster_id,omitempty"`
	TransportMethod   string          `json:"transport_method"`
	TransportCallback string          `json:"transport_callback"`
	TwitchCreatedAt   time.Time       `json:"twitch_created_at"`
	CreatedAt         time.Time       `json:"created_at"`
	RevokedAt         *time.Time      `json:"revoked_at,omitempty"`
	RevokedReason     *string         `json:"revoked_reason,omitempty"`
}

func subToResponse(s *repository.Subscription) SubscriptionResponse {
	return SubscriptionResponse{
		ID:                s.ID,
		Status:            s.Status,
		Type:              s.Type,
		Version:           s.Version,
		Cost:              s.Cost,
		Condition:         s.Condition,
		BroadcasterID:     s.BroadcasterID,
		TransportMethod:   s.TransportMethod,
		TransportCallback: s.TransportCallback,
		TwitchCreatedAt:   s.TwitchCreatedAt,
		CreatedAt:         s.CreatedAt,
		RevokedAt:         s.RevokedAt,
		RevokedReason:     s.RevokedReason,
	}
}

// SnapshotResponse is the wire shape for the quota poll row.
type SnapshotResponse struct {
	ID           int64     `json:"id"`
	Total        int64     `json:"total"`
	TotalCost    int64     `json:"total_cost"`
	MaxTotalCost int64     `json:"max_total_cost"`
	FetchedAt    time.Time `json:"fetched_at"`
}

func snapshotToResponse(s *repository.EventSubSnapshot) SnapshotResponse {
	return SnapshotResponse{
		ID:           s.ID,
		Total:        s.Total,
		TotalCost:    s.TotalCost,
		MaxTotalCost: s.MaxTotalCost,
		FetchedAt:    s.FetchedAt,
	}
}

type ListInput struct {
	Limit  int `json:"limit" validate:"min=0,max=200"`
	Offset int `json:"offset" validate:"min=0"`
}

type ListSubscriptionsResponse struct {
	Data  []SubscriptionResponse `json:"data"`
	Total int64                  `json:"total"`
}

// ListSubscriptions returns active (non-revoked) subscriptions. The total
// count mirrors active_subs, which the dashboard's cost card uses alongside
// the latest snapshot's total_cost.
func (h *Handler) ListSubscriptions(ctx context.Context, input ListInput) (ListSubscriptionsResponse, error) {
	subs, total, err := h.svc.ListActiveSubscriptions(ctx, input.Limit, input.Offset)
	if err != nil {
		h.log.Error("list active subs", "error", err)
		return ListSubscriptionsResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list subscriptions")
	}
	data := make([]SubscriptionResponse, len(subs))
	for i := range subs {
		data[i] = subToResponse(&subs[i])
	}
	return ListSubscriptionsResponse{Data: data, Total: total}, nil
}

type ListSnapshotsResponse struct {
	Data []SnapshotResponse `json:"data"`
}

// ListSnapshots returns the newest snapshots first; the dashboard renders
// a small chart of cost over time. Cap the page size at 200 to keep the
// default listing cheap.
func (h *Handler) ListSnapshots(ctx context.Context, input ListInput) (ListSnapshotsResponse, error) {
	snaps, err := h.svc.ListSnapshots(ctx, input.Limit, input.Offset)
	if err != nil {
		h.log.Error("list snapshots", "error", err)
		return ListSnapshotsResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list snapshots")
	}
	data := make([]SnapshotResponse, len(snaps))
	for i := range snaps {
		data[i] = snapshotToResponse(&snaps[i])
	}
	return ListSnapshotsResponse{Data: data}, nil
}

type LatestSnapshotResponse struct {
	Snapshot *SnapshotResponse `json:"snapshot,omitempty"`
}

// LatestSnapshot returns the most recent poll, or null when no snapshot
// has ever been recorded (fresh install, before first Snapshot()). The
// dashboard renders a "poll now" button for this null case.
func (h *Handler) LatestSnapshot(ctx context.Context) (LatestSnapshotResponse, error) {
	snap, err := h.svc.LatestSnapshot(ctx)
	if err != nil {
		h.log.Error("latest snapshot", "error", err)
		return LatestSnapshotResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load snapshot")
	}
	if snap == nil {
		return LatestSnapshotResponse{}, nil
	}
	r := snapshotToResponse(snap)
	return LatestSnapshotResponse{Snapshot: &r}, nil
}

// Snapshot triggers a manual poll. The scheduled (Phase 6) task runs the
// same code path; exposing this as a mutation lets operators force a
// refresh without waiting for the tick.
func (h *Handler) Snapshot(ctx context.Context) (SnapshotResponse, error) {
	snap, err := h.svc.Snapshot(ctx)
	if err != nil {
		h.log.Error("manual snapshot", "error", err)
		return SnapshotResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to poll twitch")
	}
	return snapshotToResponse(snap), nil
}

type SubscribeInput struct {
	BroadcasterID string `json:"broadcaster_id" validate:"required"`
}

// SubscribeStreamOnline creates a stream.online subscription for the given
// channel. Dedups via the local mirror, so repeated calls with the same
// broadcaster return the existing sub rather than burning quota.
func (h *Handler) SubscribeStreamOnline(ctx context.Context, input SubscribeInput) (SubscriptionResponse, error) {
	sub, err := h.svc.SubscribeStreamOnline(ctx, input.BroadcasterID)
	if err != nil {
		h.log.Error("subscribe stream.online", "broadcaster", input.BroadcasterID, "error", err)
		return SubscriptionResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to create subscription")
	}
	return subToResponse(sub), nil
}

type UnsubscribeInput struct {
	ID     string `json:"id" validate:"required"`
	Reason string `json:"reason,omitempty"`
}

type UnsubscribeResponse struct {
	ID string `json:"id"`
}

// Unsubscribe revokes a subscription (calls Twitch DELETE + local
// soft-delete). Reason is surfaced in the audit log and defaults to
// "manual" — useful when the dashboard triggers this vs. Twitch-initiated
// revocation.
func (h *Handler) Unsubscribe(ctx context.Context, input UnsubscribeInput) (UnsubscribeResponse, error) {
	reason := input.Reason
	if reason == "" {
		reason = "manual"
	}
	if err := h.svc.Unsubscribe(ctx, input.ID, reason); err != nil {
		h.log.Error("unsubscribe", "id", input.ID, "error", err)
		return UnsubscribeResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to revoke subscription")
	}
	return UnsubscribeResponse{ID: input.ID}, nil
}
