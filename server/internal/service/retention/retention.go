// Package retention enforces the delete window captured when a recording starts.
// The window uses the shortest matching schedule and begins at completion; a
// NULL window keeps manual recordings and recordings without a delete policy.
package retention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/mediastore"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

// maxSnapshotProbe bounds legacy snapshot discovery when storage never reports a gap.
// Journaled snapshots beyond gaps are removed separately by PurgePublications.
const maxSnapshotProbe = 100_000

const (
	// ManualDeletionTaskName identifies the worker awakened by manual delete requests.
	ManualDeletionTaskName = "recording_delete_requested"
	// ManualDeletionTaskDescription is shared by startup registration and delete admission.
	ManualDeletionTaskDescription = "Delete recordings queued by an operator, after webhook part metadata is frozen"
	// ManualDeletionIntervalSeconds bounds the delay after a failed worker wakeup.
	ManualDeletionIntervalSeconds int64 = 60

	manualDeleteBatchSize = 25
	retentionBatchSize    = 100
)

// ErrManualDeletionUnavailable means no worker can drain the manual delete queue;
// callers must reject the request before setting delete_requested_at.
var ErrManualDeletionUnavailable = errors.New("manual recording deletion worker unavailable")

// Option configures a Service before use.
type Option func(*Service)

// WithManualDeletionWorkerAvailable rejects manual deletes when this process
// cannot drain them; otherwise accepted requests would remain queued indefinitely.
func WithManualDeletionWorkerAvailable(available bool) Option {
	return func(s *Service) {
		s.manualDeletionWorkerAvailable = available
	}
}

// WithEventBus publishes committed recording changes to subscribers.
func WithEventBus(bus *eventbus.Buses) Option {
	return func(s *Service) { s.bus = bus }
}

// Service deletes expired recordings when the scheduler calls Sweep.
type Service struct {
	manualMu                      sync.Mutex
	manualAfter                   int64
	repo                          repository.Repository
	store                         *mediastore.Store
	log                           *slog.Logger
	manualDeletionWorkerAvailable bool
	bus                           *eventbus.Buses
}

