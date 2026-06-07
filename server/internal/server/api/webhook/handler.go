package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/go-chi/chi/v5"
)

const maxWebhookBodyBytes = 1 << 20

// EventProcessor dispatches decoded notifications to domain logic.
type EventProcessor interface {
	Process(ctx context.Context, n *twitch.EventSubNotification) error
}

type Handler struct {
	repo       repository.Repository
	hmacSecret string
	processor  EventProcessor
	log        *slog.Logger
	maxAge     time.Duration
}

func NewHandler(repo repository.Repository, hmacSecret string, processor EventProcessor, log *slog.Logger) *Handler {
	return &Handler{
		repo:       repo,
		hmacSecret: hmacSecret,
		processor:  processor,
		log:        log.With("domain", "webhook"),
	}
}

func (h *Handler) SetupRoutes(r chi.Router) {
	r.Post("/webhook/callback", h.handleCallback)
}

func (h *Handler) handleCallback(w http.ResponseWriter, r *http.Request) {
	// HMAC covers the literal bytes Twitch sent, before any JSON parsing.
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)
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

	// Keep audit writes alive after a client/proxy disconnect.
	dbCtx := context.WithoutCancel(r.Context())

	// Verification can arrive before the mirrored subscription row exists.
	if input.SubscriptionID != nil {
		if _, err := h.repo.GetSubscription(dbCtx, *input.SubscriptionID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				input.SubscriptionID = nil
			} else {
				h.log.Warn("check subscription for audit FK", "id", *input.SubscriptionID, "error", err)
				input.SubscriptionID = nil
			}
		}
	}

	switch notif.MessageType {
	case twitch.MsgTypeVerification:
		h.handleVerification(w, dbCtx, input, notif.Challenge)
	case twitch.MsgTypeRevocation:
		h.handleRevocation(w, dbCtx, input, &notif.Subscription)
	case twitch.MsgTypeNotification:
		h.handleNotification(w, dbCtx, input, notif)
	default:
		h.log.Warn("unknown webhook message type", "type", notif.MessageType)
		http.Error(w, "unknown message type", http.StatusBadRequest)
	}
}

func (h *Handler) handleVerification(w http.ResponseWriter, ctx context.Context, input *repository.WebhookEventInput, challenge string) {
	if _, err := h.repo.CreateWebhookEvent(ctx, input); err != nil && !errors.Is(err, repository.ErrNotFound) {
		// Echoing the challenge matters more than recording the audit row.
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
			// ON CONFLICT DO NOTHING: this Message-Id was already processed.
			h.log.Debug("duplicate webhook event, dedupd", "event_id", input.EventID)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.log.Error("failed to record notification event", "error", err, "event_id", input.EventID)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if h.processor == nil {
		// Audit-only mode should not trip the stuck-events query.
		if err := h.repo.MarkWebhookEventProcessed(ctx, event.ID); err != nil {
			h.log.Error("failed to mark audit-only webhook processed", "error", err, "id", event.ID)
		}
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
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.repo.MarkWebhookEventProcessed(ctx, event.ID); err != nil {
		h.log.Error("failed to mark webhook processed", "error", err, "id", event.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}

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
	if notif.MessageType == twitch.MsgTypeNotification && notif.Subscription.Type != "" {
		et := notif.Subscription.Type
		input.EventType = &et
	}
	if cond, ok := notif.Subscription.Condition.(twitch.BroadcasterScopedCondition); ok {
		if bid := cond.GetBroadcasterUserID(); bid != "" {
			input.BroadcasterID = &bid
		}
	}
	return input
}

func parseHeaderTimestamp(s string) time.Time {
	if ts, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return ts
	}
	if ts, err := time.Parse(time.RFC3339, s); err == nil {
		return ts
	}
	return time.Time{}
}
