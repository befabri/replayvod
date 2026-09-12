package downloader

import (
	"context"

	"github.com/befabri/replayvod/server/internal/recordingwebhook"
	"github.com/befabri/replayvod/server/internal/repository"
)

// markRecordingFailed preserves the attempt for recovery unless the recording,
// attempt and webhook outbox all commit. Callers publish and clean up afterward.
func (s *Service) markRecordingFailed(ctx context.Context, jobID string, videoID int64, message, completionKind string, truncated bool) error {
	delivery := s.recordingWebhookDelivery(videoID, recordingwebhook.EventFailed)
	return s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := tx.MarkVideoFailedAndEnqueueRecordingWebhook(ctx, videoID, message, completionKind, truncated, delivery); err != nil {
			return err
		}
		return tx.MarkJobFailed(ctx, jobID, message)
	})
}
