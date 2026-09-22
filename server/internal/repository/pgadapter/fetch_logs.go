package pgadapter

import (
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

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
