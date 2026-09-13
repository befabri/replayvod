package downloader

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/downloader/hls"
	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/playbackauth"
	"github.com/befabri/replayvod/server/internal/repository"
)

// ErrNotVOD is returned by EnqueueVOD when Params carries no VOD id.
var ErrNotVOD = errors.New("downloader: archive requires a VOD id")

// archiveRetryBackoff is the wait before each automatic retry of an archive
// that failed for a transient reason: attempt N failing waits backoff[N-1].
// An archive is retried at most len(archiveRetryBackoff) times.
var archiveRetryBackoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour}

const (
	// archiveRetryPumpInterval bounds how late a scheduled retry can start
	// when no job ends and nothing is enqueued in the meantime.
	archiveRetryPumpInterval = 30 * time.Second
	archiveRetryBatch        = 50
)

// ArchiveMaxConcurrent returns the archive capacity, independent of live recording slots.
func (s *Service) ArchiveMaxConcurrent() int {
	if s.cfg == nil || s.cfg.App.Download.ArchiveMaxConcurrent <= 0 {
		return 1
	}
	return s.cfg.App.Download.ArchiveMaxConcurrent
}

func (s *Service) activeLiveCountLocked() int {
	owners := map[string]bool{}
	for _, d := range s.active {
		if !d.vod {
			key := d.jobID
			if d.manual != nil {
				key = d.manual.id
			}
			owners[key] = true
		}
	}
	return len(owners)
}

func (s *Service) activeArchiveCountLocked() int {
	n := 0
	for _, d := range s.active {
		if d.vod {
			n++
		}
	}
	return n
}

func segmentHostConnectionCap(cfg config.DownloadConfig) int {
	return (max(1, cfg.MaxConcurrent) + max(1, cfg.ArchiveMaxConcurrent)) * max(1, cfg.SegmentConcurrency)
}

// archiveRateLimiter limits archive bandwidth; nil means unlimited.
// Its burst permits up to one second of bytes, capped at 1 GiB.
func archiveRateLimiter(cfg config.DownloadConfig) hls.RateLimiter {
	bps := cfg.ArchiveMaxBytesPerSecond
	if bps <= 0 {
		return nil
	}
	return rate.NewLimiter(rate.Limit(bps), int(min(bps, 1<<30)))
}

func archiveRetryDelay(attempt int32) (time.Duration, bool) {
	if attempt < 1 || int(attempt) > len(archiveRetryBackoff) {
		return 0, false
	}
	return archiveRetryBackoff[attempt-1], true
}

// archiveRetryable reports whether an archive failed for a transient provider or network reason.
func archiveRetryable(err error) bool {
	if err == nil || errors.Is(err, ErrCancelled) || errors.Is(err, context.Canceled) {
		return false
	}
	var sealed *sealedCaptureError
	if errors.As(err, &sealed) {
		return sealed.retryable
	}
	if errors.Is(err, twitch.ErrPlaybackTokenEmpty) || twitch.IsPermanent(err) {
		return false
	}
	if errors.Is(err, hls.ErrPlaylistGone) || errors.Is(err, hls.ErrPlaylistAuthPermanent) || errors.Is(err, hls.ErrUnsupportedManifest) {
		return false
	}
	var resolution *playbackResolutionError
	if errors.As(err, &resolution) {
		return retryablePlaybackResolution(resolution.cause)
	}
	var auth *twitch.AuthError
	if errors.As(err, &auth) {
		return auth.Status == http.StatusTooManyRequests || auth.Status >= 500
	}
	var fetch *hls.FetchError
	if errors.As(err, &fetch) {
		switch fetch.Kind {
		case hls.FetchKindTransport, hls.FetchKindServer, hls.FetchKindCDNLag, hls.FetchKindBody:
			return true
		default:
			return false
		}
	}
	var gap *hls.GapAbortError
	if errors.As(err, &gap) || errors.Is(err, hls.ErrPlaylistAuth) || errors.Is(err, playbackauth.ErrUnavailable) {
		return true
	}
	var network net.Error
	return errors.As(err, &network)
}

