// Package storagescan tombstones terminal recordings whose media is missing.
// Attached storage must confirm absence; missing tombstones keep objects and
// metadata for restoration, while manual and retention deletion own cleanup.
package storagescan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sync"
	"time"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/eventlog"
	"github.com/befabri/replayvod/server/internal/mediastore"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

const (
	scanPageSize   = 64
	scanWorkers    = 8
	inspectTimeout = 10 * time.Second
	cursorTimeout  = 5 * time.Second

	EventDomain            = "storage"
	EventRecordingMissing  = "recording_missing"
	EventRecordingRestored = "recording_restored"
	EventScanReconciled    = "scan_reconciled"
)

// ErrNotRestorable means a recording is live, permanently removed, queued for
// deletion, or a failed missing-media tombstone with no part references.
var ErrNotRestorable = errors.New("storage scan: recording is not a restorable tombstone")

// ErrStillMissing is matched by every StillMissingError.
var ErrStillMissing = errors.New("storage scan: media is still missing")

// ErrArchivedAgain means another open recording already owns this Twitch VOD;
// remove one copy before restoring the other.
var ErrArchivedAgain = errors.New("storage scan: this VOD was archived again; remove one of the two copies first")

// StillMissingError reports how much of a tombstone's media is still absent.
type StillMissingError struct {
	Missing, Total int
}

func (e *StillMissingError) Error() string {
	return fmt.Sprintf("%d of %d parts are still missing", e.Missing, e.Total)
}

func (e *StillMissingError) Is(target error) bool { return target == ErrStillMissing }

// Service reconciles missing and returned media on verified storage.
type Service struct {
	repo   repository.Repository
	store  *mediastore.Store
	bus    *eventbus.Buses
	log    *slog.Logger
	sweep  chan struct{}
	probes chan struct{}
}

// Option configures a Service before use.
type Option func(*Service)

// WithEventBus publishes committed scan and restore changes to subscribers.
func WithEventBus(bus *eventbus.Buses) Option {
	return func(s *Service) { s.bus = bus }
}

// New creates a scan service using shared media ownership and storage verification.
func New(repo repository.Repository, store *mediastore.Store, log *slog.Logger, opts ...Option) *Service {
	s := &Service{
		repo: repo, store: store,
		log:    log.With("domain", "storagescan"),
		sweep:  make(chan struct{}, 1),
		probes: make(chan struct{}, scanWorkers),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Report counts inspected recordings and committed reconciliation changes.
type Report struct {
	Scanned, Missing, Partial, Tombstoned, Restored int
	// Complete is false when the run stopped with work left; the next run
	// resumes from the persisted cursor.
	Complete bool
}

// Sweep resumes scanning and restoration from durable page cursors.
// A deadline after inspection makes progress returns a partial report without a
// deadline error; explicit cancellation is always returned.
func (s *Service) Sweep(ctx context.Context) (report Report, sweepErr error) {
	select {
	case s.sweep <- struct{}{}:
		defer func() { <-s.sweep }()
	case <-ctx.Done():
		return Report{}, ctx.Err()
	}
	defer func() { s.summarize(ctx, report, sweepErr) }()
	if err := s.verify(ctx); err != nil {
		return Report{}, err
	}
	after, restoreAfter := s.loadCursors(ctx)
	var errs []error
	// Resume restoration directly so repeated scan deadlines cannot starve returned media.
	for restoreAfter == nil {
		page, err := repository.NewBatchPage(after, scanPageSize)
		if err != nil {
			return report, errors.Join(append(errs, err)...)
		}
		candidates, err := s.repo.ListVideosForStorageScan(ctx, page)
		if err != nil {
			if ctx.Err() != nil {
				return s.stopped(ctx, report, errs)
			}
			return report, errors.Join(append(errs, err)...)
		}
		if len(candidates) == 0 {
			s.saveCursor(ctx, 0)
			start := int64(0)
			restoreAfter = &start
			if err := s.saveRestoreCursor(ctx, restoreAfter); err != nil {
				return report, errors.Join(append(errs, err)...)
			}
			break
		}
		verdicts, err := s.inspectPage(ctx, candidates)
		if ctx.Err() != nil {
			return s.stopped(ctx, report, errs)
		}
		if notAttached(err) {
			return report, errors.Join(append(errs, err)...)
		}
		if verdicts != nil {
			report.Scanned += len(candidates)
		}
		for _, v := range verdicts {
			if v.state == mediaMissing {
				report.Missing++
			}
			if v.state == mediaPartial {
				report.Partial++
			}
		}
		if err != nil {
			errs = append(errs, err)
		}
		// A failed probe leaves the whole page unchanged; another recording cannot supply its verdict.
		if err == nil {
			for i, c := range candidates {
				if verdicts[i].state != mediaMissing {
					continue
				}
				// Publication may have completed since discovery; inspect current media under ownership.
				changed, err := s.markMissing(ctx, c.VideoID, false)
				if err != nil {
					if ctx.Err() != nil {
						return s.stopped(ctx, report, errs)
					}
					errs = append(errs, err)
					if notAttached(err) {
						return report, errors.Join(errs...)
					}
					continue
				}
				if changed {
					report.Tombstoned++
				}
			}
		}
		if ctx.Err() != nil {
			return s.stopped(ctx, report, errs)
		}
		after = candidates[len(candidates)-1].VideoID
		s.saveCursor(ctx, after)
	}
	complete, restoreErrs := s.restoreReturned(ctx, &report, *restoreAfter)
	errs = append(errs, restoreErrs...)
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			errs = append(errs, ctx.Err())
		}
		return report, errors.Join(errs...)
	}
	report.Complete = complete
	return report, errors.Join(errs...)
}

