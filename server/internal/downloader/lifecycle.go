package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/befabri/replayvod/server/internal/background"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/google/uuid"
)

var errExecutionDeferred = errors.New("recording execution deferred")

func (d *download) claim() repository.AttemptClaim {
	return repository.AttemptClaim{JobID: d.jobID, VideoID: d.videoID, ExecutionID: d.executionID, MetadataStopped: (d.recovered && !d.captureIdentityVerified) || (d.resume != nil && (d.resume.CaptureStoppedAt != nil || d.resume.EndListSeen))}
}

// persist retries idempotent database work under five-second write deadlines.
// Callbacks must tolerate lost commit acknowledgements and must not capture or publish media.
func (s *Service) persist(ctx context.Context, operation string, write func(context.Context) error) error {
	delay := 100 * time.Millisecond
	for {
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err := write(writeCtx)
		cancel()
		if err == nil || errors.Is(err, repository.ErrStaleExecution) || errors.Is(err, repository.ErrStopRequested) || errors.Is(err, repository.ErrNotFound) || errors.Is(err, repository.ErrDuplicate) {
			return err
		}
		if ctx.Err() != nil {
			return err
		}
		s.log.Warn("recording persistence deferred", "operation", operation, "retry_in", delay, "error", err)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
		delay = min(2*delay, 5*time.Second)
	}
}

// persistVideoChange notifies only after the transition or its uncertain commit is confirmed.
func (s *Service) persistVideoChange(ctx context.Context, operation string, write func(context.Context) error) error {
	if err := s.persist(ctx, operation, write); err != nil {
		return err
	}
	s.bus.NotifyVideoChange()
	return nil
}

func (s *Service) claimAttempt(ctx context.Context, d *download, previous string) error {
	return s.persistVideoChange(ctx, "claim", func(writeCtx context.Context) error {
		return repository.ClaimAttempt(writeCtx, s.repo, d.claim(), previous)
	})
}

func (s *Service) recordPart(ctx context.Context, d *download, facts *repository.VideoPartFinalize) error {
	return s.persist(ctx, "part", func(writeCtx context.Context) error {
		return repository.WithAttempt(writeCtx, s.repo, d.claim(), func(tx repository.Repository) error {
			return tx.FinalizeVideoPart(writeCtx, facts)
		})
	})
}

func (s *Service) persistCheckpoint(ctx context.Context, d *download, data json.RawMessage) error {
	write := func(writeCtx context.Context) error {
		return s.repo.CheckpointAttempt(writeCtx, d.jobID, d.executionID, data)
	}
	if d.runCtx == nil {
		return write(ctx)
	}
	return s.persist(d.runCtx, "checkpoint", write)
}

// releaseAttemptScratch also handles stops committed before capture opens its recovery directory.
func (s *Service) releaseAttemptScratch(d *download) {
	workspace := d.workspace
	if workspace == nil {
		if !d.cleanupScratch {
			return
		}
		var err error
		workspace, err = s.storage.Scratch().OpenForCleanup(filepath.Join(s.cfg.Env.ScratchDir, d.jobID))
		if err != nil {
			s.log.Warn("acquire terminal recording scratch", "job_id", d.jobID, "error", err)
			return
		}
	}
	if err := workspace.Close(d.cleanupScratch); err != nil {
		s.log.Warn("release recording scratch", "job_id", d.jobID, "error", err)
	}
}

