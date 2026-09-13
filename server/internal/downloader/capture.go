package downloader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/befabri/replayvod/server/internal/downloader/hls"
)

type capturedPartStep int

const (
	finalizeCapturedPart capturedPartStep = iota
	captureNextPart
	captureFinished
)

func (a *recordingAttempt) captureParts(ctx context.Context, jobDir string, parts []partResult, observers *captureObservers) ([]partResult, error) {
	s, d := a.service, a.download
	dbCtx := context.WithoutCancel(ctx)
	if d.resume.CurrentPartIndex > 1 {
		a.emitter.setPart(int(d.resume.CurrentPartIndex))
	}
	for {
		segmentsDir := filepath.Join(jobDir, fmt.Sprintf("part%02d", d.resume.CurrentPartIndex), "segments")
		result, err := a.capturePart(ctx, segmentsDir, len(parts), observers)
		if err != nil {
			return nil, err
		}
		step, err := a.prepareCapturedPart(ctx, segmentsDir, len(parts), result)
		if err != nil {
			return nil, err
		}
		switch step {
		case captureFinished:
			return parts, nil
		case captureNextPart:
			continue
		}
		part, err := a.finalizePart(ctx, segmentsDir, len(parts), result, observers)
		if err != nil {
			return nil, err
		}
		parts = append(parts, *part)
		if pendingSplitEndedAtBoundary(d.resume, result) {
			d.resume.ClearPendingSplit()
			s.checkpointResume(dbCtx, d, a.log)
			return parts, nil
		}
		advanced, err := s.continueAfterPendingSplit(dbCtx, d, a.emitter, a.log)
		if err != nil || !advanced {
			return parts, err
		}
	}
}

func (a *recordingAttempt) capturePart(ctx context.Context, segmentsDir string, priorParts int, observers *captureObservers) (*hls.JobResult, error) {
	s, d, log := a.service, a.download, a.log
	dbCtx := context.WithoutCancel(ctx)
	// A saved split can finalize before a new part reaches acquisition. Verify
	// identity at that boundary before starting any live-channel effects.
	if err := s.recoverCaptureIdentity(ctx, d, a.params); err != nil {
		d.persistenceErr = err
		return nil, err
	}
	if err := os.MkdirAll(segmentsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create part scratch dir: %w", err)
	}
	// A checkpoint past SEGMENTS or a saved split already owns this part's
	// capture. Fetching again could append new media into a sealed boundary.
	if shouldSkipSegmentFetch(d.resume) {
		kind := segmentKindForResume(segmentsDir, d.resume)
		if d.resume.SegmentFormat == "" {
			d.resume.SegmentFormat = string(kind)
			s.checkpointResume(dbCtx, d, log)
		}
		log.Info("resume: skipping segment fetch, checkpoint past SEGMENTS",
			"stage", d.resume.Stage,
			"part_index", d.resume.CurrentPartIndex,
			"accounted_frontier", d.resume.AccountedFrontierMediaSeq,
			"segment_format", d.resume.SegmentFormat,
			"pending_split", d.resume.PendingSplit,
			"pending_threshold_split", d.resume.PendingThresholdSplit)
		return synthesizeHLSResultFromResume(d.resume, kind), nil
	}
	if err := observers.start(ctx); err != nil {
		d.persistenceErr = err
		return nil, err
	}
	s.setResumeStage(dbCtx, d, StageSegments, log)
	result, err := s.fetchWithAuthRefresh(ctx, dbCtx, d, a.emitter, a.params, segmentsDir, a.selectOpts, log)
	if err != nil {
		result, err = a.resolveCaptureError(ctx, segmentsDir, priorParts, result, err)
		if err != nil {
			return nil, err
		}
	}
	a.emitter.finalize()
	return result, nil
}

// resolveCaptureError checkpoints tolerated capture endings before remux. An
// interrupted playback attempt can publish saved media but must remain failed.
func (a *recordingAttempt) resolveCaptureError(ctx context.Context, segmentsDir string, priorParts int, result *hls.JobResult, captureErr error) (*hls.JobResult, error) {
	s, d, log := a.service, a.download, a.log
	dbCtx := context.WithoutCancel(ctx)
	switch {
	case errors.Is(captureErr, errBroadcastLookup):
		return nil, captureErr
	case s.liveCaptureEnded(ctx, d, a.params, captureErr):
		at := s.now()
		d.resume.CaptureStoppedAt = &at
		d.resume.SetStage(StagePrepareInput)
		d.resume.ClearPendingSplit()
		s.checkpointResume(dbCtx, d, log)
		if result == nil {
			result = synthesizeHLSResultFromResume(d.resume, segmentKindForResume(segmentsDir, d.resume))
		}
	case isSplitSignal(captureErr) && hasPartContent(result, d.resume):
		// Check content to prevent an endlessly broken variant creating empty
		// parts. Persist the split before remux so recovery still opens part N+1.
		d.resume.PendingSplit = true
		s.checkpointResume(dbCtx, d, log)
		var segmentsDone int64
		if result != nil {
			segmentsDone = result.SegmentsDone
		}
		log.Info("split triggered; opening new part",
			"part_index", d.resume.CurrentPartIndex,
			"segments_done", segmentsDone,
			"prior_frontier", d.resume.AccountedFrontierMediaSeq,
			"reason", captureErr)
	case shouldAcceptEmptySplitSignal(priorParts, result, captureErr):
		d.resume.PendingSplit = true
		s.checkpointResume(dbCtx, d, log)
		log.Info("split triggered on empty continuation; opening next part without remux",
			"part_index", d.resume.CurrentPartIndex,
			"parts", priorParts,
			"reason", captureErr)
	case ctx.Err() == nil && isPlaybackResolutionFailure(captureErr) && currentPartHasCommittedMedia(result, d.resume):
		// Save failure intent before remux so recovery neither reacquires live
		// media nor turns an interrupted recording into successful completion.
		d.resume.CaptureError = playbackCaptureFailure(captureErr)
		d.resume.CaptureRetryable = archiveRetryable(captureErr)
		d.resume.SetStage(StagePrepareInput)
		if result.Kind != "" {
			d.resume.SegmentFormat = string(result.Kind)
		}
		if result.Kind == "" {
			result = synthesizeHLSResultFromResume(d.resume, segmentKindForResume(segmentsDir, d.resume))
		}
		s.checkpointResume(dbCtx, d, log)
		log.Warn("playback recovery failed; finalizing captured media", "reason", d.resume.CaptureError)
	default:
		return nil, captureErr
	}
	return result, nil
}

