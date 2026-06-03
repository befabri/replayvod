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
	"github.com/befabri/replayvod/server/internal/server/api/apierr"
	eventsubsvc "github.com/befabri/replayvod/server/internal/service/eventsub"
	"github.com/befabri/replayvod/server/internal/service/eventsubconfig"
	"github.com/befabri/trpcgo"
)

// Handler is the tRPC adapter around the eventsub domain service.
type Handler struct {
	svc       *eventsubsvc.Service
	configSvc *eventsubconfig.Service
	log       *slog.Logger
}

// NewHandler creates a new eventsub tRPC handler.
func NewHandler(svc *eventsubsvc.Service, configSvc *eventsubconfig.Service, log *slog.Logger) *Handler {
	return &Handler{
		svc:       svc,
		configSvc: configSvc,
		log:       log.With("domain", "eventsub-api"),
	}
}

// ConfigResponse is the owner-facing EventSub setup state.
//
// The top-level fields describe the SAVED/desired config: what the dashboard
// form edits and what the process will run after a restart. When EnvManaged is
// true, environment variables own that config and app updates are rejected.
//
// Active describes the config the process is running RIGHT NOW. It diverges
// from the saved config when RestartRequired is true (e.g. relay was just saved
// onto a process that booted unconfigured). Live operations — creating Twitch
// subscriptions — are gated on Active, not the saved config, so a client can
// tell what actually works before the owner restarts.
type ConfigResponse struct {
	Source                     string        `json:"source"`
	Mode                       ServerMode    `json:"mode"`
	EnvManaged                 bool          `json:"env_managed"`
	SetupRequired              bool          `json:"setup_required"`
	RestartRequired            bool          `json:"restart_required"`
	CreatesTwitchSubscriptions bool          `json:"creates_twitch_subscriptions"`
	UsesRelayAgent             bool          `json:"uses_relay_agent"`
	PollsHelix                 bool          `json:"polls_helix"`
	WebhookCallbackURL         string        `json:"webhook_callback_url,omitempty"`
	RelayIngestURL             string        `json:"relay_ingest_url,omitempty"`
	RelaySubscribeURL          string        `json:"relay_subscribe_url,omitempty"`
	RelayLocalCallbackURL      string        `json:"relay_local_callback_url,omitempty"`
	Active                     ActiveRuntime `json:"active"`
}

// ActiveRuntime is the EventSub config the server is running now. Its capability
// flags reflect what the live process can do, which is what SubscribeStreamOnline
// gates on — distinct from the saved config's flags when a restart is pending.
type ActiveRuntime struct {
	Source                     string     `json:"source"`
	Mode                       ServerMode `json:"mode"`
	CreatesTwitchSubscriptions bool       `json:"creates_twitch_subscriptions"`
	UsesRelayAgent             bool       `json:"uses_relay_agent"`
	PollsHelix                 bool       `json:"polls_helix"`
}

func stateToResponse(state eventsubconfig.State) ConfigResponse {
	saved := state.Saved
	active := state.Active
	saved.Normalize()
	active.Normalize()
	// URL fields are emitted directly: saved configs are URL-cleared per
	// mode by ServerModeConfigFromApp, and env configs are validated, so the
	// fields a mode does not use are already empty (omitempty drops
	// them). No per-mode switch needed here — that rule lives only in
	// config.ServerModeConfig.ClearURLsForDelivery.
	return ConfigResponse{
		Source:                     saved.Source,
		Mode:                       ServerMode(saved.Mode),
		EnvManaged:                 saved.EnvManaged(),
		SetupRequired:              saved.SetupRequired(),
		RestartRequired:            state.RestartRequired,
		CreatesTwitchSubscriptions: saved.CreatesTwitchSubscriptions(),
		UsesRelayAgent:             saved.UsesRelayAgent(),
		PollsHelix:                 saved.PollsHelix(),
		WebhookCallbackURL:         saved.WebhookCallbackURL,
		RelayIngestURL:             saved.RelayIngestURL,
		RelaySubscribeURL:          saved.RelaySubscribeURL,
		RelayLocalCallbackURL:      saved.RelayLocalCallbackURL,
		Active: ActiveRuntime{
			Source:                     active.Source,
			Mode:                       ServerMode(active.Mode),
			CreatesTwitchSubscriptions: active.CreatesTwitchSubscriptions(),
			UsesRelayAgent:             active.UsesRelayAgent(),
			PollsHelix:                 active.PollsHelix(),
		},
	}
}

// Config returns the effective EventSub setup state. Env-managed settings win;
// otherwise it reports the app-saved server_settings row, if any.
func (h *Handler) Config(ctx context.Context) (ConfigResponse, error) {
	state, err := h.configSvc.State(ctx)
	if err != nil {
		return ConfigResponse{}, apierr.Map(h.log, err, "load EventSub config")
	}
	return stateToResponse(state), nil
}

type UpdateConfigInput struct {
	Mode                  string `json:"mode" validate:"required,oneof=off poll direct relay"`
	WebhookCallbackURL    string `json:"webhook_callback_url,omitempty"`
	RelayIngestURL        string `json:"relay_ingest_url,omitempty"`
	RelaySubscribeURL     string `json:"relay_subscribe_url,omitempty"`
	RelayLocalCallbackURL string `json:"relay_local_callback_url,omitempty"`
}

// UpdateConfig writes app-managed EventSub setup. It does not rewrite env
// variables and does not restart background relay/scheduler goroutines; callers
// should restart the server when RestartRequired is returned.
func (h *Handler) UpdateConfig(ctx context.Context, input UpdateConfigInput) (ConfigResponse, error) {
	state, err := h.configSvc.Update(ctx, eventsubconfig.UpdateInput{
		Mode:                  input.Mode,
		WebhookCallbackURL:    input.WebhookCallbackURL,
		RelayIngestURL:        input.RelayIngestURL,
		RelaySubscribeURL:     input.RelaySubscribeURL,
		RelayLocalCallbackURL: input.RelayLocalCallbackURL,
	})
	if err != nil {
		return ConfigResponse{}, apierr.Map(h.log, err, "save server mode config",
			apierr.On(eventsubconfig.ErrEnvManaged, trpcgo.CodeBadRequest, "Server mode is managed by environment variables"),
			apierr.OnVerbatim(eventsubconfig.ErrInvalid, trpcgo.CodeBadRequest))
	}
	return stateToResponse(state), nil
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
		return ListSubscriptionsResponse{}, apierr.Map(h.log, err, "list subscriptions")
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
		return ListSnapshotsResponse{}, apierr.Map(h.log, err, "list snapshots")
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
		return LatestSnapshotResponse{}, apierr.Map(h.log, err, "load snapshot")
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
		return SnapshotResponse{}, apierr.Map(h.log, err, "poll twitch")
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
	if !h.configSvc.Active().CreatesTwitchSubscriptions() {
		return SubscriptionResponse{}, trpcgo.NewError(trpcgo.CodeBadRequest, "Server mode is not configured for Twitch subscriptions")
	}
	sub, err := h.svc.SubscribeStreamOnline(ctx, input.BroadcasterID)
	if err != nil {
		return SubscriptionResponse{}, apierr.Map(h.log, err, "create subscription",
			apierr.On(eventsubsvc.ErrCallbackURLNotUsable, trpcgo.CodeBadRequest,
				"callback URL is not a valid HTTPS endpoint"))
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
		return UnsubscribeResponse{}, apierr.Map(h.log, err, "revoke subscription")
	}
	return UnsubscribeResponse{ID: input.ID}, nil
}
