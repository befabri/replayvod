package sqliteadapter

import (
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

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
