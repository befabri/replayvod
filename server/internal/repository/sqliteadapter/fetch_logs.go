package sqliteadapter

import (
	"context"

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
			FetchedAt:     row.FetchedAt.Time,
		}
	}
	return logs
}
