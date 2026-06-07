package playbackcache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/remux"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

const (
	// defaultBuildTimeout is solely the per-build ffmpeg ceiling. It is NOT the
	// crash-orphan recovery window: an orphaned 'building' row (a play-triggered
	// build the process dropped on shutdown) is reclaimed the next time the
	// recording is played, since BuildNow rebuilds any non-ready row.
	defaultBuildTimeout = 6 * time.Hour
	// buildOvershootMarginDivisor reserves expectedBytes/this (~1.5%) of headroom
	// in the build-admission gate to cover the muxed artifact running slightly
	// larger than the summed part bytes (container framing / +faststart). It
	// keeps a near-cap recording from building and then tripping the terminal
	// post-build oversize check; it defers (retryable) instead.
	buildOvershootMarginDivisor = 64
	// defaultBuildConcurrency caps how many artifact builds run at once. ffmpeg
	// copy-concat reads and rewrites a whole recording, so on the low-resource
	// box this targets, one at a time avoids I/O thrash; extras queue. Without
	// this, several streams played at once would spawn concurrent concats.
	defaultBuildConcurrency = 1
	// diskReserveFraction keeps this fraction of total disk free even when the
	// percentage budget would allow more, so the cache can never be the thing
	// that pushes a local filesystem to ENOSPC. 1/20 == 5%.
	diskReserveFraction = 20
)

// Runner stream-copies the concat list at listPath into outputPath. The
// production implementation drives remux.Remuxer (atomic .part rename, stderr
// capture, +faststart); tests substitute a stub.
type Runner interface {
	Concat(ctx context.Context, listPath, outputPath string) error
}

type Service struct {
	repo    repository.Repository
	store   storage.Storage
	scratch string
	runner  Runner
	log     *slog.Logger

	buildTimeout time.Duration

	// fsStat reports total and available bytes for a local storage root.
	// Overridable so capacity math is testable without the host filesystem.
	fsStat func(root string) (total, avail int64, err error)
	// capacityOverride, when non-nil, replaces the storage-derived budget.
	// Tests use it to exercise eviction deterministically.
	capacityOverride func(current int64) (int64, bool)

	// buildCtx is the parent of every build's context; Close cancels it so a
	// graceful shutdown kills the in-flight ffmpeg child (SIGKILL via
	// CommandContext) instead of orphaning the process and its temp file. An
	// interrupted build is rebuilt the next time the recording is played.
	buildCtx     context.Context
	cancelBuilds context.CancelFunc
	wg           sync.WaitGroup
	// sem bounds concurrent builds (buffered-channel semaphore, the same idiom
	// as recordingwebhook.Dispatcher and eventsub). Acquired in runBuild.
	sem chan struct{}

	mu       sync.Mutex
	closed   bool
	building map[int64]struct{}
}

func New(repo repository.Repository, store storage.Storage, scratchDir, ffmpegPath string, log *slog.Logger) *Service {
	if scratchDir == "" {
		scratchDir = filepath.Join(os.TempDir(), "replayvod-playback-cache")
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	log = log.With("domain", "playback-cache")
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		repo:         repo,
		store:        store,
		scratch:      scratchDir,
		runner:       remuxRunner{remuxer: &remux.Remuxer{FFmpegPath: ffmpegPath, Log: log}},
		log:          log,
		buildTimeout: defaultBuildTimeout,
		fsStat:       statfsBytes,
		buildCtx:     ctx,
		cancelBuilds: cancel,
		sem:          make(chan struct{}, defaultBuildConcurrency),
		building:     make(map[int64]struct{}),
	}
}

func (s *Service) SetRunner(r Runner) {
	if r != nil {
		s.runner = r
	}
}

// StartBuild kicks off a background build for videoID. It is called lazily the
// first time a recording is played (see StreamHandler.streamPart); an
// interrupted build is rebuilt on the next play. The caller's ctx is
// intentionally not used for the build's lifetime — the triggering HTTP
// request's context cancels the instant its range read finishes, and shutdown
// cancellation flows through buildCtx instead.
func (s *Service) StartBuild(_ context.Context, videoID int64) {
	if s == nil || s.repo == nil || s.store == nil || s.runner == nil {
		return
	}
	// Hold the lock across the closed-check and wg.Go so a build can't be added
	// to the WaitGroup after Close marked the service closed: Close sets closed
	// under the same lock before it Waits, so wg.Go (Add) can never race a Wait
	// that has already seen the counter reach zero.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.wg.Go(func() { s.runBuild(videoID) })
}

