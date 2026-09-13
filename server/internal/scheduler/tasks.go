package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/service/archiveposter"
	"github.com/befabri/replayvod/server/internal/service/categoryart"
	"github.com/befabri/replayvod/server/internal/service/categorymeta"
	"github.com/befabri/replayvod/server/internal/service/eventsub"
	"github.com/befabri/replayvod/server/internal/service/retention"
	"github.com/befabri/replayvod/server/internal/service/storagescan"
)

const (
	taskEventSubReconcileChannels = "eventsub_reconcile_channels"
	taskEventSubSnapshot          = "eventsub_snapshot"
	taskCategoryArtSync           = "category_art_sync"
	taskCategoryMetadataSync      = "category_metadata_sync"
	TaskStorageScan               = "storage_scan"
	taskArchivePosters            = "archive_posters"
	taskPlaybackCacheReconcile    = "playback_cache_reconcile"
)

type StandardTaskDeps struct {
	EventSub               *eventsub.Service
	CategoryArt            *categoryart.Service
	CategoryMetadata       *categorymeta.Service
	Retention              *retention.Service
	StorageScan            *storagescan.Service
	ArchivePosters         *archiveposter.Service
	PlaybackCacheReconcile RunFunc
}

// BuildStandardTasks selects handlers from the configuration active at boot.
// Global disablement, zero intervals, and missing dependencies omit handlers;
// dashboard pauses are deliberately not consulted. Building performs no database
// writes and runs no jobs. Pass the result through NewRegistry before reconciling.
func BuildStandardTasks(cfg *config.Config, repo repository.Repository, deps StandardTaskDeps, log *slog.Logger) []Task {
	sc := cfg.App.Scheduler
	if !sc.Enabled {
		return nil
	}
	if !cfg.ServerMode.CreatesTwitchSubscriptions() {
		deps.EventSub = nil
	}
	var tasks []Task

	if m := sc.TokenCleanupIntervalMinutes; m > 0 {
		tasks = append(tasks, Task{
			Name:            "app_token_cleanup",
			Description:     "Delete expired Twitch app access tokens",
			IntervalSeconds: int64(m) * 60,
			Run: func(ctx context.Context) error {
				return repo.DeleteExpiredAppTokens(ctx)
			},
		})
	}

	if m := sc.SessionCleanupIntervalMinutes; m > 0 {
		tasks = append(tasks, Task{
			Name:            "session_cleanup",
			Description:     "Delete expired user sessions",
			IntervalSeconds: int64(m) * 60,
			Run: func(ctx context.Context) error {
				return repo.DeleteExpiredSessions(ctx)
			},
		})
	}

	if d := sc.FetchLogsRetentionDays; d > 0 {
		tasks = append(tasks, Task{
			Name:            "fetch_logs_retention",
			Description:     fmt.Sprintf("Delete fetch_logs older than %d day(s)", d),
			IntervalSeconds: 24 * 60 * 60, // daily
			Run: func(ctx context.Context) error {
				return repo.DeleteOldFetchLogs(ctx, retentionCutoff(time.Now(), d))
			},
		})
	}

	if d := sc.WebhookEventPayloadRetentionDays; d > 0 {
		tasks = append(tasks, Task{
			Name:            "webhook_payload_trim",
			Description:     fmt.Sprintf("Null payload on webhook_events older than %d day(s)", d),
			IntervalSeconds: 24 * 60 * 60, // daily
			Run: func(ctx context.Context) error {
				return repo.ClearWebhookEventPayload(ctx, retentionCutoff(time.Now(), d))
			},
		})
	}

	if d := sc.EventLogsRetentionDays; d > 0 {
		tasks = append(tasks, Task{
			Name:            "event_logs_retention",
			Description:     fmt.Sprintf("Delete debug/info event_logs older than %d day(s)", d),
			IntervalSeconds: 24 * 60 * 60,
			Run: func(ctx context.Context) error {
				return repo.DeleteOldEventLogs(ctx, retentionCutoff(time.Now(), d))
			},
		})
	}

	if d := sc.RecordingWebhookDeliveryRetentionDays; d > 0 {
		tasks = append(tasks, Task{
			Name:            "recording_webhook_deliveries_retention",
			Description:     fmt.Sprintf("Delete terminal recording-webhook deliveries older than %d day(s)", d),
			IntervalSeconds: 24 * 60 * 60,
			Run: func(ctx context.Context) error {
				return repo.DeleteOldRecordingWebhookDeliveries(ctx, retentionCutoff(time.Now(), d))
			},
		})
	}

	if deps.EventSub != nil && sc.EventsubReconcileIntervalMinutes > 0 {
		tasks = append(tasks, Task{
			Name: taskEventSubReconcileChannels,
			Description: "Ensure stream.online/stream.offline subs exist for every local " +
				"channel; delete orphans + zombie subs. Keeps the SSE live-dot feed authoritative.",
			IntervalSeconds: int64(sc.EventsubReconcileIntervalMinutes) * 60,
			Run: func(ctx context.Context) error {
				channels, err := repo.ListChannels(ctx)
				if err != nil {
					return fmt.Errorf("list channels: %w", err)
				}
				ids := make(map[string]bool, len(channels))
				for _, ch := range channels {
					ids[ch.BroadcasterID] = true
				}
				return deps.EventSub.ReconcileChannelSubs(ctx, ids)
			},
		})
	}
	if deps.EventSub != nil && sc.EventsubIntervalMinutes > 0 {
		tasks = append(tasks, Task{
			Name:            taskEventSubSnapshot,
			Description:     "Poll Twitch EventSub subscriptions + record quota snapshot",
			IntervalSeconds: int64(sc.EventsubIntervalMinutes) * 60,
			Run: func(ctx context.Context) error {
				_, err := deps.EventSub.Snapshot(ctx)
				return err
			},
		})
	}

	if deps.CategoryArt != nil {
		if m := sc.CategoryArtIntervalMinutes; m > 0 {
			tasks = append(tasks, Task{
				Name:            taskCategoryArtSync,
				Description:     "Fetch box_art_url and igdb_id for categories the Hydrator couldn't fill eagerly",
				IntervalSeconds: int64(m) * 60,
				Run: func(ctx context.Context) error {
					result, err := deps.CategoryArt.SyncMissing(ctx)
					if result.Updated > 0 {
						log.Info("category art sync: filled metadata rows", "count", result.Updated)
						if result.IGDBUpdated > 0 && deps.CategoryMetadata != nil && sc.CategoryMetadataIntervalMinutes > 0 {
							if err := repo.SetTaskNextRun(ctx, taskCategoryMetadataSync); err != nil {
								return fmt.Errorf("queue category metadata sync: %w", err)
							}
							log.Info("category art sync: queued category metadata sync")
						}
					}
					return err
				},
			})
		}
	}

	if deps.CategoryMetadata != nil {
		if m := sc.CategoryMetadataIntervalMinutes; m > 0 {
			tasks = append(tasks, Task{
				Name:            taskCategoryMetadataSync,
				Description:     "Fetch IGDB descriptions for categories with igdb_id",
				IntervalSeconds: int64(m) * 60,
				Run: func(ctx context.Context) error {
					synced, err := deps.CategoryMetadata.SyncMissing(ctx)
					if synced > 0 {
						log.Info("category metadata sync: filled descriptions", "count", synced)
					}
					return err
				},
			})
		}
	}

	if deps.Retention != nil {
		tasks = append(tasks, Task{
			Name:            retention.ManualDeletionTaskName,
			Description:     retention.ManualDeletionTaskDescription,
			IntervalSeconds: retention.ManualDeletionIntervalSeconds,
			Run: func(ctx context.Context) error {
				deleted, err := deps.Retention.ProcessManualDeletes(ctx)
				if deleted > 0 {
					log.Info("manual recording deletion: deleted queued recordings", "count", deleted)
				}
				return err
			},
		})
		if m := sc.RecordingsRetentionIntervalMinutes; m > 0 {
			tasks = append(tasks, Task{
				Name:            "recordings_retention",
				Description:     "Delete recordings past their schedule's auto-delete window (is_delete_rediff)",
				IntervalSeconds: int64(m) * 60,
				Run: func(ctx context.Context) error {
					deleted, err := deps.Retention.Sweep(ctx, time.Now())
					if deleted > 0 {
						log.Info("recordings retention: deleted expired recordings", "count", deleted)
					}
					return err
				},
			})
		}
	}

	if deps.StorageScan != nil {
		if m := sc.StorageScanIntervalMinutes; m > 0 {
			tasks = append(tasks, Task{
				Name:            TaskStorageScan,
				Description:     "Tombstone recordings whose media files are gone from storage",
				IntervalSeconds: int64(m) * 60,
				Run: func(ctx context.Context) error {
					report, err := deps.StorageScan.Sweep(ctx)
					if report.Tombstoned > 0 || report.Partial > 0 {
						log.Info("storage scan: recordings with missing media",
							"scanned", report.Scanned, "tombstoned", report.Tombstoned, "partial", report.Partial)
					}
					if err == nil && !report.Complete {
						log.Info("storage scan: deadline reached; the next run resumes where this one stopped",
							"scanned", report.Scanned)
					}
					return err
				},
			})
		}
	}

	if deps.ArchivePosters != nil {
		if m := sc.ArchivePosterIntervalMinutes; m > 0 {
			tasks = append(tasks, Task{
				Name:            taskArchivePosters,
				Description:     "Fetch the Twitch poster of archives queued before Twitch had rendered one",
				IntervalSeconds: int64(m) * 60,
				Run: func(ctx context.Context) error {
					report, err := deps.ArchivePosters.Backfill(ctx)
					if report.Stored > 0 {
						log.Info("archive posters: stored posters", "count", report.Stored, "checked", report.Checked)
					}
					if err == nil && !report.Complete {
						log.Info("archive posters: backfill paused; the next run continues after the last archive attempted",
							"checked", report.Checked)
					}
					return err
				},
			})
		}
	}

	if deps.PlaybackCacheReconcile != nil {
		tasks = append(tasks, Task{
			Name:            taskPlaybackCacheReconcile,
			Description:     "Prune the playback-artifact cache to its size cap",
			IntervalSeconds: 5 * 60,
			Run:             deps.PlaybackCacheReconcile,
		})
	}
	return tasks
}

// retentionCutoff uses calendar days so month and year boundaries cannot move the cutoff forward.
func retentionCutoff(now time.Time, days int) time.Time {
	return now.AddDate(0, 0, -days)
}
