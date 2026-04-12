// Package system is the tRPC-transport wrapper around systemservice
// (owner-level admin surface).
package system

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/service/systemservice"
	"github.com/befabri/trpcgo"
)

type Service struct {
	svc *systemservice.Service
	log *slog.Logger
}

func NewService(svc *systemservice.Service, log *slog.Logger) *Service {
	return &Service{svc: svc, log: log.With("domain", "system-api")}
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

func (s *Service) FetchLogs(ctx context.Context, input FetchLogsInput) (FetchLogsResponse, error) {
	logs, total, err := s.svc.ListFetchLogs(ctx, systemservice.FetchLogsFilter{
		Limit:     input.Limit,
		Offset:    input.Offset,
		FetchType: input.FetchType,
	})
	if err != nil {
		s.log.Error("load fetch logs", "error", err)
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

func (s *Service) EventLogs(ctx context.Context, input EventLogsInput) (EventLogsResponse, error) {
	rows, total, err := s.svc.ListEventLogs(ctx, systemservice.EventLogsFilter{
		Limit:    input.Limit,
		Offset:   input.Offset,
		Domain:   input.Domain,
		Severity: input.Severity,
	})
	if err != nil {
		s.log.Error("load event logs", "error", err)
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

// repositoryErrNotFoundSentinel keeps the import of repository used by
// users.go / whitelist.go if those files get slimmer — belt-and-
// suspenders, harmless.
var _ = repository.ErrNotFound