// New creates a retention service using the shared media store.
func New(repo repository.Repository, store *mediastore.Store, log *slog.Logger, opts ...Option) *Service {
	s := &Service{
		repo:                          repo,
		store:                         store,
		log:                           log.With("domain", "retention"),
		manualDeletionWorkerAvailable: true,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Sweep returns the number of recordings deleted after their retention deadline.
// Individual failures are joined after the remaining candidates are processed.
func (s *Service) Sweep(ctx context.Context, now time.Time) (int, error) {
	var deleted int
	var errs []error
	for after := int64(0); ; {
		page, err := repository.NewBatchPage(after, retentionBatchSize)
		if err != nil {
			return deleted, errors.Join(append(errs, err)...)
		}
		videos, err := s.repo.ListRetentionCandidates(ctx, now, page)
		if err != nil {
			return deleted, errors.Join(append(errs, err)...)
		}
		for _, candidate := range videos {
			after = candidate.VideoID
			v, err := s.repo.GetVideo(ctx, candidate.VideoID)
			if err == nil {
				// A retry may have completed since discovery with a new retention deadline.
				var expired []int64
				expired, err = expiredVideoIDs([]repository.RetentionVideo{{
					VideoID: v.ID, BroadcasterID: v.BroadcasterID,
					DownloadedAt: v.DownloadedAt, RetentionWindowHours: v.RetentionWindowHours,
				}}, now)
				if err == nil && len(expired) > 0 {
					err = s.DeleteRecording(ctx, v, repository.DeletionKindRetention)
					if err == nil {
						deleted++
					}
				}
			}
			if err != nil {
				s.log.Warn("retention candidate deferred", "video_id", candidate.VideoID, "error", err)
				if len(errs) < 16 {
					errs = append(errs, fmt.Errorf("recording %d: %w", candidate.VideoID, err))
				}
			}
		}
		if len(videos) < 100 || ctx.Err() != nil {
			break
		}
	}

	return deleted, errors.Join(errs...)
}

// expiredVideoIDs selects recordings strictly past their stored retention window.
// Keep this boundary aligned with ListRetentionCandidates; corrupt rows are skipped with errors.
func expiredVideoIDs(videos []repository.RetentionVideo, now time.Time) ([]int64, error) {
	var errs []error
	var out []int64
	for _, v := range videos {
		if v.RetentionWindowHours == nil {
			errs = append(errs, fmt.Errorf("retention: video %d is a candidate but has no retention_window_hours", v.VideoID))
			continue
		}
		window, err := retentionWindow(*v.RetentionWindowHours)
		if err != nil {
			errs = append(errs, fmt.Errorf("retention: video %d has invalid retention window: %w", v.VideoID, err))
			continue
		}
		if v.DownloadedAt == nil {
			errs = append(errs, fmt.Errorf("retention: video %d is terminal but has no downloaded_at", v.VideoID))
			continue
		}
		if now.Sub(*v.DownloadedAt) > window {
			out = append(out, v.VideoID)
		}
	}
	return out, errors.Join(errs...)
}

func retentionWindow(hours int64) (time.Duration, error) {
	if hours <= 0 {
		return 0, fmt.Errorf("window must be > 0 hours, got %d", hours)
	}
	if hours > repository.MaxRetentionWindowHours {
		return 0, fmt.Errorf("window %d exceeds maximum %d hours", hours, repository.MaxRetentionWindowHours)
	}
	return time.Duration(hours) * time.Hour, nil
}

// RequestManualDelete queues a terminal recording for removal and wakes the worker.
// Deletion waits until pending webhook deliveries have frozen their part metadata.
func (s *Service) RequestManualDelete(ctx context.Context, v *repository.Video) error {
	if v == nil {
		return fmt.Errorf("queue manual delete: nil video")
	}
	if err := s.ensureManualDeletionWorker(ctx); err != nil {
		return err
	}
	if _, err := s.repo.RequestVideoDelete(ctx, v.ID); err != nil {
		return fmt.Errorf("queue manual delete: %w", err)
	}
	s.bus.NotifyVideoChange()
	if err := s.repo.SetTaskNextRun(ctx, ManualDeletionTaskName); err != nil {
		// The interval worker will drain this committed request even if wakeup fails.
		s.log.Warn("manual delete queued but task wakeup failed", "video_id", v.ID, "error", err)
	}
	return nil
}

func (s *Service) ensureManualDeletionWorker(ctx context.Context) error {
	if !s.manualDeletionWorkerAvailable {
		return ErrManualDeletionUnavailable
	}
	task, err := s.repo.UpsertTask(ctx, ManualDeletionTaskName, ManualDeletionTaskDescription, ManualDeletionIntervalSeconds)
	if err != nil {
		return fmt.Errorf("register manual deletion task: %w", err)
	}
	if !task.IsEnabled || task.IntervalSeconds <= 0 {
		return ErrManualDeletionUnavailable
	}
	return nil
}

// ProcessManualDeletes removes a bounded batch of queued recordings whose
// webhook deliveries have frozen their part metadata.
func (s *Service) ProcessManualDeletes(ctx context.Context) (int, error) {
	s.manualMu.Lock()
	defer s.manualMu.Unlock()
	page, err := repository.NewBatchPage(s.manualAfter, manualDeleteBatchSize)
	if err != nil {
		return 0, fmt.Errorf("list manual deletes: %w", err)
	}
	videos, err := s.repo.ListVideosPendingManualDelete(ctx, page)
	if err != nil {
		return 0, fmt.Errorf("list manual deletes: %w", err)
	}
	var (
		deleted int
		errs    []error
	)
	for i := range videos {
		s.manualAfter = videos[i].ID
		if err := s.DeleteRecording(ctx, &videos[i], repository.DeletionKindManual); err != nil {
			errs = append(errs, fmt.Errorf("delete recording %d: %w", videos[i].ID, err))
			continue
		}
		deleted++
	}
	if len(videos) < manualDeleteBatchSize {
		s.manualAfter = 0
	}
	return deleted, errors.Join(errs...)
}

// DeleteRecording purges media before tombstoning the recording and its parts.
// Failed purges leave the recording available for retry.
func (s *Service) DeleteRecording(ctx context.Context, v *repository.Video, kind string) error {
	if kind == repository.DeletionKindMissing {
		return fmt.Errorf("missing media must be reconciled without deleting objects")
	}
	unlock, err := s.store.Lock(ctx, v.ID)
	if err != nil {
		return err
	}
	defer unlock.Close()
	if err := s.verifyStorage(ctx); err != nil {
		return err
	}
	// Archive retries share this lock; discovery cannot authorize purging a newer attempt.
	fresh, err := s.repo.GetVideo(ctx, v.ID)
	if err != nil {
		return err
	}
	if fresh.JobID != v.JobID || (fresh.Status != repository.VideoStatusDone && fresh.Status != repository.VideoStatusFailed) {
		return repository.ErrStaleExecution
	}
	v = fresh
	parts, err := s.repo.ListVideoParts(ctx, v.ID)
	if err != nil {
		return fmt.Errorf("list parts: %w", err)
	}
	if err := s.purgeObjects(ctx, unlock, v, parts); err != nil {
		return err
	}
	if err := unlock.PurgePublications(ctx); err != nil {
		return err
	}
	// A mount change during purge must leave rows available for retry on trusted storage.
	if err := s.verifyStorage(ctx); err != nil {
		return err
	}
	// Soft deletion does not trigger the playback asset foreign key cascade.
	if err := s.repo.DeleteVideoPlaybackAsset(ctx, v.ID); err != nil {
		return fmt.Errorf("delete playback asset row: %w", err)
	}
	if err := s.repo.DeleteVideoWaveformKey(ctx, v.ID); err != nil {
		return fmt.Errorf("delete waveform reference: %w", err)
	}
	if err := s.repo.FinalizeDelete(ctx, v.ID, kind); err != nil {
		return fmt.Errorf("finalize db delete: %w", err)
	}
	s.bus.NotifyVideoChange()
	s.log.Info("deleted recording",
		"video_id", v.ID, "broadcaster_id", v.BroadcasterID, "parts", len(parts), "kind", kind)
	return nil
}

// purgeObjects removes referenced objects before their database rows are deleted.
func (s *Service) purgeObjects(ctx context.Context, owned *mediastore.Recording, v *repository.Video, parts []repository.VideoPart) error {
	for i := range parts {
		// Derive preview keys from the stored part name so purge matches publication.
		base := storagekeys.Base(parts[i].Filename)
		for _, p := range []string{
			storagekeys.Video(parts[i].Filename),
			storagekeys.Thumbnail(base),
			storagekeys.Strip(base),
		} {
			if err := owned.Delete(ctx, p); err != nil {
				return fmt.Errorf("delete object %s: %w", p, err)
			}
		}
	}
	if v.Thumbnail != nil {
		if err := owned.Delete(ctx, *v.Thumbnail); err != nil {
			return fmt.Errorf("delete object %s: %w", *v.Thumbnail, err)
		}
	}
	asset, err := s.repo.GetVideoPlaybackAsset(ctx, v.ID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return err
	}
	if err == nil && asset.Filename != nil {
		if err := owned.Delete(ctx, storagekeys.Video(*asset.Filename)); err != nil {
			return err
		}
	}
	waveformKey, err := s.repo.GetVideoWaveformKey(ctx, v.ID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return err
	}
	if err == nil {
		if err := owned.Delete(ctx, waveformKey); err != nil {
			return fmt.Errorf("delete waveform %s: %w", waveformKey, err)
		}
	}
	return s.purgeSnapshots(ctx, owned, v.Filename)
}

// purgeSnapshots discovers legacy snapshots through the first missing index.
// Delete in reverse order so interrupted purges leave a discoverable prefix.
func (s *Service) purgeSnapshots(ctx context.Context, owned *mediastore.Recording, filename string) error {
	var found []string
	for i := range maxSnapshotProbe {
		p := storagekeys.Snapshot(filename, i)
		if err := s.verifyStorage(ctx); err != nil {
			return err
		}
		exists, err := s.store.Exists(ctx, p)
		if err != nil {
			return fmt.Errorf("probe snapshot %s: %w", p, err)
		}
		if !exists {
			break
		}
		found = append(found, p)
	}
	if len(found) == maxSnapshotProbe {
		// The probe ceiling can leave unjournaled snapshots behind; report incomplete discovery.
		s.log.Warn("retention: snapshot purge hit probe ceiling; some snapshots may remain",
			"filename", filename, "ceiling", maxSnapshotProbe)
	}
	for i := len(found) - 1; i >= 0; i-- {
		if err := owned.Delete(ctx, found[i]); err != nil {
			return fmt.Errorf("delete snapshot %s: %w", found[i], err)
		}
	}
	return nil
}

func (s *Service) verifyStorage(ctx context.Context) error {
	if err := s.store.Verify(ctx); !storage.CanDelete(err) {
		return fmt.Errorf("storage unavailable for deletion: %w", err)
	}
	return nil
}
