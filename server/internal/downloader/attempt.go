package downloader

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
)

// recordingAttempt shares execution inputs across capture and completion. The
// download remains the owner of durable resume state and runtime progress.
type recordingAttempt struct {
	service    *Service
	download   *download
	params     Params
	filename   string
	log        *slog.Logger
	emitter    *progressEmitter
	selectOpts twitch.SelectOptions
}

func (s *Service) runAttempt(ctx context.Context, d *download, p Params, filename string) {
	log := s.log.With("job_id", d.jobID, "broadcaster_login", p.BroadcasterLogin)
	if d.vod {
		log = log.With("vod_id", p.VODID)
	}
	a := &recordingAttempt{service: s, download: d, params: p, filename: filename, log: log}
	if !a.claim(ctx) {
		return
	}
	a.startArchive(ctx)
	a.configureCapture()
	dbCtx := context.WithoutCancel(ctx)

	// All parts share the same recoverable workspace. The reservation releases
	// it after settlement; shutdown preserves its files for the next attempt.
	jobDir := filepath.Join(s.cfg.Env.ScratchDir, d.jobID)
	workspace, err := s.storage.Scratch().Open(jobDir, 0)
	if err != nil {
		d.persistenceErr = err
		s.failDownload(dbCtx, d, log, err)
		return
	}
	d.workspace = workspace
	ctx, stopScratch := workspace.Monitor(ctx)
	defer stopScratch()
	d.runCtx = ctx

	observers := newCaptureObservers(ctx, a)
	d.stopChildren = observers.stop
	defer observers.close()

	parts, err := a.restoreParts(dbCtx)
	if err == nil {
		parts, err = a.captureParts(ctx, jobDir, parts, observers)
	}
	if err == nil {
		err = a.finishLiveCapture(ctx, observers)
	}
	if err != nil {
		s.failDownload(dbCtx, d, log, err)
		return
	}
	a.complete(ctx, parts)
}

// claim resolves recovery ownership before any media work. An unresolved claim
// remains recoverable; a durable stop request must instead be settled.
func (a *recordingAttempt) claim(ctx context.Context) bool {
	s, d, log := a.service, a.download, a.log
	if err := s.recoverCaptureIdentity(ctx, d, a.params); err != nil {
		log.Warn("broadcast recovery deferred", "error", err)
		return false
	}
	if err := s.claimAttempt(ctx, d, d.previousExecutionID); err != nil {
		if errors.Is(err, repository.ErrStopRequested) {
			var job *repository.Job
			readErr := s.persist(ctx, "read stopped attempt", func(c context.Context) error {
				var err error
				job, err = s.repo.GetJob(c, d.jobID)
				return err
			})
			if readErr == nil {
				readErr = s.failUnrecoverableAttempt(ctx, job, ErrCancelled, true)
			}
			if readErr != nil {
				log.Error("cancelled admission settlement deferred", "error", readErr)
			}
			return false
		}
		log.Error("recording claim unresolved; preserving recovery", "error", err)
		return false
	}
	dbCtx := context.WithoutCancel(ctx)
	if d.resume.CaptureStoppedAt != nil {
		s.checkpointResume(dbCtx, d, log)
		if d.persistenceErr != nil {
			s.failDownload(dbCtx, d, log, d.persistenceErr)
			return false
		}
	}
	if ctx.Err() != nil {
		s.failDownload(dbCtx, d, log, ctx.Err())
		return false
	}
	return true
}

func (a *recordingAttempt) startArchive(ctx context.Context) {
	s, d := a.service, a.download
	if !d.vod {
		return
	}
	s.publishArchiveQueue(eventbus.ArchiveStarted, d.videoID)
	if d.resume.PosterURL != "" && s.posters != nil {
		// Share the attempt's lifetime and preserve a frame finalized before a restart.
		if v, err := s.repo.GetVideo(ctx, d.videoID); err == nil && v.Thumbnail == nil {
			s.posters.Fetch(ctx, d.videoID, a.filename, d.resume.PosterURL)
		}
	}
}

