package playbackcache

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/befabri/replayvod/server/internal/mediastore"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/google/uuid"
)

func (s *Service) BuildNow(ctx context.Context, videoID int64) error {
	settings, err := playbackSettings(ctx, s.repo)
	if err != nil {
		return err
	}
	// Zero percent disables caching; treating it as an active zero-byte budget
	// would erase existing artifacts.
	if !settings.active() || !settings.autoGenerate {
		return nil
	}
	if err := s.storageReady(ctx); err != nil {
		return err
	}
	// Preflight and its building row must not outlive a completed purge.
	// Release ownership for expensive concat, then acquire it for publication.
	unlock, err := s.store.Lock(ctx, videoID)
	if err != nil {
		return err
	}
	defer func() {
		if unlock != nil {
			unlock.Close()
		}
	}()

	video, err := s.repo.GetVideo(ctx, videoID)
	if err != nil {
		return err
	}
	if video.Status != repository.VideoStatusDone || video.DeletedAt != nil {
		return nil
	}

	asset, assetErr := s.repo.GetVideoPlaybackAsset(ctx, videoID)
	if assetErr != nil && !errors.Is(assetErr, repository.ErrNotFound) {
		return assetErr
	}
	keep := ""
	if assetErr == nil && asset.Filename != nil {
		keep = storagekeys.Video(*asset.Filename)
	}
	if err := unlock.PrunePublications(ctx, func(p repository.MediaPublication) bool {
		return p.Key != keep && strings.HasPrefix(p.Key, storagekeys.Video(video.Filename+"-playback-"))
	}); err != nil {
		return err
	}
	switch {
	case assetErr == nil && asset.Status == repository.PlaybackAssetStatusReady && asset.Filename != nil:
		exists, existsErr := s.store.Exists(ctx, storagekeys.Video(*asset.Filename))
		if existsErr != nil {
			// A failed existence probe cannot establish that the ready artifact is missing.
			s.log.Warn("probe existing playback artifact failed; skipping rebuild",
				"video_id", videoID, "error", existsErr)
			return nil
		}
		if exists {
			return nil
		}
	case assetErr != nil && !errors.Is(assetErr, repository.ErrNotFound):
		return assetErr
	}

	parts, err := s.repo.ListVideoParts(ctx, videoID)
	if err != nil {
		return err
	}
	ordered := orderedParts(parts)
	if len(ordered) < 2 {
		return nil
	}
	if ok, reason := canCopyConcat(ordered); !ok {
		return s.markUnavailable(ctx, videoID, reason)
	}

	budget, err := s.capacity(ctx, settings.maxPercent, s.currentCacheBytes(ctx))
	if err != nil {
		return err
	}
	expectedBytes := expectedSize(ordered)

	// Missing source parts make concat unavailable. Check before the budget
	// defer so playback gets a verdict even when the disk is full.
	if missing, err := s.firstMissingPart(ctx, ordered); err != nil {
		return err
	} else if missing != "" {
		return s.markUnavailable(ctx, videoID, fmt.Sprintf("source part %q is missing", missing))
	}

	// Insufficient headroom must remain retryable after capacity increases; the
	// muxing margin avoids builds whose estimated output barely fits the limit.
	requiredBytes := expectedBytes + expectedBytes/buildOvershootMarginDivisor
	if budget.known && requiredBytes > budget.buildHeadroom {
		s.log.Debug("deferring playback build: estimate exceeds build headroom",
			"video_id", videoID, "estimate_bytes", expectedBytes,
			"required_bytes", requiredBytes, "build_headroom", budget.buildHeadroom,
			"configured_cap", budget.configured)
		return nil
	}

	// Each build needs a unique key because an earlier ambiguous upload can finish
	// after this build publishes; the journal retains abandoned outputs for cleanup.
	if assetErr == nil && asset.Filename != nil {
		if err := unlock.Delete(ctx, storagekeys.Video(*asset.Filename)); err != nil {
			return err
		}
	}
	filename := video.Filename + "-playback-" + uuid.NewString() + partExtension(ordered[0])
	if err := s.storageReady(ctx); err != nil {
		return err
	}
	if _, err := s.repo.UpsertVideoPlaybackAsset(ctx, &repository.VideoPlaybackAssetInput{
		VideoID: videoID,
		Status:  repository.PlaybackAssetStatusBuilding,
	}); err != nil {
		return err
	}

	unlock.Close()
	unlock = nil
	artifact, buildErr := s.buildArtifact(ctx, ordered)
	if artifact != nil {
		defer artifact.cleanup()
	}

	// Recheck deletion under recording ownership before publishing; concat runs
	// outside this lock so retention does not wait for ffmpeg.
	unlock, err = s.store.Lock(ctx, videoID)
	if err != nil {
		return errors.Join(buildErr, err)
	}
	// Once owned, terminal bookkeeping must survive a client disconnect or
	// shutdown. Waiting for ownership itself remains cancellable.
	detached := context.WithoutCancel(ctx)
	fresh, err := s.repo.GetVideo(detached, videoID)
	switch {
	case err != nil && !errors.Is(err, repository.ErrNotFound):
		return errors.Join(buildErr, fmt.Errorf("re-check video before commit: %w", err))
	case errors.Is(err, repository.ErrNotFound) || fresh.Status != repository.VideoStatusDone || fresh.DeletedAt != nil:
		// Retention may already have removed the building row while concat ran.
		return s.repo.DeleteVideoPlaybackAsset(detached, videoID)
	}

	// Keep storage failures retryable and preserve same-named files on replacement
	// volumes.
	if gateErr := s.storageReady(detached); gateErr != nil {
		if storage.CanDelete(gateErr) {
			return errors.Join(buildErr, gateErr, s.deleteArtifact(detached, unlock, filename))
		}
		return errors.Join(buildErr, gateErr)
	}
	if buildErr != nil {
		return s.failBuild(detached, unlock, videoID, filename, buildErr)
	}
	size := artifact.size
	if budget.known && size > budget.configured {
		if err := s.deleteArtifact(detached, unlock, filename); err != nil {
			return err
		}
		return s.markUnavailable(detached, videoID,
			fmt.Sprintf("playback artifact %d exceeds cache cap %d", size, budget.configured))
	}

	publishErr := s.publishArtifact(ctx, unlock, filename, artifact)
	if gateErr := s.storageReady(detached); gateErr != nil {
		if storage.CanDelete(gateErr) {
			return errors.Join(publishErr, gateErr, s.deleteArtifact(detached, unlock, filename))
		}
		return errors.Join(publishErr, gateErr)
	}
	if publishErr != nil {
		return s.failBuild(detached, unlock, videoID, filename, publishErr)
	}
	at := time.Now().UTC()
	mime := mimeTypeForExtension(partExtension(ordered[0]))
	duration := totalDuration(ordered)
	ready := &repository.VideoPlaybackAssetInput{
		VideoID:         videoID,
		Status:          repository.PlaybackAssetStatusReady,
		Filename:        &filename,
		MimeType:        &mime,
		DurationSeconds: &duration,
		SizeBytes:       &size,
		GeneratedAt:     &at,
		LastAccessedAt:  &at,
	}
	unlock.Close()
	unlock = nil
	if err := s.settleReady(ctx, videoID, ready); err != nil {
		return err
	}
	// Publication is complete. Opportunistic pruning can wait on other
	// recordings, so it must retain the build's cancellation and deadline.
	if err := s.pruneWithSettings(ctx, settings); err != nil {
		s.log.Warn("playback cache prune failed", "error", err)
	}
	return nil
}

