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
	"github.com/befabri/replayvod/server/internal/recordingwebhook"
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

// ArchiveMaxConcurrent is the cap on VOD archives running at once. Archives
// have their own slots so a long back-catalogue download never keeps a live
// stream from being recorded.
func (s *Service) ArchiveMaxConcurrent() int {
	if s.cfg == nil || s.cfg.App.Download.ArchiveMaxConcurrent <= 0 {
		return 1
	}
	return s.cfg.App.Download.ArchiveMaxConcurrent
}

func (s *Service) activeLiveCountLocked() int {
	n := 0
	for _, d := range s.active {
		if !d.vod {
			n++
		}
	}
	return n
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

// segmentHostConnectionCap sizes the shared segment transport for every job
// that can run at once: live recordings and archives hold separate slots, and
// each drives SegmentConcurrency connections to the edge.
func segmentHostConnectionCap(cfg config.DownloadConfig) int {
	return (max(1, cfg.MaxConcurrent) + max(1, cfg.ArchiveMaxConcurrent)) * max(1, cfg.SegmentConcurrency)
}

// archiveRateLimiter is the per-job limiter for an archive, or nil when
// archives are not throttled. The bucket holds one second of the cap, so a
// segment read never asks for more than the limiter can grant.
func archiveRateLimiter(cfg config.DownloadConfig) hls.RateLimiter {
	bps := cfg.ArchiveMaxBytesPerSecond
	if bps <= 0 {
		return nil
	}
	return rate.NewLimiter(rate.Limit(bps), int(min(bps, 1<<30)))
}

// archiveRetryDelay is the wait before the retry that follows a failed
// attempt, or false once the attempts are used up.
func archiveRetryDelay(attempt int32) (time.Duration, bool) {
	if attempt < 1 || int(attempt) > len(archiveRetryBackoff) {
		return 0, false
	}
	return archiveRetryBackoff[attempt-1], true
}

// archiveRetryable reports whether an archive failure deserves another
// attempt on its own: network trouble, an overloaded or erroring edge, or a
// playback resolution that could not be renewed in its budget. Refusals that
// will not change (a deleted or restricted VOD, an empty playback token),
// local pipeline errors, and cancellations are not retried.
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

// EnqueueVOD records a VOD archive request as a PENDING video + job pair and
// waits in the queue until PumpArchiveQueue picks it up. Callers pump once
// after enqueueing their batch. The returned job id is the row's key either
// way. ErrDuplicate surfaces when an open archive of the same VOD already
// exists (the partial unique index on twitch_video_id).
//
// No title or category span is opened: an archive keeps the VOD title it was
// queued with and tracks no live metadata, so the watch page reads the row
// title and shows no history.
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
	var videoID int64
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		vid, err := tx.CreateVideo(ctx, &repository.VideoInput{
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
		})
		if err != nil {
			return fmt.Errorf("create video row: %w", err)
		}
		videoID = vid.ID
		_, err = tx.CreateJob(ctx, &repository.JobInput{
			ID: jobID, VideoID: vid.ID, BroadcasterID: p.BroadcasterID, ResumeState: checkpoint, Attempt: 1,
		})
		return err
	})
	if err != nil {
		return "", fmt.Errorf("enqueue archive: %w", err)
	}
	s.publishArchiveQueue(eventbus.ArchiveQueued, videoID)
	return jobID, nil
}

// PumpArchiveQueue requeues archives whose retry is due, then starts queued
// archives, oldest first, until the archive slots are full or the queue is
// empty. It runs on enqueue, whenever a job ends, at boot after Resume, and
// on the retry ticker. Pumps are serialized so two callers can never start
// the same job, and a job is marked RUNNING before its goroutine spawns so
// the next pick never sees it as still queued.
func (s *Service) PumpArchiveQueue(ctx context.Context) {
	s.pumpMu.Lock()
	defer s.pumpMu.Unlock()
	s.requeueDueRetries(ctx)
	for {
		if s.shuttingDown.Load() {
			return
		}
		s.mu.Lock()
		free := s.activeArchiveCountLocked() < s.ArchiveMaxConcurrent()
		s.mu.Unlock()
		if !free {
			return
		}
		job, err := s.repo.GetNextQueuedArchiveJob(ctx)
		if err != nil {
			if !errors.Is(err, repository.ErrNotFound) {
				s.log.Error("archive queue: pick next job", "error", err)
			}
			return
		}
		// Claim both rows atomically. A failed write leaves a queued pair that
		// the next pump can retry, never a running job with a pending video.
		if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
			if err := tx.MarkJobRunning(ctx, job.ID); err != nil {
				return err
			}
			return tx.UpdateVideoStatus(ctx, job.VideoID, repository.VideoStatusRunning)
		}); err != nil {
			s.log.Error("archive queue: claim job", "job_id", job.ID, "error", err)
			return
		}
		if err := s.restartJob(ctx, job); err != nil {
			if errors.Is(err, ErrShuttingDown) || ctx.Err() != nil {
				return // RUNNING is reclaimed by Resume on the next boot.
			}
			s.failQueuedArchive(ctx, job, err)
			continue
		}
		s.publishArchiveQueue(eventbus.ArchiveStarted, job.VideoID)
	}
}

