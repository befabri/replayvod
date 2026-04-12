package pgadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) CreateJob(ctx context.Context, input *repository.JobInput) (*repository.Job, error) {
	rs := input.ResumeState
	if len(rs) == 0 {
		// NOT NULL column; empty input means "no checkpoint yet".
		rs = json.RawMessage(`{}`)
	}
	row, err := a.queries.CreateJob(ctx, pggen.CreateJobParams{
		ID:            input.ID,
		VideoID:       input.VideoID,
		BroadcasterID: input.BroadcasterID,
		ResumeState:   rs,
	})
	if err != nil {
		return nil, fmt.Errorf("pg create job: %w", err)
	}
	return pgJobToDomain(row), nil
}

func (a *PGAdapter) GetJob(ctx context.Context, id string) (*repository.Job, error) {
	row, err := a.queries.GetJob(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgJobToDomain(row), nil
}

func (a *PGAdapter) GetJobByVideoID(ctx context.Context, videoID int64) (*repository.Job, error) {
	row, err := a.queries.GetJobByVideoID(ctx, videoID)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgJobToDomain(row), nil
}

func (a *PGAdapter) GetActiveJobByBroadcaster(ctx context.Context, broadcasterID string) (*repository.Job, error) {
	row, err := a.queries.GetActiveJobByBroadcaster(ctx, broadcasterID)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgJobToDomain(row), nil
}

func (a *PGAdapter) MarkJobRunning(ctx context.Context, id string) error {
	return a.queries.MarkJobRunning(ctx, id)
}

func (a *PGAdapter) MarkJobDone(ctx context.Context, id string) error {
	return a.queries.MarkJobDone(ctx, id)
}

func (a *PGAdapter) MarkJobFailed(ctx context.Context, id string, errMsg string) error {
	return a.queries.MarkJobFailed(ctx, pggen.MarkJobFailedParams{
		ID:    id,
		Error: &errMsg,
	})
}

func (a *PGAdapter) UpdateJobResumeState(ctx context.Context, id string, resumeState json.RawMessage) error {
	if len(resumeState) == 0 {
		resumeState = json.RawMessage(`{}`)
	}
	return a.queries.UpdateJobResumeState(ctx, pggen.UpdateJobResumeStateParams{
		ID:          id,
		ResumeState: resumeState,
	})
}

func (a *PGAdapter) ListRunningJobs(ctx context.Context) ([]repository.Job, error) {
	rows, err := a.queries.ListRunningJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg list running jobs: %w", err)
	}
	out := make([]repository.Job, len(rows))
	for i, r := range rows {
		out[i] = *pgJobToDomain(r)
	}
	return out, nil
}

func (a *PGAdapter) ListFailedJobsForRetry(ctx context.Context, before time.Time, limit int) ([]repository.Job, error) {
	rows, err := a.queries.ListFailedJobsForRetry(ctx, pggen.ListFailedJobsForRetryParams{
		FinishedAt: &before,
		Limit:      int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list failed jobs for retry: %w", err)
	}
	out := make([]repository.Job, len(rows))
	for i, r := range rows {
		out[i] = *pgJobToDomain(r)
	}
	return out, nil
}

func pgJobToDomain(j pggen.Job) *repository.Job {
	return &repository.Job{
		ID:            j.ID,
		VideoID:       j.VideoID,
		BroadcasterID: j.BroadcasterID,
		Status:        j.Status,
		StartedAt:     j.StartedAt,
		FinishedAt:    j.FinishedAt,
		Error:         j.Error,
		ResumeState:   j.ResumeState,
		CreatedAt:     j.CreatedAt,
		UpdatedAt:     j.UpdatedAt,
	}
}
