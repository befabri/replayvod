// Package storagescan reconciles terminal recordings whose media left storage.
// Discovery only writes a tombstone: it preserves objects and their metadata,
// including preview images used in history. Manual and retention deletion own
// destructive cleanup. Nothing is judged missing unless storage is attached:
// reachable and carrying this install's identity marker.
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

// ErrNotRestorable means the recording is not a missing-media tombstone whose
// media can be checked: it is live, removed for good, queued for a manual
// delete, or a failed tombstone that never owned media.
var ErrNotRestorable = errors.New("storage scan: recording is not a restorable tombstone")

// ErrStillMissing is the sentinel every StillMissingError matches.
var ErrStillMissing = errors.New("storage scan: media is still missing")

// ErrArchivedAgain means the VOD was archived anew while the tombstone was
// missing, and the library keeps one open row per VOD: the operator removes
// one of the two before the other can come back.
var ErrArchivedAgain = errors.New("storage scan: this VOD was archived again; remove one of the two copies first")

// StillMissingError reports how much of a tombstone's media is still absent.
type StillMissingError struct {
	Missing, Total int
}

func (e *StillMissingError) Error() string {
	return fmt.Sprintf("%d of %d parts are still missing", e.Missing, e.Total)
}

func (e *StillMissingError) Is(target error) bool { return target == ErrStillMissing }

// Readiness answers whether storage may be trusted right now. Verify runs a
// fresh probe and returns nil only when storage is attached; the error wraps
// storage.ErrUnreachable, ErrUnattached, ErrReadOnly or ErrFull.
type Readiness interface {
	Verify(ctx context.Context) error
}

type notReady struct{}

func (notReady) Verify(context.Context) error {
	return fmt.Errorf("%w: no storage readiness monitor", storage.ErrUnreachable)
}

type Service struct {
	repo   repository.Repository
	store  storage.Storage
	ready  Readiness
	bus    *eventbus.Buses
	log    *slog.Logger
	sweep  chan struct{}
	probes chan struct{}
}

type Option func(*Service)

// WithEventBus mirrors reconciliation summaries and individual playback/manual
// actions onto the SSE bus.
func WithEventBus(bus *eventbus.Buses) Option {
	return func(s *Service) { s.bus = bus }
}

