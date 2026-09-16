package sqliteadapter

import (
	"context"
	"encoding/json"
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

func (a *SQLiteAdapter) SetJobExecution(ctx context.Context, jobID, executionID string, acceptsMetadata bool) error {
	return executionAffected(a.queries.SetJobExecution(ctx, sqlitegen.SetJobExecutionParams{JobID: jobID, ExecutionID: executionID, AcceptsMetadata: boolToInt64(acceptsMetadata)}))
}
func (a *SQLiteAdapter) StopJobMetadata(ctx context.Context, jobID, executionID string) error {
	return executionAffected(a.queries.StopJobMetadata(ctx, sqlitegen.StopJobMetadataParams{JobID: jobID, ExecutionID: executionID}))
}

func (a *SQLiteAdapter) CheckpointAttempt(ctx context.Context, jobID, executionID string, state json.RawMessage) error {
	return executionAffected(a.queries.CheckpointAttempt(ctx, sqlitegen.CheckpointAttemptParams{JobID: jobID, ExecutionID: executionID, State: string(state)}))
}
func (a *SQLiteAdapter) ListRecoveryJobs(ctx context.Context, afterID string, limit int) ([]repository.Job, error) {
	rows, err := a.queries.ListRecoveryJobs(ctx, sqlitegen.ListRecoveryJobsParams{AfterID: afterID, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]repository.Job, len(rows))
	for i, row := range rows {
		out[i] = *sqliteJobToDomain(row)
	}
	return out, nil
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

func (a *SQLiteAdapter) ListStoppedJobs(ctx context.Context, afterID string, limit int) ([]repository.Job, error) {
	rows, err := a.queries.ListStoppedJobs(ctx, sqlitegen.ListStoppedJobsParams{AfterID: afterID, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]repository.Job, len(rows))
	for i, row := range rows {
		out[i] = *sqliteJobToDomain(row)
	}
	return out, nil
}