func (a *recordingAttempt) configureCapture() {
	s, d, p := a.service, a.download, a.params
	// Selection and progress must use the same normalized recording type.
	recordingType := p.RecordingType
	if recordingType == "" {
		recordingType = twitch.RecordingTypeVideo
	}
	a.selectOpts = twitch.SelectOptions{
		RecordingType: recordingType,
		Quality:       p.qualityCap(),
		EnableAV1:     s.cfg.App.Download.EnableAV1,
		DisableHEVC:   s.cfg.App.Download.DisableHEVC,
		ForceH264:     p.ForceH264,
	}
	a.emitter = newProgressEmitter(d.jobID, recordingType, d.progressCh, func(snap Progress) {
		d.setProgress(snap)
		s.notifyActiveChanged()
	})
	a.emitter.setMediaOffsetSource(d.MediaOffsetSeconds)
}

// restoreParts seeds totals from finalized outputs and the current part's
// committed source bytes. The latter remain an estimate until that part seals.
func (a *recordingAttempt) restoreParts(ctx context.Context) ([]partResult, error) {
	d := a.download
	existingParts, err := a.service.repo.ListVideoParts(ctx, d.videoID)
	if err != nil {
		return nil, fmt.Errorf("list existing video parts: %w", err)
	}
	var parts []partResult
	var capturedBytes int64
	for _, ep := range existingParts {
		if ep.PartIndex >= d.resume.CurrentPartIndex {
			continue
		}
		var thumbRel string
		if ep.Thumbnail != nil {
			thumbRel = *ep.Thumbnail
		}
		parts = append(parts, partResult{
			filename:        ep.Filename,
			durationSeconds: ep.DurationSeconds,
			sizeBytes:       ep.SizeBytes,
			thumbRel:        thumbRel,
		})
		d.completedMediaDurationSeconds += ep.DurationSeconds
		capturedBytes += ep.SizeBytes
	}
	if d.resume != nil && d.resume.PartBytes > 0 {
		capturedBytes += d.resume.PartBytes
	}
	a.emitter.seedCompletedBytes(capturedBytes)
	d.refreshMediaOffset()
	return parts, nil
}

func (a *recordingAttempt) complete(ctx context.Context, parts []partResult) {
	s, d, log := a.service, a.download, a.log
	var duration float64
	var size int64
	var thumbnail *string
	for _, part := range parts {
		duration += part.durationSeconds
		size += part.sizeBytes
		// A monochrome first part must not hide a later usable thumbnail.
		if thumbnail == nil && part.thumbRel != "" {
			thumb := part.thumbRel
			thumbnail = &thumb
		}
	}
	recordingType := a.selectOpts.RecordingType
	if repository.NormalizeRecordingType(recordingType) == repository.RecordingTypeAudio {
		if err := s.persistAudioWaveform(ctx, d, a.filename, recordingType, duration, parts); err != nil {
			log.Warn("audio waveform artifact generation failed; watch page can rebuild it later",
				"video_id", d.videoID, "error", err)
		}
	}

	// Only a rolled restart window makes completion partial. Other gap reasons
	// are tolerated losses. Missing ENDLIST also means capture ended early.
	completionKind := repository.CompletionKindComplete
	if d.resume.HadWindowRoll {
		completionKind = repository.CompletionKindPartial
	}
	truncated := d.resume.HadWindowRoll || !d.resume.EndListSeen
	if err := s.waitForStorage(ctx); err != nil {
		s.failDownload(context.WithoutCancel(ctx), d, log, err)
		return
	}
	if err := s.finishAttempt(ctx, d, duration, size, thumbnail, completionKind, truncated); err != nil {
		log.Error("failed to persist recording completion; preserving attempt for recovery", "error", err)
		return
	}
	d.cleanupScratch = true
	a.emitter.setStage("done")
	log.Info("download complete", "parts", len(parts), "duration_seconds", duration, "size_bytes", size)
	// Wake delivery only after the terminal state and webhook outbox commit.
	// Playback concatenation stays lazy, in StreamHandler.streamPart.
	s.publishRecordingTerminal(d.videoID, eventbus.RecordingCompleted)
	if d.vod {
		s.publishArchiveQueue(eventbus.ArchiveCompleted, d.videoID)
	}
}

// partResult carries finalized media inputs and totals used for completion.
type partResult struct {
	filename        string
	localPath       string
	durationSeconds float64
	sizeBytes       int64
	thumbRel        string // storage-relative thumbnail path; "" when none
}
