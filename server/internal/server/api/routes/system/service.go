package system

import (
	"context"
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
