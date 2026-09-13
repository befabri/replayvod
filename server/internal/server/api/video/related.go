package video

import (
	"context"
	"errors"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/apierr"
)

type RelatedRecordingResponse struct {
	ID             int64          `json:"id"`
	JobID          string         `json:"job_id"`
	Title          string         `json:"title"`
	Status         VideoStatus    `json:"status"`
	CompletionKind CompletionKind `json:"completion_kind"`
	DeletedAt      *time.Time     `json:"deleted_at,omitempty"`
	StartedAt      time.Time      `json:"started_at"`
	Position       int64          `json:"position"`
}
type RelatedRecordingsResponse struct {
	IntentID  string                     `json:"intent_id,omitempty"`
	Status    RecordingIntentStatus      `json:"status,omitempty"`
	WaitUntil *time.Time                 `json:"wait_until,omitempty"`
	Items     []RelatedRecordingResponse `json:"items"`
}

// RelatedRecordings returns a bounded window around the selected member, including removed
// recordings. Visiting another member advances the window without merging
// playback assets or the viewer's progress between recordings.
func (h *Handler) RelatedRecordings(ctx context.Context, input GetByIDInput) (RelatedRecordingsResponse, error) {
	out := RelatedRecordingsResponse{Items: []RelatedRecordingResponse{}}
	v, err := h.video.repo.GetVideo(ctx, input.ID)
	if err != nil {
		return out, apierr.Map(h.log, err, "get recording")
	}
	intent, err := h.video.repo.GetRecordingIntentByJob(ctx, v.JobID)
	if errors.Is(err, repository.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return out, apierr.Map(h.log, err, "get recording intent")
	}
	rows, err := h.video.repo.ListRelatedRecordings(ctx, v.ID)
	if err != nil {
		return out, apierr.Map(h.log, err, "list related recordings")
	}
	out.IntentID, out.Status, out.WaitUntil = intent.ID, RecordingIntentStatus(intent.Status), intent.WaitUntil
	for _, row := range rows {
		out.Items = append(out.Items, RelatedRecordingResponse{ID: row.ID, JobID: row.JobID, Title: row.Title, Status: VideoStatus(row.Status), CompletionKind: CompletionKind(row.CompletionKind), DeletedAt: row.DeletedAt, StartedAt: row.StartDownloadAt, Position: row.Position})
	}
	return out, nil
}
