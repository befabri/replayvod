// Package webhook handles incoming Twitch EventSub webhook deliveries.
//
// Twitch verifies endpoint ownership with a challenge round-trip and then
// POSTs notifications signed with HMAC-SHA256 over id‖timestamp‖body. The
// handler re-computes the HMAC from the raw body (never the parsed JSON —
// JSON round-trip changes the byte sequence and invalidates the signature),
// rejects replay attempts outside the 10-minute window, dedups by
// Twitch-Eventsub-Message-Id, and dispatches notifications to a domain
// EventProcessor.
package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/go-chi/chi/v5"
)

// EventProcessor is the hook the webhook handler uses to dispatch a decoded
// notification to domain logic (schedule matcher, auto-download trigger).
//
// Implementations must be safe for concurrent use. The handler calls Process
// synchronously so the request can return 2xx only after Process commits
// side effects — retries from Twitch on handler timeout are de-duplicated
// by the ON CONFLICT on webhook_events.event_id.
type EventProcessor interface {
	Process(ctx context.Context, n *twitch.EventSubNotification) error
}

// Handler serves POST /api/v1/webhook/callback.
type Handler struct {
	repo       repository.Repository
	hmacSecret string
	processor  EventProcessor
	log        *slog.Logger
	maxAge     time.Duration
}

// NewHandler builds a webhook handler. maxAge of 0 uses
// twitch.DefaultEventSubMessageMaxAge (10 minutes, per Twitch spec).
func NewHandler(repo repository.Repository, hmacSecret string, processor EventProcessor, log *slog.Logger) *Handler {
	return &Handler{
		repo:       repo,
		hmacSecret: hmacSecret,
		processor:  processor,
		log:        log.With("domain", "webhook"),
	}
}

// SetupRoutes registers the webhook route under /api/v1.
func (h *Handler) SetupRoutes(r chi.Router) {
	r.Post("/webhook/callback", h.handleCallback)
}

func (h *Handler) handleCallback(w http.ResponseWriter, r *http.Request) {
	// Reading the raw body first is load-bearing: HMAC covers the literal
	// bytes Twitch sent, so we must never let middleware parse or re-encode
	// the body between now and VerifyEventSubSignature.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.log.Warn("failed to read webhook body", "error", err)
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()

	if err := twitch.VerifyEventSubSignature(r.Header, body, h.hmacSecret, h.maxAge); err != nil {
		h.logVerifyError(err, r)
		status := http.StatusForbidden
		// Missing-headers / malformed-timestamp errors are client errors
		// of a different kind — 400 is the truthful answer, but 403 is
		// safer because it reveals less; stick with 403 for the signature
		// path and 400 only for the truly malformed request shape.
		if !errors.Is(err, twitch.ErrEventSubSignatureMismatch) &&
			!errors.Is(err, twitch.ErrEventSubMessageReplay) {
			status = http.StatusBadRequest
		}
		http.Error(w, "invalid webhook", status)
		return
	}

	notif, err := twitch.DecodeEventSubWebhook(r.Header, body)
	if err != nil {
		h.log.Warn("failed to decode webhook", "error", err)
		http.Error(w, "invalid webhook", http.StatusBadRequest)
		return
	}

	eventID := r.Header.Get(twitch.EventSubHeaderMessageID)
	ts := parseHeaderTimestamp(r.Header.Get(twitch.EventSubHeaderMessageTimestamp))

	input := buildEventInput(eventID, ts, body, notif)

	// DB writes use WithoutCancel so they survive a client/proxy that drops
	// the connection after our handler starts. Without this, a cancelled
	// request could record the event on one side and fail to mark it
	// processed on the other, leaving us with phantom "received" rows.
	dbCtx := context.WithoutCancel(r.Context())

	switch notif.MessageType {
	case twitch.MsgTypeVerification:
		h.handleVerification(w, dbCtx, input, notif.Challenge)
	case twitch.MsgTypeRevocation:
		h.handleRevocation(w, dbCtx, input, &notif.Subscription)
	case twitch.MsgTypeNotification:
		h.handleNotification(w, dbCtx, input, notif)
	default:
		// DecodeEventSubWebhook already rejected unknown types, but be
		// defensive — this is a security boundary.
		h.log.Warn("unknown webhook message type", "type", notif.MessageType)
		http.Error(w, "unknown message type", http.StatusBadRequest)
	}
}

func (h *Handler) handleVerification(w http.ResponseWriter, ctx context.Context, input *repository.WebhookEventInput, challenge string) {
	if _, err := h.repo.CreateWebhookEvent(ctx, input); err != nil && !errors.Is(err, repository.ErrNotFound) {
		// A failure to record shouldn't prevent verification — Twitch
		// needs our challenge echo or the subscription is DOA. Log and
		// continue.
		h.log.Error("failed to record verification event", "error", err, "event_id", input.EventID)
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(challenge))
}