// summarize emits one event for committed changes, including interrupted runs;
// quiet scans emit none, and explicit actions retain their individual events.
func (s *Service) summarize(ctx context.Context, report Report, sweepErr error) {
	if report.Tombstoned == 0 && report.Restored == 0 {
		return
	}
	s.bus.NotifyVideoChange()
	outcome, severity := "completed", repository.EventLogSeverityInfo
	failed := sweepErr != nil && !errors.Is(sweepErr, context.Canceled)
	if !report.Complete {
		outcome = "paused"
	}
	if failed {
		outcome, severity = "finished with errors", repository.EventLogSeverityWarn
	}
	message := fmt.Sprintf("storage scan %s: %d recordings removed from the library for missing media, %d restored", outcome, report.Tombstoned, report.Restored)
	data := map[string]any{
		"scanned": report.Scanned, "missing": report.Missing, "partial": report.Partial,
		"tombstoned": report.Tombstoned, "restored": report.Restored,
		"complete": report.Complete, "failed": failed,
	}
	s.log.Info(message, "complete", report.Complete, "failed", failed)
	// Audit remains best effort, but the scan deadline must not hide committed changes.
	eventCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cursorTimeout)
	defer cancel()
	eventlog.Emit(eventCtx, s.repo, s.bus, s.log, EventDomain, EventScanReconciled, severity, message, data)
}

// restoreReturned resumes missing-media restoration from its durable cursor.
// Incomplete pages replay; restored rows leave the candidate query.
func (s *Service) restoreReturned(ctx context.Context, report *Report, after int64) (bool, []error) {
	var errs []error
	for {
		if ctx.Err() != nil {
			return false, errs
		}
		page, err := repository.NewBatchPage(after, scanPageSize)
		if err != nil {
			return false, append(errs, err)
		}
		candidates, err := s.repo.ListMissingTombstones(ctx, page)
		if err != nil {
			if ctx.Err() == nil {
				errs = append(errs, err)
			}
			return false, errs
		}
		if len(candidates) == 0 {
			if err := s.saveRestoreCursor(ctx, nil); err != nil {
				return false, append(errs, err)
			}
			return true, errs
		}
		verdicts, err := s.inspectPage(ctx, candidates)
		if ctx.Err() != nil {
			return false, errs
		}
		if err != nil {
			errs = append(errs, err)
			if notAttached(err) {
				return false, errs
			}
		}
		if err == nil {
			for i, c := range candidates {
				if verdicts[i].state != mediaPresent || verdicts[i].total == 0 {
					continue
				}
				if err := s.restore(ctx, c); err != nil {
					if errors.Is(err, ErrArchivedAgain) {
						s.log.Info("recording not restored; its VOD was archived again", "video_id", c.VideoID)
						continue
					}
					errs = append(errs, err)
					continue
				}
				report.Restored++
			}
		}
		if ctx.Err() != nil {
			return false, errs
		}
		after = candidates[len(candidates)-1].VideoID
		if err := s.saveRestoreCursor(ctx, &after); err != nil {
			return false, append(errs, err)
		}
	}
}

