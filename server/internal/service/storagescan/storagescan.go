// Package storagescan reconciles terminal recordings whose media left storage.
// Discovery only writes a tombstone: it preserves objects and their metadata,
// including preview images used in history. Manual and retention deletion own
// destructive cleanup.
package storagescan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sync"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

const (
	scanPageSize       = 64
	scanWorkers        = 8
	inspectTimeout     = 10 * time.Second
	maxMissingShare    = 0.5
	minMissingToRefuse = 3
)

var (
	ErrStorageUnreachable = errors.New("storage scan: storage root unreachable")
	ErrTooManyMissing     = errors.New("storage scan: too many recordings missing, refusing to tombstone")
)

type Service struct {
	repo   repository.Repository
	store  storage.Storage
	log    *slog.Logger
	sweep  chan struct{}
	probes chan struct{}
	// Only the sweep holder accesses nextID. Completed pages survive a task
	// deadline; a later run resumes there, then wraps at the end of the library.
	nextID int64
}

func New(repo repository.Repository, store storage.Storage, log *slog.Logger) *Service {
	return &Service{repo: repo, store: store, log: log.With("domain", "storagescan"), sweep: make(chan struct{}, 1), probes: make(chan struct{}, scanWorkers)}
}

type Report struct{ Scanned, Missing, Partial, Tombstoned int }

// Sweep processes bounded pages, batching part reads and limiting concurrent
// storage probes. Completed pages commit independently, so a large library
// makes progress within the scheduler's deadline. Suspicious pages fail closed.
func (s *Service) Sweep(ctx context.Context) (Report, error) {
	select {
	case s.sweep <- struct{}{}:
		defer func() { <-s.sweep }()
	case <-ctx.Done():
		return Report{}, ctx.Err()
	}
	var report Report
	var errs []error
	for {
		candidates, err := s.repo.ListVideosForStorageScan(ctx, s.nextID, scanPageSize)
		if err != nil {
			return report, errors.Join(append(errs, err)...)
		}
		if len(candidates) == 0 {
			s.nextID = 0
			return report, errors.Join(errs...)
		}
		states, err := s.inspectPage(ctx, candidates)
		if ctx.Err() != nil {
			return report, errors.Join(err, ctx.Err())
		}
		if states != nil {
			report.Scanned += len(candidates)
		}
		for _, state := range states {
			if state == mediaMissing {
				report.Missing++
			}
			if state == mediaPartial {
				report.Partial++
			}
		}
		if err != nil {
			errs = append(errs, err)
		}
		// Any probe error refuses the page: failed probes must not dilute the
		// missing-share denominator and turn an outage into a successful scan.
		if err == nil {
			for i, c := range candidates {
				if states[i] != mediaMissing {
					continue
				}
				changed, err := s.repo.TombstoneMissingVideo(ctx, c.VideoID)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				if changed {
					report.Tombstoned++
					s.log.Info("tombstoned recording with missing media", "video_id", c.VideoID)
				}
			}
		}
		if ctx.Err() != nil {
			return report, errors.Join(append(errs, ctx.Err())...)
		}
		s.nextID = candidates[len(candidates)-1].VideoID
	}
}