func (h *Handler) handleRevocation(w http.ResponseWriter, ctx context.Context, input *repository.WebhookEventInput, sub *twitch.EventSubSubscription) {
	if _, err := h.repo.CreateWebhookEvent(ctx, input); err != nil && !errors.Is(err, repository.ErrNotFound) {
		h.log.Error("failed to record revocation event", "error", err, "event_id", input.EventID)
	}
	if sub.ID != "" {
		// sub.Status holds Twitch's reason (authorization_revoked, etc.).
		if err := h.repo.MarkSubscriptionRevoked(ctx, sub.ID, sub.Status); err != nil {
			h.log.Error("failed to mark subscription revoked", "id", sub.ID, "reason", sub.Status, "error", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleNotification(w http.ResponseWriter, ctx context.Context, input *repository.WebhookEventInput, notif *twitch.EventSubNotification) {
	event, err := h.repo.CreateWebhookEvent(ctx, input)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// ON CONFLICT DO NOTHING fired — we already processed this
			// Message-Id. Twitch retries on delivery failure with the
			// same id, so this path is load-bearing for de-dup.
			h.log.Debug("duplicate webhook event, dedupd", "event_id", input.EventID)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.log.Error("failed to record notification event", "error", err, "event_id", input.EventID)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if h.processor == nil {
		// No processor wired — the event is recorded but no-one acts on
		// it. Treat as success so Twitch doesn't retry; dashboard surfaces
		// the row in the audit log. This is the bootstrap-phase state.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if procErr := h.processor.Process(ctx, notif); procErr != nil {
		h.log.Error("event processor failed",
			"error", procErr, "event_id", input.EventID,
			"event_type", notif.Subscription.Type)
		if mark := h.repo.MarkWebhookEventFailed(ctx, event.ID, procErr.Error()); mark != nil {
			h.log.Error("failed to mark webhook failed", "error", mark, "id", event.ID)
		}
		// Still return 204 — Twitch retrying doesn't fix our processor.
		// The dashboard's failed-events view surfaces this for inspection.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.repo.MarkWebhookEventProcessed(ctx, event.ID); err != nil {
		h.log.Error("failed to mark webhook processed", "error", err, "id", event.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}

// logVerifyError logs a signature verification failure with the right severity.
// Signature mismatch from a random scanner is noisy but routine; replay is a
// smaller class; anything else suggests malformed input or misconfiguration.
func (h *Handler) logVerifyError(err error, r *http.Request) {
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	switch {
	case errors.Is(err, twitch.ErrEventSubSignatureMismatch):
		h.log.Warn("webhook signature mismatch", "ip", ip)
	case errors.Is(err, twitch.ErrEventSubMessageReplay):
		h.log.Warn("webhook replay rejected", "ip", ip,
			"timestamp", r.Header.Get(twitch.EventSubHeaderMessageTimestamp))
	default:
		h.log.Warn("webhook verification failed", "error", err, "ip", ip)
	}
}

// buildEventInput constructs the audit-log row for a decoded webhook.
// Payload is the raw body verbatim — retention-trimmed later by the scheduler.
func buildEventInput(eventID string, ts time.Time, body []byte, notif *twitch.EventSubNotification) *repository.WebhookEventInput {
	input := &repository.WebhookEventInput{
		EventID:          eventID,
		MessageType:      string(notif.MessageType),
		MessageTimestamp: ts,
		Payload:          json.RawMessage(body),
	}
	if notif.Subscription.ID != "" {
		id := notif.Subscription.ID
		input.SubscriptionID = &id
	}
	// event_type is only meaningful for notifications — verification and
	// revocation carry subscription metadata, not an event payload.
	if notif.MessageType == twitch.MsgTypeNotification && notif.Subscription.Type != "" {
		et := notif.Subscription.Type
		input.EventType = &et
	}
	if bid := broadcasterIDFromCondition(notif.Subscription.Condition); bid != "" {
		input.BroadcasterID = &bid
	}
	return input
}

// broadcasterIDFromCondition pulls BroadcasterUserID out of the typed condition
// via reflection. Works for any generated *Condition that carries the standard
// broadcaster field (the vast majority). Returns empty for condition shapes
// that don't carry one (drop.entitlement.grant, user.authorization.*) and for
// UnknownCondition — we simply don't denormalize broadcaster_id in those cases.
func broadcasterIDFromCondition(cond twitch.EventSubCondition) string {
	if cond == nil {
		return ""
	}
	rv := reflect.ValueOf(cond)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return ""
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return ""
	}
	f := rv.FieldByName("BroadcasterUserID")
	if !f.IsValid() || f.Kind() != reflect.String {
		return ""
	}
	return f.String()
}

// parseHeaderTimestamp accepts the RFC3339Nano Twitch sends, falling back to
// plain RFC3339. A malformed timestamp would have been rejected by
// VerifyEventSubSignature — this is defense in depth, returning zero time
// when the value is unparseable.
func parseHeaderTimestamp(s string) time.Time {
	if ts, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return ts
	}
	if ts, err := time.Parse(time.RFC3339, s); err == nil {
		return ts
	}
	return time.Time{}
}
