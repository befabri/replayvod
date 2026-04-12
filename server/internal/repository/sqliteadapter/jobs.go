package sqliteadapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) CreateJob(ctx context.Context, input *repository.JobInput) (*repository.Job, error) {
	rs := string(input.ResumeState)
	if rs == "" {
		rs = "{}"
	}
	row, err := a.queries.CreateJob(ctx, sqlitegen.CreateJobParams{
		ID:            input.ID,
		VideoID:       input.VideoID,
		BroadcasterID: input.BroadcasterID,
		ResumeState:   rs,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite create job: %w", err)
	}
	return sqliteJobToDomain(row), nil
}

func (a *SQLiteAdapter) GetJob(ctx context.Context, id string) (*repository.Job, error) {
	row, err := a.queries.GetJob(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteJobToDomain(row), nil
}

func (a *SQLiteAdapter) GetJobByVideoID(ctx context.Context, videoID int64) (*repository.Job, error) {
	row, err := a.queries.GetJobByVideoID(ctx, videoID)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteJobToDomain(row), nil
}

func (a *SQLiteAdapter) GetActiveJobByBroadcaster(ctx context.Context, broadcasterID string) (*repository.Job, error) {
	row, err := a.queries.GetActiveJobByBroadcaster(ctx, broadcasterID)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteJobToDomain(row), nil
}

func (a *SQLiteAdapter) MarkJobRunning(ctx context.Context, id string) error {
	return a.queries.MarkJobRunning(ctx, id)
}

func (a *SQLiteAdapter) MarkJobDone(ctx context.Context, id string) error {
	return a.queries.MarkJobDone(ctx, id)
}

func (a *SQLiteAdapter) MarkJobFailed(ctx context.Context, id string, errMsg string) error {
	return a.queries.MarkJobFailed(ctx, sqlitegen.MarkJobFailedParams{
		ID:    id,
		Error: sql.NullString{String: errMsg, Valid: true},
	})
}

func (a *SQLiteAdapter) UpdateJobResumeState(ctx context.Context, id string, resumeState json.RawMessage) error {
	s := string(resumeState)
	if s == "" {
		s = "{}"
	}
	return a.queries.UpdateJobResumeState(ctx, sqlitegen.UpdateJobResumeStateParams{
		ID:          id,
		ResumeState: s,
	})
}

func (a *SQLiteAdapter) ListRunningJobs(ctx context.Context) ([]repository.Job, error) {
	rows, err := a.queries.ListRunningJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite list running jobs: %w", err)
	}
	out := make([]repository.Job, len(rows))
	for i, r := range rows {
		out[i] = *sqliteJobToDomain(r)
	}
	return out, nil
}

func (a *SQLiteAdapter) ListFailedJobsForRetry(ctx context.Context, before time.Time, limit int) ([]repository.Job, error) {
	rows, err := a.queries.ListFailedJobsForRetry(ctx, sqlitegen.ListFailedJobsForRetryParams{
		FinishedAt: sql.NullString{String: formatTime(before), Valid: true},
		Limit:      int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list failed jobs for retry: %w", err)
	}
	out := make([]repository.Job, len(rows))
	for i, r := range rows {
		out[i] = *sqliteJobToDomain(r)
	}
	return out, nil
}

func sqliteJobToDomain(j sqlitegen.Job) *repository.Job {
	return &repository.Job{
		ID:            j.ID,
		VideoID:       j.VideoID,
		BroadcasterID: j.BroadcasterID,
		Status:        j.Status,
		StartedAt:     parseNullTime(j.StartedAt),
		FinishedAt:    parseNullTime(j.FinishedAt),
		Error:         fromNullString(j.Error),
		ResumeState:   json.RawMessage(j.ResumeState),
		CreatedAt:     parseTime(j.CreatedAt),
		UpdatedAt:     parseTime(j.UpdatedAt),
	}
}
