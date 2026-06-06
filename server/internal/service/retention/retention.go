// Package retention enforces schedule-snapshotted auto-delete of recordings.
//
// A download_schedule may opt into is_delete_rediff with a time_before_delete
// window (in hours). When the schedule processor starts a recording, it stores
// the shortest delete window from the schedules that actually matched on the
// video row. Once that terminal recording is older than the stored window it
// should be removed to reclaim disk. Manual recordings and schedule recordings
// without a matched delete policy keep retention_window_hours NULL and are not
// retention candidates.
//
// The window is measured from completion (videos.downloaded_at), not from
// when the recording was triggered — "auto-delete after N hours" means N
// hours after the rediff finished and became watchable.
package retention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

// maxSnapshotProbe is a defensive ceiling on purgeSnapshots' probe-until-gap
// loop. Live snapshots are written contiguously (the writer advances the index
// only on a successful capture), so the loop normally stops at the first gap;
// this bound only guards a pathological store that never reports one. It is
// deliberately NOT the video API's 500-frame ListSnapshots reader cap: the
// hover-preview only needs a sample, but retention must delete every snapshot or
// it strands bytes on disk. Snapshots fire ~every 5 min, so 100k is ~347 days of
// one continuous recording, far past anything real.
const maxSnapshotProbe = 100_000

const (
	// ManualDeletionTaskName is the scheduler task that drains
	// operator-requested recording deletions. The video API wakes this task
	// after queueing a delete.
	ManualDeletionTaskName = "recording_delete_requested"
	// ManualDeletionTaskDescription is kept beside the task name so the queueing
	// path can ensure the durable task row exists before accepting a delete.
	ManualDeletionTaskDescription = "Delete recordings queued by an operator, after webhook part metadata is frozen"
	// ManualDeletionIntervalSeconds is short enough that a failed wake-up still
	// drains queued deletes promptly on the next scheduler interval.
	ManualDeletionIntervalSeconds int64 = 60

	manualDeleteBatchSize = 25
)

// ErrManualDeletionUnavailable means the operator-requested delete queue cannot
// currently be drained. The API must not accept a delete request in this state:
// setting delete_requested_at without an enabled worker would strand the row in
// a pending state.
var ErrManualDeletionUnavailable = errors.New("manual recording deletion worker unavailable")

// Option tweaks retention service behaviour for the process it is wired into.
type Option func(*Service)

// WithManualDeletionWorkerAvailable tells RequestManualDelete whether this
// process has a scheduler worker that can drain ManualDeletionTaskName. The
// scheduler task itself leaves the default true; the API-facing service passes
// false when the scheduler is disabled by config so video.delete fails before
// queueing a row that no background task will ever process.
func WithManualDeletionWorkerAvailable(available bool) Option {
	return func(s *Service) {
		s.manualDeletionWorkerAvailable = available
	}
}

// Service deletes recordings once their stored retention window elapses. It
// owns no scheduling of its own — the scheduler's recordings_retention task
// drives Sweep on an interval.
type Service struct {
	repo                          repository.Repository
	store                         storage.Storage
	log                           *slog.Logger
	manualDeletionWorkerAvailable bool
}

