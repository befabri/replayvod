package streammeta

import (
	"context"
	"log/slog"
	"time"
)

// DefaultMetadataWatchInterval is the cadence MetadataWatcher falls
// back to when the caller passes 0. 60s is tight enough to catch
// the common "starting soon" → real-title transition that often
// happens in the first 2 minutes of a broadcast, and quick category
// flips during multi-game sessions. At 60 polls/hour per active
// recording the Helix-quota cost is negligible (default Helix is
// 800 req/min/app).
const DefaultMetadataWatchInterval = time.Minute

// MetadataWatcher runs alongside a recording, polling Helix on a
// ticker and persisting every distinct title AND category the
// broadcaster sets during the broadcast. Populates the
// titles+video_titles and categories+video_categories M2M pairs.
// In webhook mode the channel.update event handler writes the same
// rows via Hydrator.RecordChannelUpdate — poll and webhook share
// one write path so history shape is identical across modes.
//
// Intended lifecycle: one Watch call per recording, started when
// Stage 4 begins, canceled via ctx when the job ends. Safe to share
// across recordings — MetadataWatcher holds no per-run state.
type MetadataWatcher struct {
	hydrator *Hydrator
	log      *slog.Logger
	interval time.Duration
}

// MetadataWatchConfig carries the tunables. Zero-value-safe default
// (60s interval) so callers can ignore it when they don't need to
// override.
type MetadataWatchConfig struct {
	Interval time.Duration
}

// NewMetadataWatcher constructs the watcher. All persistence goes
// through `hydrator.RecordChannelUpdate` — the watcher itself holds
// no direct repo reference.
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

// WatchInitial carries the at-download-start metadata the watcher
// pre-links before the first tick so a recording shorter than one
// interval still carries correct history. Title and CategoryID come
// from the trigger path's Hydrate snapshot.
type WatchInitial struct {
	Title      string
	CategoryID string
}

// Watch polls for title AND category changes and links each new one
// to videoID. Blocks until ctx cancels — typically called in a
// goroutine by the downloader's run() alongside the snapshot ticker.
//
// initial values come from the download-start snapshot (the same
// values stored on videos.title + video_categories via the trigger
// path). When non-empty they're observed as the "last seen" so the
// first tick only records actual changes, not duplicate writes of
// what's already linked.
//
// Polling diffs against last-seen per field; a title or category
// that reverts to an earlier value relinks to the same dedup row,
// and ON CONFLICT DO NOTHING keeps only the first link — history
// sorts by first-appearance not exact timeline. Same semantic as
// the webhook path.
//
// ctx is used for:
//   - the ticker + early-exit check (so a canceled recording stops
//     polling promptly)
//   - the Helix fetch (inherits the cancel so a mid-shutdown tick
//     aborts the HTTP call instead of dangling)
//
// It is NOT used for the DB writes: RecordChannelUpdate wraps its
// own persist context so a tick landing right as the recording ends
// still commits.
func (w *MetadataWatcher) Watch(ctx context.Context, broadcasterID string, videoID int64, initial WatchInitial) {
	// Seed last-seen with the already-linked values so the first
	// tick only fires a RecordChannelUpdate call if something
	// actually changed on the broadcaster's side.
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
				// Helix failed or broadcaster went offline.
				// Offline is common for a recording nearing its
				// end (stream.offline + ENDLIST race); don't
				// spam warnings for it.
				continue
			}
			// Build a ChannelUpdateMeta with only the fields that
			// changed — RecordChannelUpdate is a no-op on empty
			// fields, so unchanged metadata doesn't re-link.
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
			if err := w.hydrator.RecordChannelUpdate(ctx, broadcasterID, meta); err != nil {
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