// Restore returns a missing tombstone to the library once all its media is present.
// It returns StillMissingError with absent part counts, or ErrNotRestorable when
// no media can be checked.
func (s *Service) Restore(ctx context.Context, id int64) error {
	c, err := s.repo.GetMissingTombstone(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrNotRestorable
	}
	if err != nil {
		return err
	}
	verdicts, err := s.inspectPage(ctx, []repository.StorageScanVideo{*c})
	if err != nil {
		return err
	}
	v := verdicts[0]
	if v.total == 0 {
		return ErrNotRestorable
	}
	if v.state != mediaPresent {
		return &StillMissingError{Missing: v.gone, Total: v.total}
	}
	if err := s.restore(ctx, *c); err != nil {
		return err
	}
	s.log.Info("restored recording; its media is back in storage", "video_id", c.VideoID)
	s.bus.NotifyVideoChange()
	eventlog.Emit(ctx, s.repo, s.bus, s.log, EventDomain, EventRecordingRestored, repository.EventLogSeverityInfo,
		fmt.Sprintf("recording %d is back in the library: its media returned to storage", c.VideoID),
		map[string]any{"video_id": c.VideoID, "filename": c.Filename})
	return nil
}

func (s *Service) restore(ctx context.Context, c repository.StorageScanVideo) error {
	if err := s.repo.RestoreMissingVideo(ctx, c.VideoID); err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return ErrNotRestorable
		case errors.Is(err, repository.ErrDuplicate):
			return ErrArchivedAgain
		}
		return err
	}
	return nil
}

// stopped suppresses deadlines after inspection makes progress, but always returns
// explicit cancellation so shutdown can schedule a prompt retry.
func (s *Service) stopped(ctx context.Context, report Report, errs []error) (Report, error) {
	if report.Scanned == 0 || errors.Is(ctx.Err(), context.Canceled) {
		return report, errors.Join(append(errs, ctx.Err())...)
	}
	return report, errors.Join(errs...)
}

// MarkMissing checks one recording after playback reports absence and returns
// whether attached storage confirmed enough missing media to tombstone it.
func (s *Service) MarkMissing(ctx context.Context, id int64) (bool, error) {
	return s.markMissing(ctx, id, true)
}

func (s *Service) markMissing(ctx context.Context, id int64, announce bool) (bool, error) {
	unlock, err := s.store.Lock(ctx, id)
	if err != nil {
		return false, err
	}
	defer unlock.Close()
	c, err := s.repo.GetVideoForStorageScan(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	verdicts, err := s.inspectPage(ctx, []repository.StorageScanVideo{*c})
	if err != nil {
		return false, err
	}
	if verdicts[0].state != mediaMissing {
		return false, nil
	}
	if announce {
		return s.tombstone(ctx, *c)
	}
	return s.repo.TombstoneMissingVideo(ctx, c.VideoID)
}

func (s *Service) tombstone(ctx context.Context, c repository.StorageScanVideo) (bool, error) {
	changed, err := s.repo.TombstoneMissingVideo(ctx, c.VideoID)
	if err != nil || !changed {
		return false, err
	}
	s.bus.NotifyVideoChange()
	s.log.Info("tombstoned recording with missing media", "video_id", c.VideoID)
	eventlog.Emit(ctx, s.repo, s.bus, s.log, EventDomain, EventRecordingMissing, repository.EventLogSeverityInfo,
		fmt.Sprintf("recording %d removed from the library: its media is missing from storage", c.VideoID),
		map[string]any{"video_id": c.VideoID, "filename": c.Filename})
	return true, nil
}

// verify permits read-only and full storage because scanning only reads objects.
func (s *Service) verify(ctx context.Context) error {
	err := s.store.Verify(ctx)
	if storage.CanRead(err) {
		return nil
	}
	return err
}

func notAttached(err error) bool {
	return errors.Is(err, storage.ErrUnreachable) || errors.Is(err, storage.ErrUnattached)
}

func (s *Service) loadCursors(ctx context.Context) (int64, *int64) {
	settings, err := s.repo.GetServerSettings(ctx)
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			s.log.Warn("load storage scan cursor; starting over", "error", err)
		}
		return 0, nil
	}
	return settings.StorageScanCursor, settings.StorageRestoreCursor
}

// saveCursor persists a completed page even after the run deadline.
func (s *Service) saveCursor(ctx context.Context, cursor int64) {
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cursorTimeout)
	defer cancel()
	if err := s.repo.SetStorageScanCursor(saveCtx, cursor); err != nil {
		s.log.Warn("persist storage scan cursor", "cursor", cursor, "error", err)
	}
}

