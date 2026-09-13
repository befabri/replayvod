package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrCommitUncertain means the transaction outcome needs reconciliation; callers
// retain ownership and retry the same operation before compensating.
var ErrCommitUncertain = errors.New("repository: commit confirmation unavailable")

// ErrStopRequested means cancellation must settle before further recording writes.
var ErrStopRequested = errors.New("repository: recording stop requested")

// AttemptClaim identifies a recording writer; ExecutionID is empty only while
// settling cancellation of a job that has never been claimed.
type AttemptClaim struct {
	// AllowStopRequested permits cancellation settlement after a durable stop.
	AllowStopRequested bool
	MetadataStopped    bool
	JobID              string
	VideoID            int64
	ExecutionID        string
}

// CreateAttempt atomically admits a video and job, using the job UUID to make
// retries after an uncertain commit idempotent.
func CreateAttempt(ctx context.Context, repo Repository, input *VideoInput, checkpoint json.RawMessage) (*Video, error) {
	if input.JobID == "" || !json.Valid(checkpoint) {
		return nil, fmt.Errorf("invalid attempt admission")
	}
	var video *Video
	err := repo.WithTx(ctx, func(tx Repository) error {
		existing, err := tx.GetVideoByJobID(ctx, input.JobID)
		if err == nil {
			job, err := tx.GetJob(ctx, input.JobID)
			if err != nil {
				return err
			}
			if job.VideoID != existing.ID || existing.BroadcasterID != input.BroadcasterID {
				return ErrStaleExecution
			}
			video = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if input.IntentID != "" {
			streamID := ""
			if input.StreamID != nil {
				streamID = *input.StreamID
			}
			if input.IntentPreviousJobID == "" {
				if err := tx.CreateRecordingIntent(ctx, RecordingIntent{ID: input.IntentID, BroadcasterID: input.BroadcasterID, Params: input.IntentParams, WaitSeconds: input.RestartWaitSeconds, CurrentJobID: input.JobID, LastStreamID: streamID}); err != nil {
					return err
				}
				intent, err := tx.LockRecordingIntent(ctx, input.IntentID)
				if err != nil {
					return err
				}
				if intent.BroadcasterID != input.BroadcasterID || intent.CurrentJobID != input.JobID || intent.StopRequested || intent.Status != RecordingIntentStatusActive {
					return ErrStaleExecution
				}
			} else {
				intent, err := tx.LockRecordingIntent(ctx, input.IntentID)
				if err != nil {
					return err
				}
				if intent.BroadcasterID != input.BroadcasterID {
					return ErrStaleExecution
				}
				if err := tx.ActivateRecordingIntent(ctx, input.IntentID, input.IntentPreviousJobID, input.JobID, streamID, input.IntentObservedAt); err != nil {
					return err
				}
			}
		}
		if input.StreamID != nil && *input.StreamID != "" {
			stream, err := tx.GetStream(ctx, *input.StreamID)
			if errors.Is(err, ErrNotFound) {
				_, err = tx.UpsertStream(ctx, &StreamInput{ID: *input.StreamID, BroadcasterID: input.BroadcasterID, Type: "live", Language: input.Language, ViewerCount: input.ViewerCount, StartedAt: input.StreamStartedAt})
			} else if err == nil && stream.BroadcasterID != input.BroadcasterID {
				return ErrStaleExecution
			}
			if err != nil {
				return err
			}
		}
		video, err = tx.CreateVideo(ctx, input)
		if err != nil {
			return err
		}
		_, err = tx.CreateJob(ctx, &JobInput{ID: input.JobID, VideoID: video.ID, BroadcasterID: input.BroadcasterID, ResumeState: checkpoint, Attempt: 1})
		if err == nil && input.IntentID != "" {
			err = tx.LinkRecordingIntentVideo(ctx, input.IntentID, video.ID, input.StreamID)
		}

		return err
	})
	if err != nil {
		return nil, err
	}
	return video, nil
}

// ClaimAttempt replaces only the execution identified by previous, serializing
// with metadata and terminal writes; retrying the same claim is idempotent.
func ClaimAttempt(ctx context.Context, repo Repository, claim AttemptClaim, previous string) error {
	if claim.ExecutionID == "" {
		return ErrStaleExecution
	}
	return repo.WithTx(ctx, func(tx Repository) error {
		v, err := tx.GetVideoForUpdate(ctx, claim.VideoID)
		if err != nil {
			return err
		}
		if v.JobID != claim.JobID || v.DeletedAt != nil || (v.Status != VideoStatusPending && v.Status != VideoStatusRunning) {
			return ErrStaleExecution
		}
		job, err := tx.GetJob(ctx, claim.JobID)
		if err != nil {
			return err
		}
		if job.Status != JobStatusPending && job.Status != JobStatusRunning {
			return ErrStaleExecution
		}
		if job.StopRequested {
			return ErrStopRequested
		}
		if job.ExecutionID == claim.ExecutionID {
			return nil
		}
		if job.ExecutionID != previous {
			return ErrStaleExecution
		}
		var capture struct {
			CaptureStoppedAt *time.Time `json:"capture_stopped_at"`
			EndListSeen      bool       `json:"endlist_seen"`
		}
		if err := json.Unmarshal(job.ResumeState, &capture); err != nil {
			return err
		}
		acceptsMetadata := !claim.MetadataStopped && v.Source == VideoSourceLive && capture.CaptureStoppedAt == nil && !capture.EndListSeen
		if err := tx.SetJobExecution(ctx, claim.JobID, claim.ExecutionID, acceptsMetadata); err != nil {
			return err
		}
		if err := tx.UpdateVideoStatus(ctx, v.ID, VideoStatusRunning); err != nil {
			return err
		}
		if acceptsMetadata {
			return tx.ResumeVideoMetadataSpans(ctx, v.ID, time.Now().UTC())
		}
		return nil
	})
}

// GuardAttempt must run on the transaction adapter, before any other reads.
func GuardAttempt(ctx context.Context, tx Repository, claim AttemptClaim) (*Video, error) {
	v, err := tx.GetVideoForUpdate(ctx, claim.VideoID)
	if err != nil {
		return nil, err
	}
	if v.JobID != claim.JobID || v.DeletedAt != nil {
		return nil, ErrStaleExecution
	}
	j, err := tx.GetJob(ctx, claim.JobID)
	if err != nil {
		return nil, err
	}
	if j.ExecutionID != claim.ExecutionID {
		return nil, ErrStaleExecution
	}
	if j.StopRequested && !claim.AllowStopRequested {
		return nil, ErrStopRequested
	}
	return v, nil
}

// RequestAttemptStop serializes with claims and terminal transitions and stores
// the stop separately so a late checkpoint cannot erase it.
func RequestAttemptStop(ctx context.Context, repo Repository, jobID string) error {
	job, err := repo.GetJob(ctx, jobID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return repo.WithTx(ctx, func(tx Repository) error {
		v, err := tx.GetVideoForUpdate(ctx, job.VideoID)
		if err != nil {
			return err
		}
		if v.JobID != jobID || v.DeletedAt != nil || (v.Status != VideoStatusPending && v.Status != VideoStatusRunning) {
			return nil
		}
		return tx.RequestJobStop(ctx, jobID)
	})
}

// WithAttempt runs fn in a transaction after checking that claim owns a RUNNING
// recording; fn must use only the supplied repository.
func WithAttempt(ctx context.Context, repo Repository, claim AttemptClaim, fn func(Repository) error) error {
	return repo.WithTx(ctx, func(tx Repository) error {
		v, err := GuardAttempt(ctx, tx, claim)
		if err != nil {
			return err
		}
		if v.Status != VideoStatusRunning {
			return ErrStaleExecution
		}
		return fn(tx)
	})
}

// StopAttemptMetadata atomically rejects future observations and closes the
// owning attempt's metadata spans at at.
func StopAttemptMetadata(ctx context.Context, repo Repository, claim AttemptClaim, at time.Time) error {
	return WithAttempt(ctx, repo, claim, func(tx Repository) error {
		if err := tx.StopJobMetadata(ctx, claim.JobID, claim.ExecutionID); err != nil {
			return err
		}
		return tx.CloseOpenVideoMetadataSpans(ctx, claim.VideoID, at)
	})
}

// MetadataEligible reports whether input belongs to the current recording
// execution; callers must lock the video before loading either row.
func MetadataEligible(v *Video, j *Job, input VideoMetadataChangeInput) bool {
	if v.DeletedAt != nil || v.JobID != input.JobID || j.ID != input.JobID || j.ExecutionID != input.ExecutionID || j.StopRequested {
		return false
	}
	if input.Initial {
		return v.Status == VideoStatusPending && j.Status == JobStatusPending
	}
	return v.Status == VideoStatusRunning && j.Status == JobStatusRunning && j.AcceptsMetadata && v.Source == VideoSourceLive
}
