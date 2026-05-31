// Package recordingwebhook implements the recordingWebhook.* tRPC procedures.
// All are owner-only: the config holds a process-wide signing secret and the
// target URL is a server-side egress, neither of which a viewer may read or
// change. This is the owner-managed surface, the same trust level as the
// EventSub server-mode config — deliberately NOT the viewer-level per-user
// settings routes.
package recordingwebhook

import (
	"context"
	"errors"
	"log/slog"
	"time"

	svc "github.com/befabri/replayvod/server/internal/recordingwebhook"
	"github.com/befabri/trpcgo"
)

// sender is the slice of the dispatcher the API needs: fire a one-off test
// delivery, and read the recent delivery log. Kept as an interface so the
// handler is exercisable with a fake and does not pull the whole dispatcher.
type sender interface {
	SendTest(ctx context.Context) svc.DeliveryResult
	RecentDeliveries(ctx context.Context) ([]svc.DeliveryRecord, error)
	RetryDelivery(ctx context.Context, id int64) (svc.DeliveryRecord, error)
}

// Handler is the tRPC adapter around the recording-webhook config service and
// the live dispatcher (for test sends and delivery history).
type Handler struct {
	svc      *svc.Service
	dispatch sender
	log      *slog.Logger
}

// NewHandler builds the handler. dispatch may be nil when no dispatcher is
// wired (e.g. a process with the webhook feature inert); test/deliveries then
// degrade gracefully rather than panic.
func NewHandler(service *svc.Service, dispatch sender, log *slog.Logger) *Handler {
	return &Handler{svc: service, dispatch: dispatch, log: log.With("domain", "recording-webhook-api")}
}

// RecordingWebhookConfigResponse is the owner-facing webhook config. Secret is
// returned in full (owner-only route): the owner needs it to configure
// verification on the receiving end. The type name is deliberately qualified —
// trpcgo flattens every domain's structs into one TypeScript namespace, so a
// bare ConfigResponse would collide with the eventsub domain's.
type RecordingWebhookConfigResponse struct {
	Enabled bool     `json:"enabled"`
	URL     string   `json:"url"`
	Secret  string   `json:"secret"`
	Events  []string `json:"events"`
}

func toResponse(c svc.Config) RecordingWebhookConfigResponse {
	events := c.Events
	if events == nil {
		events = []string{}
	}
	return RecordingWebhookConfigResponse{
		Enabled: c.Enabled,
		URL:     c.URL,
		Secret:  c.Secret,
		Events:  events,
	}
}

// Config returns the current webhook configuration.
func (h *Handler) Config(ctx context.Context) (RecordingWebhookConfigResponse, error) {
	cfg, err := h.svc.Get(ctx)
	if err != nil {
		h.log.Error("get recording webhook config", "error", err)
		return RecordingWebhookConfigResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load webhook config")
	}
	return toResponse(cfg), nil
}

// RecordingWebhookUpdateConfigInput is the owner update payload. Secret is
// intentionally absent: it is managed server-side (auto-generated when first
// enabled, rotated via the separate regenerateSecret procedure) so a long-lived
// signing key never round-trips through the client. Events is the allowlist of
// terminal events to fire; at least one is required when enabled (an enabled
// webhook with no events is rejected rather than treated as "all", so a direct
// API consumer can't send [] meaning "none" and silently get everything). Name
// qualified for the same reason as RecordingWebhookConfigResponse.
type RecordingWebhookUpdateConfigInput struct {
	Enabled bool     `json:"enabled"`
	URL     string   `json:"url,omitempty"`
	Events  []string `json:"events,omitempty" validate:"omitempty,max=8,dive,oneof=recording.completed recording.failed"`
}

// UpdateConfig validates and persists the webhook configuration. It takes
// effect immediately — the dispatcher reads the live config on each delivery —
// so there is no restart-required state.
func (h *Handler) UpdateConfig(ctx context.Context, input RecordingWebhookUpdateConfigInput) (RecordingWebhookConfigResponse, error) {
	cfg, err := h.svc.Update(ctx, svc.UpdateInput{
		Enabled: input.Enabled,
		URL:     input.URL,
		Events:  input.Events,
	})
	if err != nil {
		if errors.Is(err, svc.ErrInvalid) {
			return RecordingWebhookConfigResponse{}, trpcgo.NewError(trpcgo.CodeBadRequest, err.Error())
		}
		h.log.Error("update recording webhook config", "error", err)
		return RecordingWebhookConfigResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to save webhook config")
	}
	return toResponse(cfg), nil
}