// New builds the scan. A nil readiness fails closed: nothing is ever judged
// missing without a monitor vouching for the storage.
func New(repo repository.Repository, store storage.Storage, ready Readiness, log *slog.Logger, opts ...Option) *Service {
	if ready == nil {
		ready = notReady{}
	}
	s := &Service{
		repo: repo, store: store, ready: ready,
		log:    log.With("domain", "storagescan"),
		sweep:  make(chan struct{}, 1),
		probes: make(chan struct{}, scanWorkers),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type Report struct {
	Scanned, Missing, Partial, Tombstoned, Restored int
	// Complete is false when the run stopped with work left; the next run
	// resumes from the persisted cursor.
	Complete bool
}

// Sweep processes bounded pages, batching part reads and limiting concurrent
// storage probes. Completed pages commit independently and advance a cursor
// persisted in server settings, so a library too large for one scheduler
// deadline is finished across runs and restarts. A run that finished at least
// one page before its deadline is a success with Complete false.
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
	// A pending restore pass has already finished normal scanning. Resume it
	// directly so a large library does not spend every deadline scanning again.
	for restoreAfter == nil {
		candidates, err := s.repo.ListVideosForStorageScan(ctx, after, scanPageSize)
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
		// Any probe error refuses the page: a recording that could not be
		// checked must not be tombstoned on a neighbour's verdict.
		if err == nil {
			for i, c := range candidates {
				if verdicts[i].state != mediaMissing {
					continue
				}
				changed, err := s.repo.TombstoneMissingVideo(ctx, c.VideoID)
				if err != nil {
					errs = append(errs, err)
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

// summarize emits one bounded event for the changes this run committed, even
// when cancellation or an error stopped it partway through. Quiet scans do
// not add activity rows. Per-recording audit events belong to explicit actions.
func (s *Service) summarize(ctx context.Context, report Report, sweepErr error) {
	if report.Tombstoned == 0 && report.Restored == 0 {
		return
	}
	s.notifyRemoval()
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
	// The scan deadline must not hide changes already committed. Audit failure
	// is still best effort and cannot change the sweep's reconciliation result.
	eventCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cursorTimeout)
	defer cancel()
	eventlog.Emit(eventCtx, s.repo, s.bus, s.log, EventDomain, EventScanReconciled, severity, message, data)
}

// restoreReturned is the sweep's second phase: every missing tombstone whose
// media is fully present again comes back into the library. The durable cursor
// skips completed pages of still-missing files across deadlines and restarts.
// A partially processed page replays; already restored rows leave the query.
func (s *Service) restoreReturned(ctx context.Context, report *Report, after int64) (bool, []error) {
	var errs []error
	for {
		if ctx.Err() != nil {
			return false, errs
		}
		candidates, err := s.repo.ListMissingTombstones(ctx, after, scanPageSize)
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

// Restore brings one missing-media tombstone back once all of its media is
// present again. Anything still absent is reported with counts; a tombstone
// that cannot be checked at all is ErrNotRestorable.
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
	s.notifyRemoval()
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

func (s *Service) notifyRemoval() {
	if s.bus != nil && s.bus.VideoRemovals != nil {
		s.bus.VideoRemovals.Publish(eventbus.VideoRemovalEvent{})
	}
}

// stopped reports a run cut short by its deadline. The pages it finished are
// committed and the cursor points past them, so that is progress, not a
// failure; only a run that finished nothing surfaces the deadline. Explicit
// cancellation is always surfaced so shutdown can schedule a prompt retry.
func (s *Service) stopped(ctx context.Context, report Report, errs []error) (Report, error) {
	if report.Scanned == 0 || errors.Is(ctx.Err(), context.Canceled) {
		return report, errors.Join(append(errs, ctx.Err())...)
	}
	return report, errors.Join(errs...)
}

// MarkMissing tombstones one recording whose media is gone. The playback path
// calls it after a definitive not-found; only the target's own parts are
// inspected, and only on attached storage.
func (s *Service) MarkMissing(ctx context.Context, id int64) (bool, error) {
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
	return s.tombstone(ctx, *c)
}

func (s *Service) tombstone(ctx context.Context, c repository.StorageScanVideo) (bool, error) {
	changed, err := s.repo.TombstoneMissingVideo(ctx, c.VideoID)
	if err != nil || !changed {
		return false, err
	}
	s.notifyRemoval()
	s.log.Info("tombstoned recording with missing media", "video_id", c.VideoID)
	eventlog.Emit(ctx, s.repo, s.bus, s.log, EventDomain, EventRecordingMissing, repository.EventLogSeverityInfo,
		fmt.Sprintf("recording %d removed from the library: its media is missing from storage", c.VideoID),
		map[string]any{"video_id": c.VideoID, "filename": c.Filename})
	return true, nil
}

// verify accepts read-only and full storage: the scan reads objects and writes
// database rows.
func (s *Service) verify(ctx context.Context) error {
	err := s.ready.Verify(ctx)
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

// saveCursor commits the resume position even when the run's context has
// expired: the page it points past is done.
func (s *Service) saveCursor(ctx context.Context, cursor int64) {
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cursorTimeout)
	defer cancel()
	if err := s.repo.SetStorageScanCursor(saveCtx, cursor); err != nil {
		s.log.Warn("persist storage scan cursor", "cursor", cursor, "error", err)
	}
}

// Saving a completed restore page is independent of the request deadline.
// Surface a persistence failure so it cannot masquerade as durable progress.
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

// verdict is one recording's inspection: its state plus how many of its
// media objects are absent, so a refusal can say how much is still missing.
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
	// Recheck after the probes too: a volume that detached mid-page would have
	// answered not-found for every object stat'd after it went away.
	if err := s.verify(ctx); err != nil {
		return verdicts, err
	}
	return verdicts, nil
}

// A filesystem syscall can ignore cancellation. Let its caller stop waiting
// while keeping the global probe slot occupied until the I/O actually returns;
// repeated requests can strand at most scanWorkers workers across this service.
// Workers only inspect and cannot publish a tombstone after the caller leaves.
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
	paths := storagekeys.MediaPaths(c.Filename, c.Status, parts)
	if len(paths) == 0 {
		return verdict{state: mediaPresent}, nil
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
	// A ready continuous-playback artifact may still contain the whole recording.
	// Preserve that playable copy even when all original parts disappeared.
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
