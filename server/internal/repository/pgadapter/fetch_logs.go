package pgadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) CreateFetchLog(ctx context.Context, input *repository.FetchLogInput) error {
	return a.queries.CreateFetchLog(ctx, pggen.CreateFetchLogParams{
		UserID:        input.UserID,
		FetchType:     input.FetchType,
		BroadcasterID: input.BroadcasterID,
		Status:        int32(input.Status),
		Error:         input.Error,
		DurationMs:    int32(input.DurationMs),
	})
}

func (a *PGAdapter) ListFetchLogs(ctx context.Context, limit, offset int) ([]repository.FetchLog, error) {
	rows, err := a.queries.ListFetchLogs(ctx, pggen.ListFetchLogsParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list fetch logs: %w", err)
	}
	return pgFetchLogsToDomain(rows), nil
}

func (a *PGAdapter) ListFetchLogsByType(ctx context.Context, fetchType string, limit, offset int) ([]repository.FetchLog, error) {
	rows, err := a.queries.ListFetchLogsByType(ctx, pggen.ListFetchLogsByTypeParams{
		FetchType: fetchType,
		Limit:     int32(limit),
		Offset:    int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list fetch logs by type: %w", err)
	}
	return pgFetchLogsToDomain(rows), nil
}

func (a *PGAdapter) CountFetchLogs(ctx context.Context) (int64, error) {
	return a.queries.CountFetchLogs(ctx)
}

func (a *PGAdapter) CountFetchLogsByType(ctx context.Context, fetchType string) (int64, error) {
	return a.queries.CountFetchLogsByType(ctx, fetchType)
}

func (a *PGAdapter) DeleteOldFetchLogs(ctx context.Context, before time.Time) error {
	return a.queries.DeleteOldFetchLogs(ctx, before)
}

func pgFetchLogsToDomain(rows []pggen.FetchLog) []repository.FetchLog {
	logs := make([]repository.FetchLog, len(rows))
	for i, row := range rows {
		logs[i] = repository.FetchLog{
			ID:            row.ID,
			UserID:        row.UserID,
			FetchType:     row.FetchType,
			BroadcasterID: row.BroadcasterID,
			Status:        int(row.Status),
			Error:         row.Error,
			DurationMs:    int64(row.DurationMs),
			FetchedAt:     row.FetchedAt,
		}
	}
	return logs
}