// MarkMissing applies the same outage guard as the scheduled scan. The exact
// candidate lookup rejects nonpositive IDs and cannot select another recording.
func (s *Service) MarkMissing(ctx context.Context, id int64) (bool, error) {
	c, err := s.repo.GetVideoForStorageScan(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// Include nearby recordings as an outage sample, with the target guaranteed
	// to be present. This is a conservative guard, not proof of storage identity.
	after := max(int64(0), id-scanPageSize/2)
	candidates, err := s.repo.ListVideosForStorageScan(ctx, after, scanPageSize)
	if err != nil {
		return false, err
	}
	found := false
	for _, v := range candidates {
		if v.VideoID == id {
			found = true
			break
		}
	}
	if !found {
		candidates = append(candidates, *c)
	}
	states, err := s.inspectPage(ctx, candidates)
	if err != nil {
		return false, err
	}
	for i, v := range candidates {
		if v.VideoID == id && states[i] == mediaMissing {
			return s.repo.TombstoneMissingVideo(ctx, id)
		}
	}
	return false, nil
}

func (s *Service) probeRoot(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	prober, ok := s.store.(storage.RootProber)
	if !ok {
		return fmt.Errorf("%w: backend does not support a root probe", ErrStorageUnreachable)
	}
	if err := prober.ProbeRoot(ctx); err != nil {
		return fmt.Errorf("%w: %w", ErrStorageUnreachable, err)
	}
	return nil
}

type mediaState int

const (
	mediaPresent mediaState = iota
	mediaPartial
	mediaMissing
)

func (s *Service) inspectPage(ctx context.Context, candidates []repository.StorageScanVideo) ([]mediaState, error) {
	if err := s.probeRoot(ctx); err != nil {
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
	states := make([]mediaState, len(candidates))
	errs := make([]error, len(candidates))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(scanWorkers, len(candidates)) {
		wg.Go(func() {
			for i := range jobs {
				select {
				case s.probes <- struct{}{}:
				case <-ctx.Done():
					errs[i] = ctx.Err()
					continue
				}
				probeCtx, cancel := context.WithTimeout(ctx, inspectTimeout)
				states[i], errs[i] = s.inspect(probeCtx, candidates[i], byVideo[candidates[i].VideoID])
				cancel()
				<-s.probes
			}
		})
	}
	for i := range candidates {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return states, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return states, err
	}
	// Recheck after the probes too, catching a mount disappearing mid-page.
	if err := s.probeRoot(ctx); err != nil {
		return states, err
	}
	missing := 0
	for _, state := range states {
		if state == mediaMissing {
			missing++
		}
	}
	if missing >= minMissingToRefuse && float64(missing) > maxMissingShare*float64(len(candidates)) {
		return states, fmt.Errorf("%w: %d of %d; verify storage before retrying", ErrTooManyMissing, missing, len(candidates))
	}
	return states, nil
}

func (s *Service) inspect(ctx context.Context, c repository.StorageScanVideo, parts []repository.VideoPart) (mediaState, error) {
	paths := mediaPaths(c.Filename, c.Status, parts)
	if len(paths) == 0 {
		return mediaPresent, nil
	}
	gone := 0
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return mediaPresent, err
		}
		_, err := s.store.Stat(ctx, p)
		switch {
		case err == nil:
		case errors.Is(err, fs.ErrNotExist):
			gone++
		default:
			return mediaPresent, fmt.Errorf("stat %s: %w", p, err)
		}
	}
	if gone == 0 {
		return mediaPresent, nil
	}
	if gone < len(paths) {
		return mediaPartial, nil
	}
	// A ready continuous-playback artifact may still contain the whole recording.
	// Preserve that playable copy even when all original parts disappeared.
	if len(parts) > 1 {
		asset, err := s.repo.GetVideoPlaybackAsset(ctx, c.VideoID)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return mediaPresent, err
		}
		if err == nil && asset.Status == repository.PlaybackAssetStatusReady && asset.Filename != nil {
			_, err := s.store.Stat(ctx, storagekeys.Video(*asset.Filename))
			if err == nil {
				return mediaPartial, nil
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return mediaPresent, err
			}
		}
	}
	return mediaMissing, nil
}

// Historical DONE rows predate video_parts and use a single MP4 key, matching
// playback's zero-part fallback. Failed rows without parts never owned media.
func mediaPaths(filename, status string, parts []repository.VideoPart) []string {
	if len(parts) == 0 {
		if status == repository.VideoStatusDone {
			return []string{storagekeys.Video(filename + ".mp4")}
		}
		return nil
	}
	paths := make([]string, len(parts))
	for i, p := range parts {
		paths[i] = storagekeys.Video(p.Filename)
	}
	return paths
}
