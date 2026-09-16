package sqliteadapter

import (
	"context"
	"fmt"

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
		Attempt:       int64(max(input.Attempt, 1)),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite create job: %w", err)
	}
	return sqliteJobToDomain(row), nil
}
