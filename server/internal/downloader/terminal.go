package downloader

import (
	"context"
	"time"

	"github.com/befabri/replayvod/server/internal/recordingwebhook"
	"github.com/befabri/replayvod/server/internal/repository"
)

// markRecordingFailed preserves the attempt for recovery unless the recording,
// attempt and webhook outbox all commit. Callers publish and clean up afterward.
func (s *Service) markRecordingFailed(ctx context.Context, claim repository.AttemptClaim, message, completionKind string, truncated bool) error {
	claim.AllowStopRequested = completionKind == repository.CompletionKindCancelled
	delivery := s.recordingWebhookDelivery(claim.VideoID, recordingwebhook.EventFailed)
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		v, err := repository.GuardAttempt(ctx, tx, claim)
		if err != nil {
			return err
		}
		if v.Status == repository.VideoStatusFailed {
			return nil
		}
		if v.Status != repository.VideoStatusPending && v.Status != repository.VideoStatusRunning {
			return repository.ErrStaleExecution
		}
		if err := tx.CloseOpenVideoMetadataSpans(ctx, claim.VideoID, time.Now().UTC()); err != nil {
			return err
		}
		if err := tx.MarkVideoFailedAndEnqueueRecordingWebhook(ctx, claim.VideoID, message, completionKind, truncated, delivery); err != nil {
			return err
		}
		return tx.MarkJobFailed(ctx, claim.JobID, message)
	})
	if err == nil {
		s.bus.NotifyVideoChange()
	}
	return err
}

func (s *Service) finishAttempt(ctx context.Context, d *download, duration float64, size int64, thumbnail *string, kind string, truncated bool) error {
	delivery := s.recordingWebhookDelivery(d.videoID, recordingwebhook.EventCompleted)
	return s.persistVideoChange(ctx, "completion", func(writeCtx context.Context) error {
		return s.repo.WithTx(writeCtx, func(tx repository.Repository) error {
			v, err := repository.GuardAttempt(writeCtx, tx, d.claim())
			if err != nil {
				return err
			}
			if v.Status == repository.VideoStatusDone {
				return nil
			}
			if v.Status != repository.VideoStatusRunning {
				return repository.ErrStaleExecution
			}
			if err := tx.MarkVideoDoneAndEnqueueRecordingWebhook(writeCtx, d.videoID, duration, size, thumbnail, kind, truncated, delivery); err != nil {
				return err
			}
			return tx.MarkJobDone(writeCtx, d.jobID)
		})
	})
}
