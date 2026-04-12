package system

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// Service handles tRPC system procedures (owner-level).
type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

// NewService creates a new system tRPC service.
func NewService(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With("domain", "system"),
	}
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

// FetchLogsInput pagination + optional type filter.
type FetchLogsInput struct {
	Limit     int    `json:"limit" validate:"min=0,max=500"`
	Offset    int    `json:"offset" validate:"min=0"`
	FetchType string `json:"fetch_type"`
}

// FetchLogsResponse is the paginated response.
type FetchLogsResponse struct {
	Total int64           `json:"total"`
	Data  []FetchLogEntry `json:"data"`
}

// FetchLogs returns paginated API fetch logs.
func (s *Service) FetchLogs(ctx context.Context, input FetchLogsInput) (FetchLogsResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}

	var (
		logs  []repository.FetchLog
		total int64
		err   error
	)
	if input.FetchType != "" {
		logs, err = s.repo.ListFetchLogsByType(ctx, input.FetchType, limit, input.Offset)
		if err == nil {
			total, err = s.repo.CountFetchLogsByType(ctx, input.FetchType)
		}
	} else {
		logs, err = s.repo.ListFetchLogs(ctx, limit, input.Offset)
		if err == nil {
			total, err = s.repo.CountFetchLogs(ctx)
		}
	}
	if err != nil {
		s.log.Error("failed to load fetch logs", "error", err)
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
// event_logs records things the app itself did (task runs, auto-
// downloads, auth outcomes).
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

// EventLogsInput paginates and optionally filters by domain or severity.
type EventLogsInput struct {
	Limit    int    `json:"limit" validate:"min=0,max=500"`
	Offset   int    `json:"offset" validate:"min=0"`
	Domain   string `json:"domain"`
	Severity string `json:"severity" validate:"omitempty,oneof=debug info warn error"`
}

// EventLogsResponse is the paginated response shape.
type EventLogsResponse struct {
	Total int64           `json:"total"`
	Data  []EventLogEntry `json:"data"`
}

// EventLogs returns paginated app-side event log rows. Owner-only per
// router wiring — these expose details a regular viewer shouldn't see.
func (s *Service) EventLogs(ctx context.Context, input EventLogsInput) (EventLogsResponse, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}

	var (
		rows  []repository.EventLog
		total int64
		err   error
	)
	switch {
	case input.Domain != "":
		rows, err = s.repo.ListEventLogsByDomain(ctx, input.Domain, limit, input.Offset)
		if err == nil {
			total, err = s.repo.CountEventLogsByDomain(ctx, input.Domain)
		}
	case input.Severity != "":
		rows, err = s.repo.ListEventLogsBySeverity(ctx, input.Severity, limit, input.Offset)
		if err == nil {
			// Fall back to overall count when filtering by severity — a
			// sev-scoped count query would mostly bloat the sqlc surface
			// for a number the UI uses only as a "you probably want to
			// paginate" hint.
			total, err = s.repo.CountEventLogs(ctx)
		}
	default:
		rows, err = s.repo.ListEventLogs(ctx, limit, input.Offset)
		if err == nil {
			total, err = s.repo.CountEventLogs(ctx)
		}
	}
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
