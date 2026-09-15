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

func (a *PGAdapter) LinkRecordingIntentVideo(ctx context.Context, intentID string, videoID int64, streamID *string) error {
	return mapErr(a.queries.LinkRecordingIntentVideo(ctx, pggen.LinkRecordingIntentVideoParams{IntentID: intentID, VideoID: videoID, StreamID: streamID}))
}

func (a *PGAdapter) GetRecordingIntentByJob(ctx context.Context, id string) (*repository.RecordingIntent, error) {
	row, err := a.queries.GetRecordingIntentByJob(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgRecordingIntentToDomain(row), nil
}
func (a *PGAdapter) ListRecoverableRecordingIntents(ctx context.Context, after string, limit int) ([]repository.RecordingIntent, error) {
	rows, err := a.queries.ListRecoverableRecordingIntents(ctx, pggen.ListRecoverableRecordingIntentsParams{AfterID: after, BatchLimit: int32(limit)})
	if err != nil {
		return nil, err
	}
	return pgRecordingIntentsToDomain(rows), nil
}
func (a *PGAdapter) SetRecordingIntentWaiting(ctx context.Context, id, jobID string, until time.Time) error {
	n, err := a.queries.SetRecordingIntentWaiting(ctx, pggen.SetRecordingIntentWaitingParams{ID: id, CurrentJobID: jobID, WaitUntil: &until})
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
func (a *PGAdapter) ActivateRecordingIntent(ctx context.Context, id, previousJobID, nextJobID, streamID string, observedAt time.Time) error {
	return executionAffected(a.queries.ActivateRecordingIntent(ctx, pggen.ActivateRecordingIntentParams{ID: id, PreviousJobID: previousJobID, NextJobID: nextJobID, LastStreamID: streamID, ObservedAt: &observedAt}))
}

func (a *PGAdapter) ListRecordingIntentJobs(ctx context.Context, intentID, after string, limit int) ([]repository.Job, error) {
	rows, err := a.queries.ListRecordingIntentJobs(ctx, pggen.ListRecordingIntentJobsParams{IntentID: intentID, AfterID: after, BatchLimit: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]repository.Job, len(rows))
	for i, row := range rows {
		out[i] = *pgJobToDomain(row)
	}
	return out, nil
}
func (a *PGAdapter) ListRelatedRecordings(ctx context.Context, videoID int64) ([]repository.RelatedRecording, error) {
	rows, err := a.queries.ListRelatedRecordings(ctx, videoID)
	if err != nil {
		return nil, err
	}
	out := make([]repository.RelatedRecording, len(rows))
	for i, row := range rows {
		out[i] = repository.RelatedRecording{ID: row.ID, JobID: row.JobID, Title: row.Title, Status: row.Status, CompletionKind: row.CompletionKind, DeletedAt: row.DeletedAt, StartDownloadAt: row.StartDownloadAt, Position: row.Position}
	}
	return out, nil
}
