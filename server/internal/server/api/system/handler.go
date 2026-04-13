package system

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/befabri/trpcgo"
)

// Handler is the tRPC adapter for the system (admin) domain. All
// procedures it exposes are owner-gated at the router.
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler wires a handler around a system Service.
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log.With("domain", "system-api")}
}

// FetchLogEntry is the wire shape for a fetch log entry.
type FetchLogEntry struct {
	ID            int64     `json:"id"`
	UserID        *string   `json:"user_id,omitempty"`
	FetchType     string    `json:"fetch_type"`
	BroadcasterID *string   `json:"broadcaster_id,omitempty"`
	Status        int       `json:"status"`
	Error         *string   `json:"error,omitempty"`
	DurationMs    int64     `json:"duration_ms"`
	FetchedAt     time.Time `json:"fetched_at"`
}

type FetchLogsInput struct {
	Limit     int    `json:"limit" validate:"min=0,max=500"`
	Offset    int    `json:"offset" validate:"min=0"`
	FetchType string `json:"fetch_type,omitempty"`
}

type FetchLogsResponse struct {
	Total int64           `json:"total"`
	Data  []FetchLogEntry `json:"data"`
}

func (h *Handler) FetchLogs(ctx context.Context, input FetchLogsInput) (FetchLogsResponse, error) {
	logs, total, err := h.svc.ListFetchLogs(ctx, FetchLogsFilter{
		Limit:     input.Limit,
		Offset:    input.Offset,
		FetchType: input.FetchType,
	})
	if err != nil {
		h.log.Error("load fetch logs", "error", err)
		return FetchLogsResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load fetch logs")
	}
	data := make([]FetchLogEntry, len(logs))
	for i, l := range logs {
		data[i] = FetchLogEntry{
			ID:            l.ID,
			UserID:        l.UserID,
			FetchType:     l.FetchType,
			BroadcasterID: l.BroadcasterID,
			Status:        l.Status,
			Error:         l.Error,
			DurationMs:    l.DurationMs,
			FetchedAt:     l.FetchedAt,
		}
	}
	return FetchLogsResponse{Total: total, Data: data}, nil
}

// EventLogEntry is the wire shape for an app-side event log row.
// Distinct from FetchLogEntry: those record outbound Helix calls;
// event_logs records things the app itself did.
type EventLogEntry struct {
	ID          int64           `json:"id"`
	Domain      string          `json:"domain"`
	EventType   string          `json:"event_type"`
	Severity    string          `json:"severity"`
	Message     string          `json:"message"`
	ActorUserID *string         `json:"actor_user_id,omitempty"`
	Data        json.RawMessage `json:"data,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

type EventLogsInput struct {
	Limit    int    `json:"limit" validate:"min=0,max=500"`
	Offset   int    `json:"offset" validate:"min=0"`
	Domain   string `json:"domain,omitempty"`
	Severity string `json:"severity,omitempty" validate:"omitempty,oneof=debug info warn error"`
}

type EventLogsResponse struct {
	Total int64           `json:"total"`
	Data  []EventLogEntry `json:"data"`
}

func (h *Handler) EventLogs(ctx context.Context, input EventLogsInput) (EventLogsResponse, error) {
	rows, total, err := h.svc.ListEventLogs(ctx, EventLogsFilter{
		Limit:    input.Limit,
		Offset:   input.Offset,
		Domain:   input.Domain,
		Severity: input.Severity,
	})
	if err != nil {
		h.log.Error("load event logs", "error", err)
		return EventLogsResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load event logs")
	}
	data := make([]EventLogEntry, len(rows))
	for i, r := range rows {
		data[i] = EventLogEntry{
			ID:          r.ID,
			Domain:      r.Domain,
			EventType:   r.EventType,
			Severity:    r.Severity,
			Message:     r.Message,
			ActorUserID: r.ActorUserID,
			Data:        r.Data,
			CreatedAt:   r.CreatedAt,
		}
	}
	return EventLogsResponse{Total: total, Data: data}, nil
}

type SearchEventLogsInput struct {
	Query  string `json:"query" validate:"required,min=1,max=200"`
	Limit  int    `json:"limit" validate:"min=0,max=200"`
	Offset int    `json:"offset" validate:"min=0"`
}

type SearchEventLogEntry struct {
	EventLogEntry
	Rank float64 `json:"rank"`
}

type SearchEventLogsResponse struct {
	Total  int64                 `json:"total"`
	Data   []SearchEventLogEntry `json:"data"`
	Ranked bool                  `json:"ranked"`
}

// SearchEventLogs runs a relevance-ordered search over event_logs.
// Full-text on Postgres (websearch_to_tsquery syntax — quoted phrases,
// AND/OR, -negation); substring fallback on SQLite with ranked=false
// so the dashboard can hide the rank column on backends that can't
// produce it.
func (h *Handler) SearchEventLogs(ctx context.Context, input SearchEventLogsInput) (SearchEventLogsResponse, error) {
	out, err := h.svc.SearchEventLogs(ctx, input.Query, input.Limit, input.Offset)
	if err != nil {
		h.log.Error("search event logs", "error", err)
		return SearchEventLogsResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to search event logs")
	}
	data := make([]SearchEventLogEntry, len(out.Results))
	for i, r := range out.Results {
		data[i] = SearchEventLogEntry{
			EventLogEntry: EventLogEntry{
				ID:          r.ID,
				Domain:      r.Domain,
				EventType:   r.EventType,
				Severity:    r.Severity,
				Message:     r.Message,
				ActorUserID: r.ActorUserID,
				Data:        r.Data,
				CreatedAt:   r.CreatedAt,
			},
			Rank: r.Rank,
		}
	}
	return SearchEventLogsResponse{Total: out.Total, Data: data, Ranked: out.Ranked}, nil
}
