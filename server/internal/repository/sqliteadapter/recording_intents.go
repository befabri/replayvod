package sqliteadapter

import (
	"context"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) CreateRecordingIntent(ctx context.Context, input repository.RecordingIntent) error {
	return mapErr(a.queries.CreateRecordingIntent(ctx, sqlitegen.CreateRecordingIntentParams{ID: input.ID, BroadcasterID: input.BroadcasterID, Params: string(input.Params), WaitSeconds: input.WaitSeconds, CurrentJobID: input.CurrentJobID, LastStreamID: input.LastStreamID}))
}

func (a *SQLiteAdapter) ListRecoverableRecordingIntents(ctx context.Context, afterID string, limit int) ([]repository.RecordingIntent, error) {
	rows, err := a.queries.ListRecoverableRecordingIntents(ctx, sqlitegen.ListRecoverableRecordingIntentsParams{AfterID: afterID, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	return sqliteRecordingIntentsToDomain(rows), nil
}
func (a *SQLiteAdapter) SetRecordingIntentWaiting(ctx context.Context, id, jobID string, until time.Time) error {
	n, err := a.queries.SetRecordingIntentWaiting(ctx, sqlitegen.SetRecordingIntentWaitingParams{ID: id, JobID: jobID, Until: until.UTC().Format("2006-01-02 15:04:05.000000000")})
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
func (a *SQLiteAdapter) ActivateRecordingIntent(ctx context.Context, id, previousJobID, nextJobID, streamID string, observedAt time.Time) error {
	return executionAffected(a.queries.ActivateRecordingIntent(ctx, sqlitegen.ActivateRecordingIntentParams{ID: id, PreviousJobID: previousJobID, NextJobID: nextJobID, StreamID: streamID, ObservedAt: observedAt.UTC().Format("2006-01-02 15:04:05.000000000")}))
}

func (a *SQLiteAdapter) ListRecordingIntentJobs(ctx context.Context, intentID, afterID string, limit int) ([]repository.Job, error) {
	rows, err := a.queries.ListRecordingIntentJobs(ctx, sqlitegen.ListRecordingIntentJobsParams{IntentID: intentID, AfterID: afterID, Limit: int64(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]repository.Job, len(rows))
	for i, row := range rows {
		out[i] = *sqliteJobToDomain(row)
	}
	return out, nil
}
func (a *SQLiteAdapter) ListRelatedRecordings(ctx context.Context, videoID int64) ([]repository.RelatedRecording, error) {
	rows, err := a.queries.ListRelatedRecordings(ctx, videoID)
	if err != nil {
		return nil, err
	}
	out := make([]repository.RelatedRecording, len(rows))
	for i, row := range rows {
		out[i] = repository.RelatedRecording{ID: row.ID, JobID: row.JobID, Title: row.Title, Status: row.Status, CompletionKind: row.CompletionKind, DeletedAt: timePtrFromSQLite(row.DeletedAt), StartDownloadAt: row.StartDownloadAt.Time, Position: row.Position}
	}
	return out, nil
}
