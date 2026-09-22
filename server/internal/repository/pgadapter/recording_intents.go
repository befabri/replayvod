package pgadapter

import (
	"context"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) CreateRecordingIntent(ctx context.Context, input repository.RecordingIntent) error {
	return mapErr(a.queries.CreateRecordingIntent(ctx, pggen.CreateRecordingIntentParams{ID: input.ID, BroadcasterID: input.BroadcasterID, Params: input.Params, WaitSeconds: input.WaitSeconds, CurrentJobID: input.CurrentJobID, LastStreamID: input.LastStreamID}))
}

func (a *PGAdapter) SetRecordingIntentWaiting(ctx context.Context, id, jobID string, until time.Time) error {
	n, err := a.queries.SetRecordingIntentWaiting(ctx, pggen.SetRecordingIntentWaitingParams{ID: id, JobID: jobID, Until: &until})
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	row, err := a.GetRecordingIntent(ctx, id)
	if err != nil {
		return err
	}
	if row.CurrentJobID == jobID && row.Status == "waiting" {
		return nil
	}
	return repository.ErrStaleExecution
}
