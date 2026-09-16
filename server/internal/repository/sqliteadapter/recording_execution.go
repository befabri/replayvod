package sqliteadapter

import (
	"context"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func executionAffected(n int64, err error) error {
	if err != nil {
		return err
	}
	if n != 1 {
		return repository.ErrStaleExecution
	}
	return nil
}

func (a *SQLiteAdapter) ListQueuedArchiveJobs(ctx context.Context, after time.Time, afterID int64, limit int) ([]repository.ArchiveQueueCandidate, error) {
	rows, err := a.queries.ListQueuedArchiveJobs(ctx, sqlitegen.ListQueuedArchiveJobsParams{After: sqliteTime(after), AfterID: afterID, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]repository.ArchiveQueueCandidate, len(rows))
	for i, row := range rows {
		out[i] = repository.ArchiveQueueCandidate{JobID: row.JobID, VideoID: row.VideoID, QueuedAt: row.QueuedAt.Time}
	}
	return out, nil
}