func (s *Service) runBuild(videoID int64) {
	if !s.reserve(videoID) {
		return
	}
	defer s.release(videoID)
	// Acquire a build slot (bounds concurrent ffmpeg concats); queued builds park
	// here until a slot frees or shutdown cancels buildCtx.
	select {
	case s.sem <- struct{}{}:
	case <-s.buildCtx.Done():
		return
	}
	defer func() { <-s.sem }()

	ctx := s.buildCtx
	if s.buildTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.buildTimeout)
		defer cancel()
	}
	if err := s.BuildNow(ctx, videoID); err != nil {
		s.log.Warn("playback artifact build failed", "video_id", videoID, "error", err)
	}
}

// Wait blocks until in-flight background builds finish, without canceling them.
// Close is the shutdown path; Wait is mainly for tests that need to observe a
// StartBuild result.
func (s *Service) Wait() {
	if s == nil {
		return
	}
	s.wg.Wait()
}

// Close cancels in-flight builds and waits (bounded) for them to unwind. Wiring
// it into shutdown keeps a graceful stop from orphaning an ffmpeg child.
func (s *Service) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	if s.cancelBuilds != nil {
		s.cancelBuilds()
	}
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		s.log.Warn("playback cache shutdown: 30s timeout reached; a build is still unwinding")
	}
}

// Reconcile keeps the cache within its size budget. Builds are NOT started
// here: artifacts are produced lazily the first time a recording is watched
// (see StreamHandler.streamPart), so the server never bulk-concatenates the
// many VODs nobody opens. A build dropped on shutdown or one that failed
// transiently simply rebuilds the next time that recording is played. Reconcile
// only prunes — so a lowered cap reclaims space — and is a no-op when the cache
// is disabled. Run on startup and on a schedule.
func (s *Service) Reconcile(ctx context.Context) error {
	if s == nil || s.repo == nil {
		return nil
	}
	settings, err := playbackSettings(ctx, s.repo)
	if err != nil {
		return err
	}
	if !settings.active() {
		// Off: don't prune (don't wipe the existing cache) and don't build.
		return nil
	}
	if err := s.pruneWithSettings(ctx, settings); err != nil {
		s.log.Warn("reconcile prune failed", "error", err)
	}
	return nil
}