// EnqueueVOD atomically queues a video and its first job, returning the job ID.
// Call PumpArchiveQueue after enqueueing a batch; ErrDuplicate means the VOD is already queued.
func (s *Service) EnqueueVOD(ctx context.Context, p Params) (string, error) {
	if p.VODID == "" {
		return "", ErrNotVOD
	}
	if s.shuttingDown.Load() {
		return "", ErrShuttingDown
	}
	jobID := uuid.NewString()
	filename := buildFilename(p.BroadcasterLogin, jobID)
	vodID := p.VODID
	state := NewResumeState()
	state.PosterURL = p.PosterURL
	checkpoint, err := state.MarshalJSON()
	if err != nil {
		return "", fmt.Errorf("encode archive checkpoint: %w", err)
	}
	input := &repository.VideoInput{
		JobID:         jobID,
		Filename:      filename,
		DisplayName:   p.DisplayName,
		Title:         p.Title,
		Status:        repository.VideoStatusPending,
		Quality:       p.Quality,
		BroadcasterID: p.BroadcasterID,
		StreamID:      p.StreamID,
		Language:      p.Language,
		RecordingType: p.RecordingType,
		ForceH264:     p.ForceH264,
		Source:        repository.VideoSourceVOD,
		TwitchVideoID: &vodID,
		BroadcastAt:   p.BroadcastAt,
	}
	admission, err := s.work.Reserve("admission", jobID)
	if err != nil {
		return "", ErrShuttingDown
	}
	defer admission.Release()
	var vid *repository.Video
	write := func(writeCtx context.Context) error {
		var err error
		vid, err = repository.CreateAttempt(writeCtx, s.repo, input, checkpoint)
		return err
	}
	err = write(ctx)
	if errors.Is(err, repository.ErrCommitUncertain) {
		err = s.persist(admission.Context(), "archive admission", write)
	}
	if err != nil {
		return "", fmt.Errorf("enqueue archive: %w", err)
	}
	s.bus.NotifyVideoChange()
	videoID := vid.ID

	s.publishArchiveQueue(eventbus.ArchiveQueued, videoID)
	return jobID, nil
}

// PumpArchiveQueue starts queued archives and due retries within archive capacity.
// Concurrent pumps are serialized; worker reservations prevent duplicate acquisition.
func (s *Service) PumpArchiveQueue(ctx context.Context) {
	s.pumpMu.Lock()
	defer s.pumpMu.Unlock()
	if s.shuttingDown.Load() || ctx.Err() != nil {
		return
	}
	if err := s.settleStoppedJobs(ctx); err != nil {
		s.log.Warn("stopped recording discovery deferred", "error", err)
	}
	// Waiting for storage must not even create the next retry attempt.
	if s.shuttingDown.Load() || ctx.Err() != nil || s.storageReady() != nil {
		return
	}
	s.requeueDueRetries(ctx)
	after := time.Time{}
	afterID := int64(0)
	for {
		jobs, err := s.repo.ListQueuedArchiveJobs(ctx, after, afterID, archiveRetryBatch)
		if err != nil {
			s.log.Warn("archive discovery failed", "error", err)
			return
		}
		for _, candidate := range jobs {
			after = candidate.QueuedAt
			afterID = candidate.VideoID
			if s.shuttingDown.Load() || ctx.Err() != nil || s.storageReady() != nil {
				return
			}
			s.mu.Lock()
			free := s.activeArchiveCountLocked() < s.ArchiveMaxConcurrent()
			s.mu.Unlock()
			if !free {
				return
			}
			job, err := s.repo.GetJob(ctx, candidate.JobID)
			if err != nil {
				s.log.Warn("archive lookup deferred", "job_id", candidate.JobID, "error", err)
				continue
			}
			if err := s.restartJob(ctx, job); err != nil {
				if errors.Is(err, ErrAtCapacity) {
					return
				}
				if errors.Is(err, errInvalidResume) {
					if writeErr := s.failQueuedArchive(ctx, job, err); writeErr != nil {
						s.log.Warn("archive failure settlement deferred", "error", writeErr)
					}
					continue
				}
				// A row-specific discovery failure must not pin all younger requests.
				s.log.Warn("archive start deferred", "job_id", job.ID, "error", err)
			}
		}
		if len(jobs) < archiveRetryBatch {
			return
		}
	}
}

