package playbackcache

import (
	"context"
	"errors"
	"fmt"

	"github.com/befabri/replayvod/server/internal/mediastore"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

// Prune evicts ready artifacts in LRU order until the cache fits its budget.
// A disabled cache or unknown budget preserves existing artifacts.
func (s *Service) Prune(ctx context.Context) error {
	settings, err := playbackSettings(ctx, s.repo)
	if err != nil {
		return err
	}
	return s.pruneWithSettings(ctx, settings)
}

func (s *Service) pruneWithSettings(ctx context.Context, settings playbackConfig) error {
	if !settings.active() {
		return nil
	}
	if err := s.storageDeletionReady(ctx); err != nil {
		return err
	}
	total, err := s.repo.SumReadyPlaybackBytes(ctx)
	if err != nil {
		return err
	}
	budget, err := s.capacity(ctx, settings.maxPercent, total)
	if err != nil {
		return err
	}
	if !budget.known {
		return nil
	}
	// Cache eviction yields space to recordings; build admission prevents rebuilding
	// evicted artifacts until free space recovers.
	capBytes := budget.current
	for after := (repository.PlaybackAssetCursor{}); total > capBytes; {
		entries, err := s.repo.ListReadyVideoPlaybackAssets(ctx, after, 100)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			after = repository.PlaybackCursor(entry)
			if total <= capBytes {
				break
			}
			pruned, err := s.pruneEntry(ctx, entry)
			if err != nil {
				s.log.Warn("cache eviction deferred", "video_id", entry.VideoID, "error", err)
				return err
			}
			if !pruned {
				// Publication or access changed this LRU snapshot. Recompute order
				// and the byte budget on the next pass rather than evict newer work.
				return nil
			}
			if entry.SizeBytes != nil {
				total -= *entry.SizeBytes
			}
			s.log.Info("playback artifact evicted", "video_id", entry.VideoID)
		}
		if len(entries) < 100 {
			break
		}
	}
	return nil
}

// pruneEntry rechecks publication and access while holding recording ownership.
// A stale LRU snapshot cannot authorize deleting a newer artifact.
func (s *Service) pruneEntry(ctx context.Context, entry repository.VideoPlaybackAsset) (bool, error) {
	unlock, err := s.store.Lock(ctx, entry.VideoID)
	if err != nil {
		return false, err
	}
	defer unlock.Close()
	fresh, err := s.repo.GetVideoPlaybackAsset(ctx, entry.VideoID)
	if errors.Is(err, repository.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if fresh.Status != repository.PlaybackAssetStatusReady ||
		!sameOptional(fresh.Filename, entry.Filename) ||
		!sameOptional(fresh.SizeBytes, entry.SizeBytes) ||
		!sameOptional(fresh.GeneratedAt, entry.GeneratedAt) ||
		!sameOptional(fresh.LastAccessedAt, entry.LastAccessedAt) {
		return false, nil
	}
	if err := s.storageDeletionReady(ctx); err != nil {
		return false, err
	}
	// The row must survive a failed object deletion so the next pass can retry it.
	if fresh.Filename != nil {
		if err := s.deleteArtifact(ctx, unlock, *fresh.Filename); err != nil {
			return false, err
		}
	}
	if err := s.repo.DeleteVideoPlaybackAsset(ctx, entry.VideoID); err != nil {
		// Stop at the oldest victim; skipping it would invert LRU order.
		s.log.Warn("delete playback artifact row during prune failed", "video_id", entry.VideoID, "error", err)
		return false, fmt.Errorf("delete playback artifact row %d: %w", entry.VideoID, err)
	}
	return true, nil
}

func sameOptional[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (s *Service) deleteArtifact(ctx context.Context, owned *mediastore.Recording, filename string) error {
	if err := s.storageDeletionReady(ctx); err != nil {
		return err
	}
	if err := owned.Delete(ctx, storagekeys.Video(filename)); err != nil {
		s.log.Warn("delete playback artifact failed", "filename", filename, "error", err)
		return err
	}
	// An outage during deletion leaves the row available for retry on the verified
	// volume.
	return s.storageDeletionReady(ctx)
}