func (s *Service) BuildNow(ctx context.Context, videoID int64) error {
	settings, err := playbackSettings(ctx, s.repo)
	if err != nil {
		return err
	}
	// maxPercent<=0 (or the feature switched off) means "cache disabled": never
	// build and, crucially, never treat it as a zero-byte budget that would
	// wipe the existing cache. autoGenerate gates only the automatic path.
	if !settings.active() || !settings.autoGenerate {
		return nil
	}

	video, err := s.repo.GetVideo(ctx, videoID)
	if err != nil {
		return err
	}
	if video.Status != repository.VideoStatusDone || video.DeletedAt != nil {
		return nil
	}

	// Idempotency: a ready artifact whose file is still present needs no work.
	// Skips needless rebuilds (and the transient unavailability they'd cause).
	asset, assetErr := s.repo.GetVideoPlaybackAsset(ctx, videoID)
	switch {
	case assetErr == nil && asset.Status == repository.PlaybackAssetStatusReady && asset.Filename != nil:
		exists, existsErr := s.store.Exists(ctx, storagekeys.Video(*asset.Filename))
		if existsErr != nil {
			// Don't treat "couldn't check" as "missing" and rebuild a healthy
			// artifact (re-running ffmpeg + flickering the watch page to building).
			// Assume present; a genuinely-gone file self-heals via streamPlayback's
			// stale-row demotion + the reconciler.
			s.log.Warn("probe existing playback artifact failed; skipping rebuild",
				"video_id", videoID, "error", existsErr)
			return nil
		}
		if exists {
			return nil
		}
		// Ready row but the file is genuinely gone: fall through and rebuild.
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

	// A part row whose file is gone (partial cleanup, corruption) can never be
	// concatenated; mark it permanently unavailable rather than letting the
	// reconciler relaunch a doomed ffmpeg run forever. Checked before the
	// budget defer so a missing part isn't retried just because the disk is full.
	if missing, err := s.firstMissingPart(ctx, ordered); err != nil {
		return err
	} else if missing != "" {
		return s.markUnavailable(ctx, videoID, fmt.Sprintf("source part %q is missing", missing))
	}

	// Doesn't fit right now: leave NO verdict and retry on the next play.
	// buildHeadroom = min(configured, free - reserve) folds both reasons — "no
	// free-space headroom now" (transient, retries when the disk frees) and
	// "bigger than the configured cap" (retries on a raised max_percent). Neither
	// is permanent, so we must not mark it unavailable (that would mean the next
	// view never retries and silently kill the feature). The overshoot margin
	// keeps a recording whose estimate sits just under the cap from building and
	// then tripping the terminal post-build size check — it defers instead.
	requiredBytes := expectedBytes + expectedBytes/buildOvershootMarginDivisor
	if budget.known && requiredBytes > budget.buildHeadroom {
		s.log.Debug("deferring playback build: estimate exceeds build headroom",
			"video_id", videoID, "estimate_bytes", expectedBytes,
			"required_bytes", requiredBytes, "build_headroom", budget.buildHeadroom,
			"configured_cap", budget.configured)
		return nil
	}

	filename := playbackFilename(video, ordered[0])
	if _, err := s.repo.UpsertVideoPlaybackAsset(ctx, &repository.VideoPlaybackAssetInput{
		VideoID: videoID,
		Status:  repository.PlaybackAssetStatusBuilding,
	}); err != nil {
		return err
	}

	size, err := s.buildArtifact(ctx, filename, ordered)
	if err != nil {
		// Build context may already be canceled/timed out; clean up any partial
		// artifact and record the outcome on a detached context so it sticks.
		detached := context.WithoutCancel(ctx)
		s.deleteArtifact(detached, filename)
		if errors.Is(err, context.Canceled) {
			// Interrupted by a graceful shutdown — not a real failure. Drop the
			// building row so the next play rebuilds it (via the no-row path)
			// instead of leaving a stale 'failed' row, and so a clean stop isn't
			// surfaced as a failed artifact.
			if delErr := s.repo.DeleteVideoPlaybackAsset(detached, videoID); delErr != nil {
				s.log.Warn("clear interrupted playback build row failed", "video_id", videoID, "error", delErr)
			}
			return err
		}
		_, _ = s.repo.UpsertVideoPlaybackAsset(detached, &repository.VideoPlaybackAssetInput{
			VideoID: videoID,
			Status:  repository.PlaybackAssetStatusFailed,
			Error:   nonEmpty(errorPreview(err)),
		})
		return err
	}

	// All terminal verdicts and the ready commit run on a detached context so a
	// build that finishes just as buildTimeout fires still records its outcome.
	detached := context.WithoutCancel(ctx)

	// Freshness re-check FIRST, before any terminal verdict. Retention may have
	// soft-deleted and purged this video while ffmpeg ran (the guard at the top
	// of BuildNow is stale by now). If it's gone, drop the artifact + row and
	// return — recording an unavailable/ready row for a soft-deleted video leaks
	// it (retention does not revisit deleted_at IS NOT NULL).
	fresh, err := s.repo.GetVideo(detached, videoID)
	switch {
	case err != nil && !errors.Is(err, repository.ErrNotFound):
		// Transient read error: fail safe. Don't commit on an unverified video;
		// leave the 'building' row so the next play retries.
		s.deleteArtifact(detached, filename)
		return fmt.Errorf("re-check video before commit: %w", err)
	case errors.Is(err, repository.ErrNotFound) || fresh.Status != repository.VideoStatusDone || fresh.DeletedAt != nil:
		s.deleteArtifact(detached, filename)
		if delErr := s.repo.DeleteVideoPlaybackAsset(detached, videoID); delErr != nil {
			s.log.Warn("clear playback row for deleted video failed", "video_id", videoID, "error", delErr)
		}
		return nil
	}

	// A single artifact larger than the configured cap can never coexist with
	// the cache; keeping it would make Prune evict every other entry and still
	// overflow. Drop it and mark it terminally unavailable.
	//
	// Terminal, NOT 'failed': the pre-build overshoot-margined defer is the
	// retryable cap gate that backfills on a raised max_percent. Reaching HERE
	// means a real overshoot beyond the margin that ffmpeg reproduces
	// deterministically, so a retryable verdict would relaunch it forever.
	if budget.known && size > budget.configured {
		s.deleteArtifact(detached, filename)
		return s.markUnavailable(detached, videoID,
			fmt.Sprintf("playback artifact %d exceeds cache cap %d", size, budget.configured))
	}

	at := time.Now().UTC()
	mime := mimeTypeForExtension(partExtension(ordered[0]))
	duration := totalDuration(ordered)
	if _, err := s.repo.UpsertVideoPlaybackAsset(detached, &repository.VideoPlaybackAssetInput{
		VideoID:         videoID,
		Status:          repository.PlaybackAssetStatusReady,
		Filename:        &filename,
		MimeType:        &mime,
		DurationSeconds: &duration,
		SizeBytes:       &size,
		GeneratedAt:     &at,
		LastAccessedAt:  &at,
	}); err != nil {
		s.deleteArtifact(detached, filename)
		return err
	}

	if err := s.pruneWithSettings(detached, settings); err != nil {
		s.log.Warn("playback cache prune failed", "error", err)
	}
	return nil
}

// firstMissingPart returns the filename of the first part whose stored object is
// absent, or "" when all parts are present.
func (s *Service) firstMissingPart(ctx context.Context, parts []repository.VideoPart) (string, error) {
	for _, part := range parts {
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
	_, err := s.repo.UpsertVideoPlaybackAsset(ctx, &repository.VideoPlaybackAssetInput{
		VideoID: videoID,
		Status:  repository.PlaybackAssetStatusUnavailable,
		Error:   nonEmpty(reason),
	})
	return err
}

// Prune evicts ready artifacts in LRU order until the cache fits its budget.
// When the cache is disabled (or the budget is unknown, e.g. an object store
// without a derivable cap) it leaves everything in place — disabling must not
// wipe the cache.
func (s *Service) Prune(ctx context.Context) error {
	settings, err := playbackSettings(ctx, s.repo)
	if err != nil {
		return err
	}
	return s.pruneWithSettings(ctx, settings)
}

// pruneWithSettings is Prune with the settings already loaded, so callers that
// just read them (BuildNow, Reconcile) don't re-fetch GetServerSettings.
func (s *Service) pruneWithSettings(ctx context.Context, settings playbackConfig) error {
	if !settings.active() {
		return nil
	}
	entries, err := s.repo.ListReadyVideoPlaybackAssets(ctx)
	if err != nil {
		return err
	}
	var total int64
	for _, entry := range entries {
		if entry.SizeBytes != nil {
			total += *entry.SizeBytes
		}
	}
	budget, err := s.capacity(ctx, settings.maxPercent, total)
	if err != nil {
		return err
	}
	if !budget.known {
		return nil
	}
	// Evict down to the free-space-clamped budget so the cache yields to
	// recordings under disk pressure (current collapses toward 0 when the disk
	// fills). The BuildNow defer keeps this from churning: wiped artifacts are
	// only rebuilt once there's genuinely room again.
	capBytes := budget.current
	for _, entry := range entries {
		if total <= capBytes {
			break
		}
		// Delete the row first: if the storage delete then fails we have at
		// worst an orphaned file, never a ready row pointing at a gone file that
		// streamPlayback would 404 on. The orphaned file is NOT reclaimed by a
		// later prune (prune only walks rows, and this one's row is gone) — it's
		// cleaned up by retention's deterministic-key delete when the recording
		// ages out. Until then currentCacheBytes undercounts real disk use.
		if err := s.repo.DeleteVideoPlaybackAsset(ctx, entry.VideoID); err != nil {
			// Stop, don't skip: entries are oldest-first, so continuing would evict
			// a NEWER artifact to compensate for this older one we couldn't delete —
			// inverting LRU. Leave the cache briefly over-cap; the next prune retries
			// this same victim.
			s.log.Warn("delete playback artifact row during prune failed", "video_id", entry.VideoID, "error", err)
			break
		}
		if entry.Filename != nil {
			if err := s.store.Delete(ctx, storagekeys.Video(*entry.Filename)); err != nil {
				s.log.Warn("delete playback artifact during prune failed", "video_id", entry.VideoID, "filename", *entry.Filename, "error", err)
			}
		}
		if entry.SizeBytes != nil {
			total -= *entry.SizeBytes
		}
		s.log.Info("playback artifact evicted", "video_id", entry.VideoID)
	}
	return nil
}

func (s *Service) currentCacheBytes(ctx context.Context) int64 {
	entries, err := s.repo.ListReadyVideoPlaybackAssets(ctx)
	if err != nil {
		s.log.Warn("sum playback cache bytes failed", "error", err)
		return 0
	}
	var total int64
	for _, entry := range entries {
		if entry.SizeBytes != nil {
			total += *entry.SizeBytes
		}
	}
	return total
}

// cacheBudget separates three limits so a transient full disk is never mistaken
// for a permanent "too big" verdict, and a build is never admitted that can't
// physically fit in current free space.
type cacheBudget struct {
	// configured is maxPercent% of the reference size (total disk for local,
	// recorded-library bytes for object storage). It is the stable cap an
	// artifact must fit under; exceeding it is permanent (until maxPercent
	// rises). For maxPercent > 0 it is always > 0.
	configured int64
	// current is configured clamped by (current cache bytes + free - reserve):
	// the cache's allowed total size, which Prune evicts down to. It re-adds the
	// existing cache bytes because Prune CAN reclaim them.
	current int64
	// buildHeadroom is configured clamped by (free - reserve) only — what a NEW
	// artifact must fit under right now, since the existing cache is not freed
	// before the build writes (the prune runs after). Using current here would
	// admit a build that overflows real free space and ENOSPCs the disk.
	// Equals configured on object storage (no local disk to pressure).
	buildHeadroom int64
	known         bool
}

// capacity computes the cache budget.
//
//   - Local storage: configured is maxPercent% of the filesystem; current
//     clamps that by free space so the cache never drives the disk below
//     diskReserveFraction (the ENOSPC guard the raw "% of total disk" math
//     lacked). Under disk pressure current collapses toward 0 while configured
//     stays put — that split is what lets BuildNow defer instead of permanently
//     failing, and lets the cache yield to recordings without being declared
//     fundamentally too small.
//   - Object storage: no local disk to pressure, so current == configured ==
//     maxPercent% of the recorded-library size. Bounded and enforced rather
//     than silently unbounded.
func (s *Service) capacity(ctx context.Context, maxPercent int, currentCacheBytes int64) (cacheBudget, error) {
	if s.capacityOverride != nil {
		b, known := s.capacityOverride(currentCacheBytes)
		return cacheBudget{configured: b, current: b, buildHeadroom: b, known: known}, nil
	}
	if local, ok := s.store.(*storage.LocalStorage); ok {
		total, avail, err := s.fsStat(local.Root)
		if err != nil {
			return cacheBudget{}, fmt.Errorf("stat storage filesystem: %w", err)
		}
		// Divide before multiply: total is a whole-filesystem byte count and
		// total*maxPercent could overflow int64 on a very large array.
		configured := max(total/100*int64(maxPercent), 0)
		reserve := total / diskReserveFraction
		return cacheBudget{
			configured: configured,
			// Prune target: the cache may grow to its own bytes + free - reserve.
			current: max(min(configured, currentCacheBytes+avail-reserve), 0),
			// Build admission: a new artifact must fit in free space alone (the
			// existing cache isn't reclaimed until the post-build prune).
			buildHeadroom: max(min(configured, avail-reserve), 0),
			known:         true,
		}, nil
	}
	totals, err := s.repo.VideoStatsTotals(ctx, "")
	if err != nil {
		return cacheBudget{}, fmt.Errorf("read library totals for playback cache cap: %w", err)
	}
	configured := max(totals.TotalSize/100*int64(maxPercent), 0)
	return cacheBudget{configured: configured, current: configured, buildHeadroom: configured, known: true}, nil
}

func statfsBytes(root string) (int64, int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(root, &stat); err != nil {
		return 0, 0, err
	}
	return int64(stat.Blocks) * int64(stat.Bsize), int64(stat.Bavail) * int64(stat.Bsize), nil
}

func (s *Service) buildArtifact(ctx context.Context, filename string, parts []repository.VideoPart) (int64, error) {
	if err := os.MkdirAll(s.scratch, 0o755); err != nil {
		return 0, fmt.Errorf("create playback scratch dir: %w", err)
	}
	workDir, err := os.MkdirTemp(s.scratch, "build-*")
	if err != nil {
		return 0, fmt.Errorf("create playback build dir: %w", err)
	}
	defer os.RemoveAll(workDir) //nolint:errcheck

	localParts, err := s.localPartPaths(ctx, workDir, parts)
	if err != nil {
		return 0, err
	}
	listPath := filepath.Join(workDir, "parts.txt")
	if err := remux.WriteConcatListFile(listPath, localParts); err != nil {
		return 0, err
	}

	if local, ok := s.store.(*storage.LocalStorage); ok {
		finalPath, err := local.LocalPath(storagekeys.Video(filename))
		if err != nil {
			return 0, err
		}
		if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
			return 0, fmt.Errorf("create playback artifact parent: %w", err)
		}
		// runner.Concat commits atomically (remux writes <final>.part then
		// renames), so no temp dance is needed here.
		if err := s.runner.Concat(ctx, listPath, finalPath); err != nil {
			return 0, err
		}
		info, err := os.Stat(finalPath)
		if err != nil {
			return 0, fmt.Errorf("stat playback artifact: %w", err)
		}
		return info.Size(), nil
	}

	outputPath := filepath.Join(workDir, "playback"+partExtension(parts[0]))
	if err := s.runner.Concat(ctx, listPath, outputPath); err != nil {
		return 0, err
	}
	info, err := os.Stat(outputPath)
	if err != nil {
		return 0, fmt.Errorf("stat playback artifact: %w", err)
	}
	f, err := os.Open(outputPath)
	if err != nil {
		return 0, fmt.Errorf("open playback artifact: %w", err)
	}
	defer f.Close()
	if err := s.store.Save(ctx, storagekeys.Video(filename), f); err != nil {
		return 0, fmt.Errorf("save playback artifact: %w", err)
	}
	return info.Size(), nil
}

func (s *Service) deleteArtifact(ctx context.Context, filename string) {
	if err := s.store.Delete(ctx, storagekeys.Video(filename)); err != nil {
		s.log.Warn("delete playback artifact failed", "filename", filename, "error", err)
	}
}

func (s *Service) localPartPaths(ctx context.Context, workDir string, parts []repository.VideoPart) ([]string, error) {
	out := make([]string, 0, len(parts))
	if local, ok := s.store.(*storage.LocalStorage); ok {
		for _, part := range parts {
			path, err := local.LocalPath(storagekeys.Video(part.Filename))
			if err != nil {
				return nil, err
			}
			out = append(out, path)
		}
		return out, nil
	}
	for i, part := range parts {
		localPath := filepath.Join(workDir, fmt.Sprintf("part%03d%s", i+1, partExtension(part)))
		if err := copyStorageObject(ctx, s.store, storagekeys.Video(part.Filename), localPath); err != nil {
			return nil, err
		}
		out = append(out, localPath)
	}
	return out, nil
}

func copyStorageObject(ctx context.Context, store storage.Storage, srcPath, dstPath string) error {
	src, err := store.Open(ctx, srcPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", srcPath, err)
	}
	defer src.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("create local part copy: %w", err)
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil {
		return fmt.Errorf("copy %s: %w", srcPath, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close local part copy: %w", closeErr)
	}
	return nil
}

func (s *Service) reserve(videoID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.building[videoID]; ok {
		return false
	}
	s.building[videoID] = struct{}{}
	return true
}

func (s *Service) release(videoID int64) {
	s.mu.Lock()
	delete(s.building, videoID)
	s.mu.Unlock()
}

type playbackConfig struct {
	enabled      bool
	autoGenerate bool
	maxPercent   int
}

// active reports whether the cache should build and prune. A disabled feature
// or a non-positive percentage is "off": no builds, and Prune leaves the
// existing cache untouched rather than wiping it.
func (c playbackConfig) active() bool {
	return c.enabled && c.maxPercent > 0
}

func playbackSettings(ctx context.Context, repo repository.Repository) (playbackConfig, error) {
	settings, err := repo.GetServerSettings(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return playbackConfig{}, nil
		}
		return playbackConfig{}, err
	}
	cfg := playbackConfig{
		enabled:      settings.PlaybackCacheEnabled,
		autoGenerate: settings.PlaybackCacheAutoGenerate,
		maxPercent:   settings.PlaybackCacheMaxPercent,
	}
	if cfg.maxPercent < 0 {
		cfg.maxPercent = 0
	}
	if cfg.maxPercent > 100 {
		cfg.maxPercent = 100
	}
	return cfg, nil
}

func orderedParts(parts []repository.VideoPart) []repository.VideoPart {
	out := append([]repository.VideoPart(nil), parts...)
	slices.SortFunc(out, func(a, b repository.VideoPart) int {
		return int(a.PartIndex) - int(b.PartIndex)
	})
	return out
}

func canCopyConcat(parts []repository.VideoPart) (bool, string) {
	if len(parts) < 2 {
		return false, "recording has fewer than two parts"
	}
	first := parts[0]
	ext := partExtension(first)
	if ext != ".mp4" && ext != ".m4a" {
		return false, "only MP4/M4A parts can be copy-concatenated"
	}
	for i, part := range parts {
		if part.PartIndex != int32(i+1) {
			return false, "part indexes are not contiguous"
		}
		if part.SizeBytes <= 0 {
			return false, "one or more parts have no stored bytes"
		}
		if partExtension(part) != ext {
			return false, "parts use different container extensions"
		}
		if part.Quality != first.Quality || part.Codec != first.Codec || part.SegmentFormat != first.SegmentFormat || !fpsEqual(part.FPS, first.FPS) {
			return false, "parts do not share the same recorded rendition metadata"
		}
	}
	return true, ""
}

func fpsEqual(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	diff := *a - *b
	if diff < 0 {
		diff = -diff
	}
	return diff < 0.001
}

func playbackFilename(video *repository.Video, first repository.VideoPart) string {
	return storagekeys.PlaybackName(video.Filename, first.Filename)
}

func partExtension(part repository.VideoPart) string {
	return strings.ToLower(filepath.Ext(part.Filename))
}

func totalDuration(parts []repository.VideoPart) float64 {
	var total float64
	for _, part := range parts {
		total += part.DurationSeconds
	}
	return total
}

func expectedSize(parts []repository.VideoPart) int64 {
	var total int64
	for _, part := range parts {
		total += part.SizeBytes
	}
	return total
}

func mimeTypeForExtension(ext string) string {
	if ext == ".m4a" {
		return "audio/mp4"
	}
	return "video/mp4"
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

// remuxRunner adapts remux.Remuxer to the Runner interface so the playback
// concat shares one ffmpeg invocation, escaping, and atomic-rename path with
// the recording pipeline.
type remuxRunner struct {
	remuxer *remux.Remuxer
}

func (r remuxRunner) Concat(ctx context.Context, listPath, outputPath string) error {
	kind := remux.KindVideo
	if strings.EqualFold(filepath.Ext(outputPath), ".m4a") {
		kind = remux.KindAudio
	}
	return r.remuxer.Run(ctx, remux.RunInput{
		Mode:           remux.ModeTS,
		Kind:           kind,
		Faststart:      true,
		InputPath:      listPath,
		OutputDir:      filepath.Dir(outputPath),
		OutputBasename: strings.TrimSuffix(filepath.Base(outputPath), filepath.Ext(outputPath)),
	})
}
