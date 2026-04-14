package thumbnail

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// livePreviewURLTemplate is Twitch's stable CDN path for a live
// channel's auto-refreshing preview image. Refreshed every ~1-5
// minutes server-side; we fetch it as-is and cache-bust via a
// timestamp query param. Pattern has been stable on Twitch for
// years; when it does break the failure is a clean 404 the
// ticker swallows, not a recording crash.
const livePreviewURLTemplate = "https://static-cdn.jtvnw.net/previews-ttv/live_user_%s-%dx%d.jpg"

// DefaultSnapshotInterval matches the Helix /streams refresh
// cadence. Sampling faster than this gets the same image twice;
// slower wastes the "live time-lapse" feature for users who
// watch a 3-minute recording.
const DefaultSnapshotInterval = 5 * time.Minute

// SnapshotWriter abstracts the destination so the snapshotter
// doesn't bake in storage.Storage or filesystem paths. Callers
// that want to upload directly pass a closure that streams body
// bytes into their storage backend; tests pass an in-memory
// sink.
type SnapshotWriter interface {
	WriteSnapshot(ctx context.Context, index int, body io.Reader) error
}

// SnapshotterConfig parameterizes one Snapshotter. Zero values
// produce sensible defaults (5 min interval, 1280×720 frame,
// 15s per-request timeout).
type SnapshotterConfig struct {
	// HTTPClient is the client used for the CDN fetch. Nil uses
	// a client with a 15s timeout — enough for a ~100 KB JPEG
	// over any reasonable link.
	HTTPClient *http.Client

	// Interval between snapshot fetches. Default 5 min.
	Interval time.Duration

	// Width + Height substituted into the preview URL's
	// `{width}x{height}` placeholder. Default 1280×720 — matches
	// what the Twitch directory page uses.
	Width  int
	Height int

	// Log routes snapshot-level debug/warn events. Nil discards.
	Log *slog.Logger
}

// Snapshotter fetches Twitch's auto-refreshing live preview image
// on a ticker and writes each capture through a SnapshotWriter.
// Intended lifecycle: one goroutine per active recording, started
// when Stage 4 (fetch loop) begins, canceled via ctx when the
// downloader run() goroutine exits.
type Snapshotter struct {
	client   *http.Client
	interval time.Duration
	width    int
	height   int
	log      *slog.Logger
}

// NewSnapshotter constructs a Snapshotter with sensible defaults.
// Safe for concurrent use across multiple recordings.
func NewSnapshotter(cfg SnapshotterConfig) *Snapshotter {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = DefaultSnapshotInterval
	}
	w := cfg.Width
	if w <= 0 {
		w = 1280
	}
	h := cfg.Height
	if h <= 0 {
		h = 720
	}
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Snapshotter{
		client:   client,
		interval: interval,
		width:    w,
		height:   h,
		log:      log.With("domain", "thumbnail.snapshot"),
	}
}

// Run blocks until ctx is canceled, fetching one snapshot
// immediately and then one per interval. Transient fetch failures
// (offline window, CDN 404 between stream ends and scheduler
// cleanup, network blip) are logged and skipped — the caller's
// recording continues either way.
//
// Returns the count of successfully written snapshots on exit
// (ctx.Err is always non-nil at that point — Run only exits via
// cancel). Useful for downstream accounting / DB updates without
// needing an external counter.
func (s *Snapshotter) Run(ctx context.Context, login string, w SnapshotWriter) int {
	url := fmt.Sprintf(livePreviewURLTemplate, strings.ToLower(login), s.width, s.height)

	count := 0
	tryFetch := func() {
		if err := s.fetchOne(ctx, url, count, w); err != nil {
			// ctx cancel isn't a fetch failure — it's the
			// shutdown signal we're about to observe at the
			// top of the loop. Only log real failures.
			if ctx.Err() == nil {
				s.log.Warn("snapshot fetch failed", "index", count, "error", err)
			}
			return
		}
		count++
	}

	// First snapshot fires immediately so a 3-minute recording
	// has something to show before its first tick.
	tryFetch()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return count
		case <-ticker.C:
			tryFetch()
		}
	}
}

// fetchOne performs one HTTP GET against the preview URL and
// streams the body into w. cache-bust query param is added per
// call so the CDN doesn't return a stale image from an
// intermediate proxy.
func (s *Snapshotter) fetchOne(ctx context.Context, url string, index int, w SnapshotWriter) error {
	// Cache-bust with the current unix-second. Twitch itself
	// refreshes at ~minute granularity, so second-level buster
	// is overkill but cheap and reliable.
	full := url + "?_t=" + strconv.FormatInt(time.Now().Unix(), 10)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("get: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}

	if err := w.WriteSnapshot(ctx, index, resp.Body); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	s.log.Debug("snapshot saved", "index", index, "size_header", resp.Header.Get("Content-Length"))
	return nil
}