// requeueDueRetries requires pumpMu so admission and queue discovery cannot race.
func (s *Service) requeueDueRetries(ctx context.Context) {
	if s.shuttingDown.Load() {
		return
	}
	now := time.Now().UTC()
	for after, afterID := (time.Time{}), int64(0); ; {
		due, err := s.repo.ListArchivesDueForRetry(ctx, now, after, afterID, archiveRetryBatch)
		if err != nil {
			s.log.Error("archive retry: list due archives", "error", err)
			return
		}
		for i := range due {
			if s.shuttingDown.Load() || ctx.Err() != nil {
				return
			}
			v := &due[i]
			after, afterID = *v.NextRetryAt, v.ID
			log := s.log.With("video_id", v.ID)
			jobID, err := s.requeueArchive(ctx, v, true)
			switch {
			case err == nil:
				log.Info("archive retry queued", "job_id", jobID)
			case errors.Is(err, repository.ErrNotFound):
				// Cancelled or removed since the listing.
			case errors.Is(err, repository.ErrDuplicate):
				// The VOD was queued by hand in the meantime, so this row's retry
				// has nothing left to do.
				log.Warn("archive retry dropped: the VOD is queued elsewhere")
				if err := s.repo.ClearArchiveRetry(ctx, v.ID); err != nil && !errors.Is(err, repository.ErrNotFound) {
					log.Error("archive retry: clear superseded retry", "error", err)
				}
			default:
				log.Error("archive retry: requeue", "error", err)
			}
		}
		if len(due) < archiveRetryBatch || ctx.Err() != nil {
			return
		}
	}
}

// requeueArchive atomically creates a new job and returns its ID.
// It preserves finalized parts under the same media lock used by retention.
func (s *Service) requeueArchive(ctx context.Context, v *repository.Video, scheduledOnly bool) (string, error) {
	// Retention holds the same lock while removing saved parts, so admission
	// must finish before those parts can become eligible for deletion again.
	owned, err := s.storage.Lock(ctx, v.ID)
	if err != nil {
		return "", err
	}
	defer owned.Close()
	current, err := s.repo.GetVideo(ctx, v.ID)
	if err != nil {
		return "", err
	}
	if current.JobID != v.JobID || current.Source != repository.VideoSourceVOD || current.Status != repository.VideoStatusFailed || current.DeletedAt != nil || current.DeleteRequestedAt != nil {
		return "", repository.ErrNotFound
	}
	v = current
	// Use the video's current job; timestamp ordering cannot distinguish attempts created together.
	prev, err := s.repo.GetJob(ctx, v.JobID)
	if err != nil {
		return "", fmt.Errorf("load previous attempt: %w", err)
	}
	parts, err := s.repo.ListVideoParts(ctx, v.ID)
	if err != nil {
		return "", fmt.Errorf("list finalized parts: %w", err)
	}
	prevState, err := UnmarshalResumeState(prev.ResumeState)
	if err != nil {
		s.log.Warn("archive retry: previous checkpoint unreadable; starting the attempt without it", "video_id", v.ID, "error", err)
		prevState = nil
	}
	state := continuationResumeState(prevState, parts)
	checkpoint, err := state.MarshalJSON()
	if err != nil {
		return "", fmt.Errorf("encode retry checkpoint: %w", err)
	}
	jobID := uuid.NewString()
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if _, err := tx.CreateJob(ctx, &repository.JobInput{
			ID: jobID, VideoID: v.ID, BroadcasterID: v.BroadcasterID, ResumeState: checkpoint, Attempt: prev.Attempt + 1,
		}); err != nil {
			return err
		}
		return tx.RequeueArchiveVideo(ctx, v.ID, jobID, scheduledOnly)
	})
	if err != nil {
		return "", err
	}
	s.bus.NotifyVideoChange()
	s.publishArchiveQueue(eventbus.ArchiveQueued, v.ID)
	return jobID, nil
}

// continuationResumeState resumes after the last finalized part under that part's variant lock.
// An attempt without finalized parts starts fresh and retains only the archive poster URL.
func continuationResumeState(prev *ResumeState, parts []repository.VideoPart) *ResumeState {
	state := NewResumeState()
	if prev != nil {
		state.PosterURL = prev.PosterURL
	}
	var last *repository.VideoPart
	for i := range parts {
		p := &parts[i]
		if p.EndMediaSeq == nil || p.SizeBytes <= 0 {
			continue
		}
		if last == nil || p.PartIndex > last.PartIndex {
			last = p
		}
	}
	if last == nil {
		return state
	}
	state.SelectedQuality = last.Quality
	state.SelectedFPS = last.FPS
	state.SelectedCodec = last.Codec
	state.SegmentFormat = last.SegmentFormat
	state.CurrentPartIndex = last.PartIndex + 1
	state.PartStartMediaSequence = *last.EndMediaSeq + 1
	state.PartStarted = true
	state.AccountedFrontierMediaSeq = *last.EndMediaSeq
	return state
}

// RetryArchive immediately queues another attempt of a failed archive.
// ErrBusy means its worker is still exiting; ErrNotFound means it is no longer retryable.
func (s *Service) RetryArchive(ctx context.Context, videoID int64) error {
	if err := s.retryArchiveLocked(ctx, videoID); err != nil {
		return err
	}
	s.PumpArchiveQueue(ctx)
	return nil
}