// firstMissingPart returns the filename of the first part whose stored object is
// absent, or "" when all parts are present.
func (s *Service) firstMissingPart(ctx context.Context, parts []repository.VideoPart) (string, error) {
	for _, part := range parts {
		if err := s.storageReady(ctx); err != nil {
			return "", err
		}
		exists, err := s.store.Exists(ctx, storagekeys.Video(part.Filename))
		if err != nil {
			return "", err
		}
		if !exists {
			return part.Filename, nil
		}
	}
	return "", nil
}

func (s *Service) markUnavailable(ctx context.Context, videoID int64, reason string) error {
	if err := s.storageReady(ctx); err != nil {
		return err
	}
	_, err := s.repo.UpsertVideoPlaybackAsset(ctx, &repository.VideoPlaybackAssetInput{
		VideoID: videoID,
		Status:  repository.PlaybackAssetStatusUnavailable,
		Error:   nonEmpty(reason),
	})
	return err
}

// failBuild runs with recording ownership held and trusted storage verified.
func (s *Service) failBuild(ctx context.Context, owned *mediastore.Recording, videoID int64, filename string, buildErr error) error {
	if cleanupErr := s.deleteArtifact(ctx, owned, filename); cleanupErr != nil {
		return errors.Join(buildErr, cleanupErr)
	}
	if errors.Is(buildErr, context.Canceled) {
		// A graceful interruption remains retryable without a failed verdict.
		return errors.Join(buildErr, s.repo.DeleteVideoPlaybackAsset(ctx, videoID))
	}
	_, persistErr := s.repo.UpsertVideoPlaybackAsset(ctx, &repository.VideoPlaybackAssetInput{
		VideoID: videoID,
		Status:  repository.PlaybackAssetStatusFailed,
		Error:   nonEmpty(errorPreview(buildErr)),
	})
	return errors.Join(buildErr, persistErr)
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func errorPreview(err error) string {
	const max = 4 << 10
	msg := err.Error()
	if len(msg) <= max {
		return msg
	}
	return msg[:max] + "\n..."
}

// settleReady retries only the completed output's facts. Locks are reacquired
// for each bounded write and are released before retrying or joining workers.
func (s *Service) settleReady(ctx context.Context, id int64, input *repository.VideoPlaybackAssetInput) error {
	for {
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		owned, err := s.store.Lock(writeCtx, id)
		if err == nil {
			var video *repository.Video
			video, err = s.repo.GetVideo(writeCtx, id)
			if err == nil && (video.Status != repository.VideoStatusDone || video.DeletedAt != nil || video.DeleteRequestedAt != nil) {
				err = repository.ErrStaleExecution
			}
			if err == nil {
				err = owned.Commit(writeCtx, func(tx repository.Repository) error {
					_, err := tx.UpsertVideoPlaybackAsset(writeCtx, input)
					return err
				})
			}
			owned.Close()
		}
		cancel()
		if err == nil || errors.Is(err, repository.ErrStaleExecution) || errors.Is(err, repository.ErrNotFound) || ctx.Err() != nil {
			return err
		}
		s.log.Warn("playback outcome persistence deferred", "video_id", id, "error", err)
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
	}
}
