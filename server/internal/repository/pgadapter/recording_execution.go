package pgadapter

import (
	"context"
	"encoding/json"
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

func (a *PGAdapter) SetJobExecution(ctx context.Context, jobID, executionID string, acceptsMetadata bool) error {
	return executionAffected(a.queries.SetJobExecution(ctx, pggen.SetJobExecutionParams{JobID: jobID, ExecutionID: executionID, AcceptsMetadata: acceptsMetadata}))
}
func (a *PGAdapter) StopJobMetadata(ctx context.Context, jobID, executionID string) error {
	return executionAffected(a.queries.StopJobMetadata(ctx, pggen.StopJobMetadataParams{JobID: jobID, ExecutionID: executionID}))
}

func (a *PGAdapter) CheckpointAttempt(ctx context.Context, jobID, executionID string, state json.RawMessage) error {
	return executionAffected(a.queries.CheckpointAttempt(ctx, pggen.CheckpointAttemptParams{JobID: jobID, ExecutionID: executionID, State: state}))
}
func (a *PGAdapter) ListRecoveryJobs(ctx context.Context, afterID string, limit int) ([]repository.Job, error) {
	rows, err := a.queries.ListRecoveryJobs(ctx, pggen.ListRecoveryJobsParams{AfterID: afterID, Limit: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]repository.Job, len(rows))
	for i, row := range rows {
		out[i] = *pgJobToDomain(row)
	}
	return out, nil
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

func (a *PGAdapter) ListStoppedJobs(ctx context.Context, afterID string, limit int) ([]repository.Job, error) {
	rows, err := a.queries.ListStoppedJobs(ctx, pggen.ListStoppedJobsParams{AfterID: afterID, Limit: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]repository.Job, len(rows))
	for i, row := range rows {
		out[i] = *pgJobToDomain(row)
	}
	return out, nil
}
