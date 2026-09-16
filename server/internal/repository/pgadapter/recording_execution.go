package pgadapter

import (
	"context"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
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

func (a *PGAdapter) ListQueuedArchiveJobs(ctx context.Context, after time.Time, afterID int64, limit int) ([]repository.ArchiveQueueCandidate, error) {
	rows, err := a.queries.ListQueuedArchiveJobs(ctx, pggen.ListQueuedArchiveJobsParams{After: after, AfterID: afterID, Limit: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]repository.ArchiveQueueCandidate, len(rows))
	for i, row := range rows {
		out[i] = repository.ArchiveQueueCandidate{JobID: row.JobID, VideoID: row.VideoID, QueuedAt: row.QueuedAt}
	}
	return out, nil
}