// prepareCapturedPart decides whether captured media can be sealed, an empty
// split needs reanchoring, or the earlier parts already cover the whole stream.
func (a *recordingAttempt) prepareCapturedPart(ctx context.Context, segmentsDir string, priorParts int, result *hls.JobResult) (capturedPartStep, error) {
	s, d, log := a.service, a.download, a.log
	dbCtx := context.WithoutCancel(ctx)
	// ENDLIST proves completion only when the parts durably own all media.
	// A threshold split can still have a post-boundary tail to prune/refetch.
	resumeChanged := false
	if result != nil && result.EndList {
		if thresholdSplitEndListAtBoundary(d.resume, result) && !d.resume.PendingSplitEndListAtBoundary {
			d.resume.PendingSplitEndListAtBoundary = true
			resumeChanged = true
		}
		if shouldPersistEndListSeen(d.resume, result) && !d.resume.EndListSeen {
			d.resume.EndListSeen = true
			resumeChanged = true
		}
	}
	if captureHadWindowRoll(d.resume) {
		resumeChanged = true
	}
	sealedThresholdSplit := d.resume.PendingThresholdSplit && d.resume.PendingSplitBoundarySet

	// Empty guards consider both this fetch and durable committed media. An
	// empty fetch after recovery must not discard a previously captured part.
	if shouldFinalizeEmptyContinuation(priorParts, result, d.resume) {
		d.resume.SetStage(StagePrepareInput)
		s.checkpointResume(dbCtx, d, log)
		log.Info("continuation part captured no segments (stream ended at split boundary); finalizing recording",
			"part_index", d.resume.CurrentPartIndex, "parts", priorParts)
		return captureFinished, nil
	}
	if resumeChanged {
		s.checkpointResume(dbCtx, d, log)
	}
	if shouldSkipEmptySplitPart(priorParts, result, d.resume) {
		log.Info("split continuation resolved no media; opening next part without remux",
			"part_index", d.resume.CurrentPartIndex,
			"parts", priorParts,
			"pending_threshold_split", d.resume.PendingThresholdSplit)
		advanced, err := s.reanchorCurrentPartAfterEmptySplit(dbCtx, d, a.emitter, log)
		if err != nil || !advanced {
			return captureFinished, err
		}
		return captureNextPart, nil
	}
	if priorParts > 0 && !currentPartHasCommittedMedia(result, d.resume) {
		cause := ctx.Err()
		if cause == nil {
			cause = errors.New("continuation part captured no segments before ENDLIST")
		}
		return captureFinished, cause
	}
	if sealedThresholdSplit {
		if err := pruneSegmentsAfterBoundary(segmentsDir, result.Kind, d.resume.PendingSplitBoundaryMediaSeq, d.workspace); err != nil {
			return captureFinished, fmt.Errorf("prune threshold split tail: %w", err)
		}
	}
	return finalizeCapturedPart, nil
}

func (a *recordingAttempt) finalizePart(ctx context.Context, segmentsDir string, priorParts int, result *hls.JobResult, observers *captureObservers) (*partResult, error) {
	s, d := a.service, a.download
	if !d.vod && (d.resume.EndListSeen || d.resume.CaptureStoppedAt != nil) {
		if err := a.finishLiveCapture(ctx, observers); err != nil {
			return nil, err
		}
		if priorParts == 0 && !currentPartHasCommittedMedia(result, d.resume) {
			// Identity can become obsolete before a variant or media exists.
			return nil, errors.New("broadcast ended before any media was captured")
		}
	}
	part, err := s.runPart(ctx, context.WithoutCancel(ctx), d, a.params, a.filename, segmentsDir, result, a.emitter, a.log)
	if err != nil {
		return nil, err
	}
	if d.resume.CaptureError != "" {
		return nil, sealedCaptureFailure(d.resume)
	}
	d.completedMediaDurationSeconds += part.durationSeconds
	// The sealed duration is already folded into completedMediaDurationSeconds.
	// refreshMediaOffset would count it twice until the next split resets resume.
	d.setMediaOffset(d.completedMediaDurationSeconds, len(d.resume.AuthGapSeqs()) == 0)
	return part, nil
}