// requeueDueRetries turns every archive whose scheduled retry has come into a
// queued attempt. Runs under pumpMu so the pick that follows sees the rows.
func (s *Service) requeueDueRetries(ctx context.Context) {
	if s.shuttingDown.Load() {
		return
	}
	due, err := s.repo.ListArchivesDueForRetry(ctx, time.Now().UTC(), archiveRetryBatch)
	if err != nil {
		s.log.Error("archive retry: list due archives", "error", err)
		return
	}
	for i := range due {
		if s.shuttingDown.Load() || ctx.Err() != nil {
			return
		}
		v := &due[i]
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
}

// requeueArchive creates the next attempt of a failed archive: a fresh job
// whose resume state continues after the parts earlier attempts finalized,
// and the video back in PENDING. Both rows change in one transaction, so a
// job never exists for a row that stayed FAILED.
func (s *Service) requeueArchive(ctx context.Context, v *repository.Video, scheduledOnly bool) (string, error) {
	// The row's job_id is the attempt that failed; jobs accumulate per attempt
	// and created_at ordering cannot tell two of them apart within a second.
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
	s.publishArchiveQueue(eventbus.ArchiveQueued, v.ID)
	return jobID, nil
}

// continuationResumeState seeds a retry so it appends to the parts earlier
// attempts finalized instead of downloading them again. It is ContinuePart
// for a fresh attempt: the next part starts at the media sequence after the
// last finalized one, under that part's variant lock, so the resolved URL
// must land on the same rendition (media sequences are not shared across
// renditions) and a genuine change surfaces as ErrVariantChanged instead of
// a silent mix. The lock comes from the part row, not the failed attempt's
// checkpoint, which may never have resolved a rendition. With no finalized
// part the attempt starts fresh; only the poster URL carries over.
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

// RetryArchive requeues a failed archive right away, whether or not a retry
// was scheduled. ErrBusy when the archive is still winding down, and from the
// repository ErrNotFound when no failed archive has that id or ErrDuplicate
// when another open row already holds the VOD.
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

// startArchiveRetryLoop pumps the queue periodically so a scheduled retry
// starts on time even when no job ends and nothing is enqueued. Runs once
// per service; Shutdown stops it.
func (s *Service) startArchiveRetryLoop() {
	if s.stopRetry == nil {
		return // A Service built without NewService (tests) has no loop to stop.
	}
	s.retryOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(archiveRetryPumpInterval)
			defer ticker.Stop()
			for {
				select {
				case <-s.stopRetry:
					return
				case <-ticker.C:
					s.pumpAfterJobEnd()
				}
			}
		}()
	})
}

// failQueuedArchive records a start failure on a queued archive. Nothing was
// captured, so the row fails "complete" and not truncated, like a live job
// that never got past setup.
func (s *Service) failQueuedArchive(ctx context.Context, job *repository.Job, cause error) {
	s.log.Error("archive queue: start job failed",
		"job_id", job.ID, "video_id", job.VideoID, "error", cause)
	msg := fmt.Sprintf("start archive: %v", cause)
	_ = s.repo.MarkJobFailed(ctx, job.ID, msg)
	delivery := s.recordingWebhookDelivery(job.VideoID, recordingwebhook.EventFailed)
	if err := s.repo.MarkVideoFailedAndEnqueueRecordingWebhook(ctx, job.VideoID, msg, repository.CompletionKindComplete, false, delivery); err != nil {
		s.log.Error("archive queue: mark video failed", "video_id", job.VideoID, "error", err)
		return
	}
	s.publishArchiveQueue(eventbus.ArchiveFailed, job.VideoID)
	s.publishRecordingTerminal(job.VideoID, eventbus.RecordingFailed)
}

// DequeueArchive removes an archive that has not started. A running archive
// is refused with ErrBusy (stop it with Cancel instead); a missing or live row
// is repository.ErrNotFound. The pump lock keeps the queue from starting the
// row between the check and the delete.
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

// pumpAfterJobEnd is run()'s exit hook and the retry ticker's body: a
// finished job of either kind may have freed an archive slot (a live job
// never holds one, but the check is cheap and keeps the rule in one place),
// and a scheduled retry may have come due.
func (s *Service) pumpAfterJobEnd() {
	if s.shuttingDown.Load() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