func (s *Service) retryArchiveLocked(ctx context.Context, videoID int64) error {
	s.pumpMu.Lock()
	defer s.pumpMu.Unlock()
	v, err := s.repo.GetVideo(ctx, videoID)
	if err != nil {
		return err
	}
	if v.Source != repository.VideoSourceVOD || v.Status != repository.VideoStatusFailed || v.DeletedAt != nil {
		return repository.ErrNotFound
	}
	s.mu.Lock()
	for _, d := range s.active {
		if d.videoID == videoID {
			s.mu.Unlock()
			return ErrBusy
		}
	}
	s.mu.Unlock()
	_, err = s.requeueArchive(ctx, v, false)
	return err
}

func (s *Service) startArchiveRetryLoop() {
	// Admission shares the lock used by Shutdown and job reservations. Once
	// shutdown starts no new worker can Add after its Wait observes zero.
	s.mu.Lock()
	if s.shuttingDown.Load() || s.retryCancel != nil {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.retryCancel = cancel
	s.discoveryWG.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.discoveryWG.Done()
		interval := s.retryInterval
		if interval <= 0 {
			interval = archiveRetryPumpInterval
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				recoveryCtx, cancelRecovery := context.WithTimeout(ctx, 30*time.Second)
				if err := s.resumeRunning(recoveryCtx); err != nil && ctx.Err() == nil {
					s.log.Warn("recording recovery deferred", "error", err)
				}
				if err := s.storage.Reconcile(recoveryCtx); err != nil && ctx.Err() == nil {
					s.log.Warn("media cleanup deferred", "error", err)
				}
				cancelRecovery()
				s.pumpWithTimeout(ctx)
			}
		}
	}()
}

func (s *Service) failQueuedArchive(ctx context.Context, job *repository.Job, cause error) error {
	s.log.Error("archive queue: start job failed",
		"job_id", job.ID, "video_id", job.VideoID, "error", cause)
	msg := fmt.Sprintf("start archive: %v", cause)
	if err := s.markRecordingFailed(ctx, repository.AttemptClaim{JobID: job.ID, VideoID: job.VideoID, ExecutionID: job.ExecutionID}, msg, repository.CompletionKindComplete, false); err != nil {
		s.log.Error("archive queue: persist start failure", "video_id", job.VideoID, "error", err)
		return err
	}
	s.publishArchiveQueue(eventbus.ArchiveFailed, job.VideoID)
	s.publishRecordingTerminal(job.VideoID, eventbus.RecordingFailed)
	return nil
}

// DequeueArchive removes an archive before acquisition starts.
// It returns ErrBusy for a running archive and ErrNotFound for a missing or live recording.
func (s *Service) DequeueArchive(ctx context.Context, videoID int64) error {
	s.pumpMu.Lock()
	defer s.pumpMu.Unlock()
	s.mu.Lock()
	for _, d := range s.active {
		if d.videoID == videoID {
			s.mu.Unlock()
			return ErrBusy
		}
	}
	s.mu.Unlock()
	if err := s.repo.DeleteQueuedArchiveVideo(ctx, videoID); err != nil {
		return err
	}
	s.publishArchiveQueue(eventbus.ArchiveDequeued, videoID)
	return nil
}

func (s *Service) pumpAfterJobEnd() {
	s.pumpWithTimeout(context.Background())
}

func (s *Service) pumpWithTimeout(parent context.Context) {
	if s.shuttingDown.Load() {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	s.PumpArchiveQueue(ctx)
}

// CancelArchiveRetry drops the scheduled retry of a failed archive, which
// releases the VOD for a fresh enqueue. repository.ErrNotFound when no retry
// is scheduled for that id.
func (s *Service) CancelArchiveRetry(ctx context.Context, videoID int64) error {
	if err := s.repo.ClearArchiveRetry(ctx, videoID); err != nil {
		return err
	}
	s.bus.NotifyVideoChange()
	s.publishArchiveQueue(eventbus.ArchiveRetryCancelled, videoID)
	return nil
}

// publishArchiveQueue tells subscribers the archive queue changed. Nil-safe
// and non-blocking like every bus publish; the row is the source of truth.
func (s *Service) publishArchiveQueue(kind eventbus.ArchiveQueueKind, videoID int64) {
	if s.bus == nil || s.bus.ArchiveQueue == nil {
		return
	}
	s.bus.ArchiveQueue.Publish(eventbus.ArchiveQueueEvent{Kind: kind, VideoID: videoID, At: time.Now().UTC()})
}