// RegenerateSecret rotates the signing secret and returns the saved config
// (with the new secret). Separate from UpdateConfig so rotating a key is always
// a deliberate, standalone action, never a side effect of saving the form.
func (h *Handler) RegenerateSecret(ctx context.Context) (RecordingWebhookConfigResponse, error) {
	cfg, err := h.svc.RegenerateSecret(ctx)
	if err != nil {
		h.log.Error("regenerate recording webhook secret", "error", err)
		return RecordingWebhookConfigResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to regenerate secret")
	}
	return toResponse(cfg), nil
}

// RecordingWebhookTestResult is the synchronous outcome of a test delivery, so
// the dashboard can show whether the receiver answered without waiting for a
// real recording.
type RecordingWebhookTestResult struct {
	OK     bool   `json:"ok"`
	Status int    `json:"status"`
	Error  string `json:"error,omitempty"`
}

// TestDelivery fires a one-off signed test payload at the configured receiver
// and reports the result. Owner-only, like the rest of this surface.
func (h *Handler) TestDelivery(ctx context.Context) (RecordingWebhookTestResult, error) {
	if h.dispatch == nil {
		return RecordingWebhookTestResult{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "webhook dispatcher is not running")
	}
	r := h.dispatch.SendTest(ctx)
	return RecordingWebhookTestResult{OK: r.OK, Status: r.Status, Error: r.Error}, nil
}

// RecordingWebhookDeliveryResponse is one recent delivery surfaced to the
// dashboard. Metadata only: no payload body, no secret, no raw URL.
type RecordingWebhookDeliveryResponse struct {
	ID        int64  `json:"id"`
	Time      string `json:"time"`
	Event     string `json:"event"`
	VideoID   int64  `json:"video_id"`
	Outcome   string `json:"outcome"`
	Status    int    `json:"status"`
	Attempts  int    `json:"attempts"`
	Error     string `json:"error,omitempty"`
	Test      bool   `json:"test,omitempty"`
	MessageID string `json:"message_id"`
}

// Deliveries returns the recent delivery log, newest first, for the dashboard's
// delivery-status view. Empty when no dispatcher is wired.
func (h *Handler) Deliveries(ctx context.Context) ([]RecordingWebhookDeliveryResponse, error) {
	if h.dispatch == nil {
		return []RecordingWebhookDeliveryResponse{}, nil
	}
	recs, err := h.dispatch.RecentDeliveries(ctx)
	if err != nil {
		h.log.Error("list recording webhook deliveries", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load webhook deliveries")
	}
	out := make([]RecordingWebhookDeliveryResponse, len(recs))
	for i, r := range recs {
		out[i] = RecordingWebhookDeliveryResponse{
			// Time is RFC3339Nano for sub-second display precision; the
			// dashboard keys the list on ID, not this timestamp.
			ID:        r.ID,
			Time:      r.Time.Format(time.RFC3339Nano),
			Event:     r.Event,
			VideoID:   r.VideoID,
			Outcome:   string(r.Outcome),
			Status:    r.Status,
			Attempts:  r.Attempts,
			Error:     r.Error,
			Test:      r.Test,
			MessageID: r.MessageID,
		}
	}
	return out, nil
}

type RecordingWebhookRetryDeliveryInput struct {
	ID int64 `json:"id"`
}

func (h *Handler) RetryDelivery(ctx context.Context, input RecordingWebhookRetryDeliveryInput) (RecordingWebhookDeliveryResponse, error) {
	if h.dispatch == nil {
		return RecordingWebhookDeliveryResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "webhook dispatcher is not running")
	}
	rec, err := h.dispatch.RetryDelivery(ctx, input.ID)
	if err != nil {
		if errors.Is(err, svc.ErrDeliveryNotRetryable) {
			return RecordingWebhookDeliveryResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "delivery not found or not in a retryable state")
		}
		h.log.Error("retry recording webhook delivery", "id", input.ID, "error", err)
		return RecordingWebhookDeliveryResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to retry webhook delivery")
	}
	return deliveryResponse(rec), nil
}

func deliveryResponse(r svc.DeliveryRecord) RecordingWebhookDeliveryResponse {
	return RecordingWebhookDeliveryResponse{
		ID:        r.ID,
		Time:      r.Time.Format(time.RFC3339Nano),
		Event:     r.Event,
		VideoID:   r.VideoID,
		Outcome:   string(r.Outcome),
		Status:    r.Status,
		Attempts:  r.Attempts,
		Error:     r.Error,
		Test:      r.Test,
		MessageID: r.MessageID,
	}
}