// failUnrecoverableAttempt settles without executing a checkpoint.
// Unconfirmed settlement preserves saved media for bounded discovery retries.
func (s *Service) failUnrecoverableAttempt(ctx context.Context, job *repository.Job, cause error, cancelled bool) (err error) {
	workspace, err := s.storage.Scratch().OpenForCleanup(filepath.Join(s.cfg.Env.ScratchDir, job.ID))
	if err != nil {
		return err
	}
	committed := false
	defer func() { err = errors.Join(err, workspace.Close(committed)) }()
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	hasPart, err := s.repo.HasFinalizedVideoParts(writeCtx, job.VideoID)
	if err != nil {
		return err
	}
	kind := repository.CompletionKindComplete
	if hasPart {
		kind = repository.CompletionKindPartial
	}
	message := fmt.Sprintf("resume: %v", cause)
	if cancelled {
		kind = repository.CompletionKindCancelled
		message = ErrCancelled.Error()
	}
	cutShort := true
	if state, err := UnmarshalResumeState(job.ResumeState); err == nil {
		cutShort = cancelled || state.HadWindowRoll || !state.EndListSeen
	}
	claim := repository.AttemptClaim{JobID: job.ID, VideoID: job.VideoID, ExecutionID: job.ExecutionID}
	write := func(c context.Context) error {
		return s.markRecordingFailed(c, claim, message, kind, failedRunTruncated(true, hasPart, cutShort))
	}
	err = write(writeCtx)
	if errors.Is(err, repository.ErrCommitUncertain) {
		// A committed terminal row leaves discovery immediately. Reconcile the
		// acknowledgement while we still own its scratch and runner key.
		err = s.persist(writeCtx, "confirm recovered settlement", write)
	}
	if err != nil {
		return err
	}
	committed = true
	s.publishRecordingTerminal(job.VideoID, eventbus.RecordingFailed)
	return nil
}

// settleUnownedAttempt leaves active writers in charge of their settlement and scratch.
// Discovery reserves their job key without consuming capture capacity.
func (s *Service) settleUnownedAttempt(ctx context.Context, job *repository.Job, cause error, cancelled bool) error {
	s.mu.Lock()
	if s.active[job.ID] != nil {
		s.mu.Unlock()
		return nil
	}
	reservation, err := s.work.Reserve("settlement", job.ID)
	s.mu.Unlock()
	if errors.Is(err, background.ErrBusy) {
		return nil
	}
	if err != nil {
		return err
	}
	return reservation.Run(func(context.Context) error {
		return s.failUnrecoverableAttempt(ctx, job, cause, cancelled)
	}, nil)
}

func (s *Service) settleStoppedJobs(ctx context.Context) error {
	var failures []error
	for after := ""; ctx.Err() == nil; {
		jobs, err := s.repo.ListStoppedJobs(ctx, after, recoveryPageSize)
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		for i := range jobs {
			job := &jobs[i]
			after = job.ID
			if ctx.Err() != nil {
				break
			}
			if err := s.settleUnownedAttempt(ctx, job, ErrCancelled, true); err != nil && !errors.Is(err, repository.ErrStaleExecution) && len(failures) < 16 {
				failures = append(failures, fmt.Errorf("settle stopped job %s: %w", job.ID, err))
			}
		}
		if len(jobs) < recoveryPageSize {
			break
		}
	}
	return errors.Join(append(failures, ctx.Err())...)
}

func (s *Service) reconstructAttempt(ctx context.Context, job *repository.Job) (*download, Params, string, error) {
	v, err := s.repo.GetVideo(ctx, job.VideoID)
	if err != nil {
		return nil, Params{}, "", err
	}
	if v.JobID != job.ID || v.DeletedAt != nil {
		return nil, Params{}, "", errObsoleteJob
	}
	state, err := UnmarshalResumeState(job.ResumeState)
	if err != nil {
		return nil, Params{}, "", fmt.Errorf("%w: %v", errInvalidResume, err)
	}
	channel, err := s.repo.GetChannel(ctx, job.BroadcasterID)
	if err != nil {
		return nil, Params{}, "", err
	}
	p := Params{BroadcasterID: v.BroadcasterID, BroadcasterLogin: channel.BroadcasterLogin, DisplayName: v.DisplayName, Title: v.Title, Quality: v.Quality, Language: v.Language, ViewerCount: v.ViewerCount, StreamID: v.StreamID, RecordingType: v.RecordingType, ForceH264: v.ForceH264, MaxHeight: state.MaxHeight, BroadcastAt: v.BroadcastAt}
	if v.Source == repository.VideoSourceVOD && v.TwitchVideoID != nil {
		p.VODID = *v.TwitchVideoID
	}
	d := &download{recovered: true, jobID: job.ID, videoID: v.ID, executionID: uuid.NewString(), previousExecutionID: job.ExecutionID, broadcasterID: job.BroadcasterID, vod: p.isVOD(), attempt: job.Attempt, progressCh: make(chan Progress, 16), startedAt: s.now(), resume: state}
	if d.vod {
		d.limiter = archiveRateLimiter(s.cfg.App.Download)
	}
	return d, p, v.Filename, nil
}
