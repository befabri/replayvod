package sqliteadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) CreateFetchLog(ctx context.Context, input *repository.FetchLogInput) error {
	return a.queries.CreateFetchLog(ctx, sqlitegen.CreateFetchLogParams{
		UserID:        toNullString(input.UserID),
		FetchType:     input.FetchType,
		BroadcasterID: toNullString(input.BroadcasterID),
		Status:        int64(input.Status),
		Error:         toNullString(input.Error),
		DurationMs:    input.DurationMs,
	})
}

func (a *SQLiteAdapter) ListFetchLogs(ctx context.Context, limit, offset int) ([]repository.FetchLog, error) {
	rows, err := a.queries.ListFetchLogs(ctx, sqlitegen.ListFetchLogsParams{
		Limit:  int64(limit),
		Offset: int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list fetch logs: %w", err)
	}
	return sqliteFetchLogsToDomain(rows), nil
}

func (a *SQLiteAdapter) ListFetchLogsByType(ctx context.Context, fetchType string, limit, offset int) ([]repository.FetchLog, error) {
	rows, err := a.queries.ListFetchLogsByType(ctx, sqlitegen.ListFetchLogsByTypeParams{
		FetchType: fetchType,
		Limit:     int64(limit),
		Offset:    int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list fetch logs by type: %w", err)
	}
	return sqliteFetchLogsToDomain(rows), nil
}

func (a *SQLiteAdapter) CountFetchLogs(ctx context.Context) (int64, error) {
	return a.queries.CountFetchLogs(ctx)
}

func (a *SQLiteAdapter) CountFetchLogsByType(ctx context.Context, fetchType string) (int64, error) {
	return a.queries.CountFetchLogsByType(ctx, fetchType)
}

func (a *SQLiteAdapter) DeleteOldFetchLogs(ctx context.Context, before time.Time) error {
	return a.queries.DeleteOldFetchLogs(ctx, formatTime(before))
}

func sqliteFetchLogsToDomain(rows []sqlitegen.FetchLog) []repository.FetchLog {
	logs := make([]repository.FetchLog, len(rows))
	for i, row := range rows {
		logs[i] = repository.FetchLog{
			ID:            row.ID,
			UserID:        fromNullString(row.UserID),
			FetchType:     row.FetchType,
			BroadcasterID: fromNullString(row.BroadcasterID),
			Status:        int(row.Status),
			Error:         fromNullString(row.Error),
			DurationMs:    row.DurationMs,
			FetchedAt:     parseTime(row.FetchedAt),
		}
	}
	return logs
}