// saveRestoreCursor persists a completed page independently of the request deadline
// and returns persistence errors so failed saves cannot report durable progress.
func (s *Service) saveRestoreCursor(ctx context.Context, cursor *int64) error {
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cursorTimeout)
	defer cancel()
	if err := s.repo.SetStorageRestoreCursor(saveCtx, cursor); err != nil {
		return fmt.Errorf("persist storage restore cursor: %w", err)
	}
	return nil
}

type mediaState int

const (
	mediaPresent mediaState = iota
	mediaPartial
	mediaMissing
)

type verdict struct {
	state       mediaState
	gone, total int
}

func (s *Service) inspectPage(ctx context.Context, candidates []repository.StorageScanVideo) ([]verdict, error) {
	if err := s.verify(ctx); err != nil {
		return nil, err
	}
	ids := make([]int64, len(candidates))
	for i, c := range candidates {
		ids[i] = c.VideoID
	}
	parts, err := s.repo.ListVideoPartsForVideos(ctx, ids)
	if err != nil {
		return nil, err
	}
	byVideo := make(map[int64][]repository.VideoPart, len(candidates))
	for _, p := range parts {
		byVideo[p.VideoID] = append(byVideo[p.VideoID], p)
	}
	verdicts := make([]verdict, len(candidates))
	errs := make([]error, len(candidates))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(scanWorkers, len(candidates)) {
		wg.Go(func() {
			for i := range jobs {
				verdicts[i], errs[i] = s.inspectBounded(ctx, candidates[i], byVideo[candidates[i].VideoID])
			}
		})
	}
	for i := range candidates {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return verdicts, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return verdicts, err
	}
	// A volume detached during inspection can falsely report every subsequent object missing.
	if err := s.verify(ctx); err != nil {
		return verdicts, err
	}
	return verdicts, nil
}

// inspectBounded retains a probe slot until filesystem I/O settles, even after
// the caller stops waiting; late workers only inspect and cannot tombstone.
func (s *Service) inspectBounded(ctx context.Context, c repository.StorageScanVideo, parts []repository.VideoPart) (verdict, error) {
	probeCtx, cancel := context.WithTimeout(ctx, inspectTimeout)
	defer cancel()
	select {
	case s.probes <- struct{}{}:
	case <-probeCtx.Done():
		return verdict{}, probeCtx.Err()
	}
	if err := probeCtx.Err(); err != nil {
		<-s.probes
		return verdict{}, err
	}
	type result struct {
		verdict verdict
		err     error
	}
	results := make(chan result, 1)
	go func() {
		defer func() { <-s.probes }()
		v, err := s.inspect(probeCtx, c, parts)
		results <- result{verdict: v, err: err}
	}()
	select {
	case res := <-results:
		if err := probeCtx.Err(); err != nil {
			return verdict{}, err
		}
		return res.verdict, res.err
	case <-probeCtx.Done():
		return verdict{}, probeCtx.Err()
	}
}

func (s *Service) inspect(ctx context.Context, c repository.StorageScanVideo, parts []repository.VideoPart) (verdict, error) {
	paths := storagekeys.MediaPaths(parts)
	if len(paths) == 0 {
		return verdict{state: mediaMissing}, nil
	}
	gone := 0
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return verdict{}, err
		}
		err := s.statMedia(ctx, p)
		switch {
		case err == nil:
		case errors.Is(err, fs.ErrNotExist):
			gone++
		default:
			return verdict{}, fmt.Errorf("stat %s: %w", p, err)
		}
	}
	v := verdict{gone: gone, total: len(paths)}
	if gone == 0 {
		return v, nil
	}
	if gone < len(paths) {
		v.state = mediaPartial
		return v, nil
	}
	// A ready playback artifact can preserve the recording after every source part disappears.
	if len(parts) > 1 {
		asset, err := s.repo.GetVideoPlaybackAsset(ctx, c.VideoID)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return verdict{}, err
		}
		if err == nil && asset.Status == repository.PlaybackAssetStatusReady && asset.Filename != nil {
			err := s.statMedia(ctx, storagekeys.Video(*asset.Filename))
			if err == nil {
				v.state = mediaPartial
				return v, nil
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return verdict{}, err
			}
		}
	}
	v.state = mediaMissing
	return v, nil
}

func (s *Service) statMedia(ctx context.Context, key string) error {
	info, err := s.store.Stat(ctx, key)
	if err != nil {
		return err
	}
	if info.IsDir {
		return fmt.Errorf("media path %s is a directory", key)
	}
	return nil
}