// New builds the retention service. store is required: a pass that can't
// reach the object store would tombstone rows while leaving the files
// behind — the exact orphan the sweep exists to prevent — so main.go only
// constructs this once a storage backend is up.
func New(repo repository.Repository, store storage.Storage, log *slog.Logger, opts ...Option) *Service {
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

// Sweep deletes every recording whose completion is older than the retention
// window stored on the recording. now is injected so the boundary is
// deterministic in tests; the task passes time.Now(). Returns the count
// deleted.
//
// A bad candidate row or per-recording failure (object store hiccup mid-purge,
// etc.) is collected and the sweep continues with the rest; the joined error
// fails the task run so the operator sees it, and the next run retries. Every
// step is idempotent, so a partial pass converges.
func (s *Service) Sweep(ctx context.Context, now time.Time) (int, error) {
	videos, err := s.repo.ListFinishedVideosForRetention(ctx, now)
	if err != nil {
		return 0, fmt.Errorf("list finished videos: %w", err)
	}
	var (
		deleted int
		errs    []error
	)
	expired, err := expiredVideoIDs(videos, now)
	if err != nil {
		errs = append(errs, err)
	}
	for _, id := range expired {
		v, err := s.repo.GetVideo(ctx, id)
		if err != nil {
			errs = append(errs, fmt.Errorf("load recording %d: %w", id, err))
			continue
		}
		if err := s.DeleteRecording(ctx, v, repository.DeletionKindRetention); err != nil {
			errs = append(errs, fmt.Errorf("delete recording %d: %w", id, err))
			continue
		}
		deleted++
	}
	return deleted, errors.Join(errs...)
}

// expiredVideoIDs is the pure eligibility decision. It uses the creation-time
// retention window stored on each video, then selects the finished recordings
// whose completion is strictly older than that window. A recording exactly at
// the window is kept and deleted on the first sweep past it. A video with no
// retention window is never selected.
//
// ListFinishedVideosForRetention applies the same strict due-time comparison in
// SQL to keep sweeps bounded to due rows; keep the two boundaries in lockstep.
//
// It reports impossible shapes while continuing with the remaining rows. A
// null, <=0, or duration-overflowing retention window, or a null completion on
// a row the query already filtered to downloaded_at IS NOT NULL, signals
// corruption. The offending row is skipped and returned in the joined error so
// one bad row cannot halt unrelated deletion.
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

// RequestManualDelete marks a terminal recording for background removal and
// nudges the scheduler task to run on its next tick. The actual purge is not
// performed in the API request: object deletion can be slow or transiently fail,
// and video_parts must not be removed until pending recording-webhook deliveries
// have frozen their part metadata.
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
	if err := s.repo.SetTaskNextRun(ctx, ManualDeletionTaskName); err != nil {
		// Queueing succeeded. A wakeup failure should not make the API caller
		// retry and potentially duplicate user-visible work; the interval task
		// will still pick the row up.
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

// ProcessManualDeletes drains queued operator-requested deletions that are safe
// to finalize now. The repository query applies the same webhook frozen-parts
// guard as the retention sweep: rows with pending/delivering deliveries whose
// frozen_parts is still empty are left queued until the webhook dispatcher
// captures their part list.
func (s *Service) ProcessManualDeletes(ctx context.Context) (int, error) {
	videos, err := s.repo.ListVideosPendingManualDelete(ctx, manualDeleteBatchSize)
	if err != nil {
		return 0, fmt.Errorf("list manual deletes: %w", err)
	}
	var (
		deleted int
		errs    []error
	)
	for i := range videos {
		if err := s.DeleteRecording(ctx, &videos[i], repository.DeletionKindManual); err != nil {
			errs = append(errs, fmt.Errorf("delete recording %d: %w", videos[i].ID, err))
			continue
		}
		deleted++
	}
	return deleted, errors.Join(errs...)
}

// DeleteRecording removes one recording's bytes, then finalizes the DB cleanup
// in a transaction. Storage deletes run first and any failure aborts before the
// DB writes, leaving the recording selectable for the next sweep. Once every
// object is gone we tombstone the video and drop its part rows atomically so
// readers never observe a visible recording whose parts disappeared. kind
// records why the recording was removed (DeletionKindRetention for the sweep,
// DeletionKindManual for an operator delete); the purge itself is identical.
// Exported so the video API's manual-delete handler reuses this exact routine
// rather than reimplementing the object purge and stranding orphans.
//
// v is the already-loaded recording row: Sweep loads it per due id and the
// manual-delete handler passes the row it fetched for its precheck, so the purge
// never issues a redundant read.
func (s *Service) DeleteRecording(ctx context.Context, v *repository.Video, kind string) error {
	parts, err := s.repo.ListVideoParts(ctx, v.ID)
	if err != nil {
		return fmt.Errorf("list parts: %w", err)
	}
	if err := s.purgeObjects(ctx, v, parts); err != nil {
		return err
	}
	// FinalizeDelete soft-deletes the video row, so the
	// video_playback_assets ON DELETE CASCADE never fires. Drop the row
	// explicitly or a stale ready row would dangle past every retention pass.
	if err := s.repo.DeleteVideoPlaybackAsset(ctx, v.ID); err != nil {
		return fmt.Errorf("delete playback asset row: %w", err)
	}
	if err := s.repo.FinalizeDelete(ctx, v.ID, kind); err != nil {
		return fmt.Errorf("finalize db delete: %w", err)
	}
	s.log.Info("deleted recording",
		"video_id", v.ID, "broadcaster_id", v.BroadcasterID, "parts", len(parts), "kind", kind)
	return nil
}

// purgeObjects deletes every stored object a recording owns: each part's
// video file plus its thumbnail and sprite strip, the video-level thumbnail,
// the waveform artifact, and the live-snapshot JPEGs. storage.Delete is
// idempotent (a missing object is not an error), so a re-run after a partial
// pass is safe; a real I/O error stops the purge so the caller leaves the DB
// untouched.
func (s *Service) purgeObjects(ctx context.Context, v *repository.Video, parts []repository.VideoPart) error {
	if len(parts) == 0 {
		// Historical rows predate video_parts and store the media at
		// videos/<videos.filename>.mp4. Keep this in lockstep with stream.go's
		// zero-part fallback so read and retention paths agree on the legacy
		// shape. The legacy thumbnail, if present, is deleted below via the
		// stored videos.thumbnail key.
		p := storagekeys.Video(v.Filename + ".mp4")
		if err := s.store.Delete(ctx, p); err != nil {
			return fmt.Errorf("delete object %s: %w", p, err)
		}
	} else {
		for i := range parts {
			// Keys come from storagekeys, the same source the downloader writes
			// through (see downloader.finalizePart), so the thumbnail/strip names
			// can't drift out of sync with the writer and strand orphans.
			base := storagekeys.Base(parts[i].Filename)
			for _, p := range []string{
				storagekeys.Video(parts[i].Filename),
				storagekeys.Thumbnail(base),
				storagekeys.Strip(base),
			} {
				if err := s.store.Delete(ctx, p); err != nil {
					return fmt.Errorf("delete object %s: %w", p, err)
				}
			}
		}
	}
	if v.Thumbnail != nil {
		if err := s.store.Delete(ctx, *v.Thumbnail); err != nil {
			return fmt.Errorf("delete object %s: %w", *v.Thumbnail, err)
		}
	}
	// The playback-cache artifact is derived deterministically from the
	// recording's first part (storagekeys.PlaybackName is the shared authority),
	// so deleting it here keeps retention in lockstep with playbackcache without
	// consulting the asset row (which may be building/failed with a NULL filename
	// yet a stale file on disk). Only multi-part recordings ever get an artifact
	// (canCopyConcat requires >= 2 parts), so single-part rows are skipped.
	if len(parts) > 1 {
		artifact := storagekeys.PlaybackName(v.Filename, parts[0].Filename)
		if err := s.store.Delete(ctx, storagekeys.Video(artifact)); err != nil {
			return fmt.Errorf("delete object %s: %w", artifact, err)
		}
	}
	if err := s.store.Delete(ctx, storagekeys.Waveform(v.Filename)); err != nil {
		return fmt.Errorf("delete object %s: %w", storagekeys.Waveform(v.Filename), err)
	}
	return s.purgeSnapshots(ctx, v.Filename)
}

// purgeSnapshots deletes the live-snapshot JPEGs (<filename>-snapNN.jpg)
// written during recording. It probes index 0,1,2,... and stops at the first
// gap (snapshots are contiguous by construction), bounded by maxSnapshotProbe.
// Unlike the video API's ListSnapshots it does NOT cap at 500: retention must
// remove every snapshot a recording owns or it strands bytes on disk.
//
// Deletion runs highest-index-first so index 0 — the probe's sentinel —
// goes last. A crash mid-purge then leaves a contiguous 0..k prefix that
// the next sweep re-discovers, instead of a hole at index 0 that would
// break the probe and strand the tail forever.
func (s *Service) purgeSnapshots(ctx context.Context, filename string) error {
	var found []string
	for i := range maxSnapshotProbe {
		p := storagekeys.Snapshot(filename, i)
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
		// No gap in 100k probes: almost certainly a pathological store, not a
		// genuine 347-day recording. Delete what we found, but warn so the purge
		// is never silently incomplete.
		s.log.Warn("retention: snapshot purge hit probe ceiling; some snapshots may remain",
			"filename", filename, "ceiling", maxSnapshotProbe)
	}
	for i := len(found) - 1; i >= 0; i-- {
		if err := s.store.Delete(ctx, found[i]); err != nil {
			return fmt.Errorf("delete snapshot %s: %w", found[i], err)
		}
	}
	return nil
}
