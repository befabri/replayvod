package pgadapter

import (
	"context"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

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
