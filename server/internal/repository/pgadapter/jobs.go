package pgadapter

import (
	"context"
	"encoding/json"
	"fmt"

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
		Attempt:       max(input.Attempt, 1),
	})
	if err != nil {
		return nil, fmt.Errorf("pg create job: %w", err)
	}
	return pgJobToDomain(row), nil
}

func (a *PGAdapter) MarkJobFailed(ctx context.Context, id string, errMsg string) error {
	return a.queries.MarkJobFailed(ctx, pggen.MarkJobFailedParams{
		ID:    id,
		Error: &errMsg,
	})
}

func (a *PGAdapter) ListRunningLiveBroadcasters(ctx context.Context) ([]string, error) {
	return a.queries.ListRunningLiveBroadcasters(ctx)
}
