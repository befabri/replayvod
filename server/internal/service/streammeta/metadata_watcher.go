package streammeta

import (
	"context"
	"github.com/befabri/replayvod/server/internal/repository"
	"log/slog"
	"time"
)

// DefaultMetadataWatchInterval is the polling interval when configuration is nonpositive.
const DefaultMetadataWatchInterval = time.Minute

// MetadataWatcher records live title and category changes for one execution per Watch call.
// It can be shared across recordings.
type MetadataWatcher struct {
	hydrator *Hydrator
	log      *slog.Logger
	interval time.Duration
}

// MetadataWatchConfig controls polling; a nonpositive interval uses DefaultMetadataWatchInterval.
type MetadataWatchConfig struct {
	Interval time.Duration
}

// NewMetadataWatcher creates a poller that persists changes through hydrator.
func NewMetadataWatcher(hydrator *Hydrator, cfg MetadataWatchConfig, log *slog.Logger) *MetadataWatcher {
	interval := cfg.Interval
	if interval <= 0 {
		interval = DefaultMetadataWatchInterval
	}
	return &MetadataWatcher{
		hydrator: hydrator,
		log:      log.With("domain", "streammeta.watch"),
		interval: interval,
	}
}

// WatchInitial contains already-linked opening metadata and the execution that may update it.
type WatchInitial struct {
	Claim       repository.AttemptClaim
	Title       string
	CategoryID  string
	MediaOffset MediaOffsetProvider
}

func (initial WatchInitial) currentMediaOffset() *float64 {
	if initial.MediaOffset == nil {
		return nil
	}
	seconds, ok := initial.MediaOffset.MediaOffsetSeconds()
	if !ok {
		return nil
	}
	return cleanMediaOffset(&seconds)
}

// Watch records changed metadata until ctx is cancelled.
// Initial values suppress duplicate observations; execution ownership rejects late writes.
func (w *MetadataWatcher) Watch(ctx context.Context, broadcasterID string, videoID int64, initial WatchInitial) {
	lastTitle := initial.Title
	lastCategory := initial.CategoryID

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap := w.hydrator.Hydrate(ctx, broadcasterID)
			if snap == nil {
				// Offline responses are expected while HLS drains the broadcast's final segments.
				continue
			}
			var meta ChannelUpdateMeta
			if snap.Title != "" && snap.Title != lastTitle {
				meta.Title = snap.Title
			}
			if snap.GameID != "" && snap.GameID != lastCategory {
				meta.CategoryID = snap.GameID
				meta.CategoryName = snap.GameName
			}
			if meta.Title == "" && meta.CategoryID == "" {
				continue
			}
			meta.MediaOffsetSeconds = initial.currentMediaOffset()
			if err := w.hydrator.recordVideoMetadata(ctx, initial.Claim, false, meta, w.hydrator.now().UTC()); err != nil {
				w.log.Warn("record channel update",
					"video_id", videoID, "broadcaster_id", broadcasterID, "error", err)
				continue
			}
			if meta.Title != "" {
				lastTitle = meta.Title
			}
			if meta.CategoryID != "" {
				lastCategory = meta.CategoryID
			}
		}
	}
}
