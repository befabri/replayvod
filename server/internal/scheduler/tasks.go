package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/service/categoryart"
	"github.com/befabri/replayvod/server/internal/service/eventsub"
	"github.com/befabri/replayvod/server/internal/service/retention"
)

const (
	taskEventSubReconcileChannels = "eventsub_reconcile_channels"
	taskEventSubSnapshot          = "eventsub_snapshot"
)

// RegisterStandardTasks wires the default scheduled jobs against a scheduler
// Service. Each task reads its interval from cfg.App.Scheduler; a task whose
// interval is zero (or whose backing service is unavailable) is simply left
// unregistered.
//
// The EventSub tasks are the exception. Their delivery is toggled at runtime
// from the dashboard, so when it is off they are still persisted with a zero
// interval (registerDisabledTask) instead of being left unregistered. That
// neutralizes a stale row from an older direct/relay config — a zero-interval
// row is never "due" (ListDueTasks filters interval_seconds > 0) — so tick()
// stops warning, every poll, about a due task with no runner. The other tasks
// are config-only and do not get this treatment.
//
// artsvc is optional: when set, the category-art backfill task runs
// on the configured interval; when nil the eager Hydrator path is the
// only filler for box art.
//
// retentionsvc is optional in the same way: when set (a storage backend is
// up) the per-schedule recordings auto-delete task runs on the configured
// interval; when nil that task is left unregistered.
func RegisterStandardTasks(s *Service, cfg *config.Config, repo repository.Repository, esvc *eventsub.Service, artsvc *categoryart.Service, retentionsvc *retention.Service, log *slog.Logger) error {
	sc := cfg.App.Scheduler

	if m := sc.TokenCleanupIntervalMinutes; m > 0 {
		if err := s.Register(Task{
			Name:            "app_token_cleanup",
			Description:     "Delete expired Twitch app access tokens",
			IntervalSeconds: int64(m) * 60,
			Run: func(ctx context.Context) error {
				return repo.DeleteExpiredAppTokens(ctx)
			},
		}); err != nil {
			return err
		}
	}

	if m := sc.SessionCleanupIntervalMinutes; m > 0 {
		if err := s.Register(Task{
			Name:            "session_cleanup",
			Description:     "Delete expired user sessions",
			IntervalSeconds: int64(m) * 60,
			Run: func(ctx context.Context) error {
				return repo.DeleteExpiredSessions(ctx)
			},
		}); err != nil {
			return err
		}
	}

	if d := sc.FetchLogsRetentionDays; d > 0 {
		if err := s.Register(Task{
			Name:            "fetch_logs_retention",
			Description:     fmt.Sprintf("Delete fetch_logs older than %d day(s)", d),
			IntervalSeconds: 24 * 60 * 60, // daily
			Run: func(ctx context.Context) error {
				return repo.DeleteOldFetchLogs(ctx, retentionCutoff(time.Now(), d))
			},
		}); err != nil {
			return err
		}
	}

	if d := sc.WebhookEventPayloadRetentionDays; d > 0 {
		if err := s.Register(Task{
			Name:            "webhook_payload_trim",
			Description:     fmt.Sprintf("Null payload on webhook_events older than %d day(s)", d),
			IntervalSeconds: 24 * 60 * 60, // daily
			Run: func(ctx context.Context) error {
				return repo.ClearWebhookEventPayload(ctx, retentionCutoff(time.Now(), d))
			},
		}); err != nil {
			return err
		}
	}

	if d := sc.EventLogsRetentionDays; d > 0 {
		if err := s.Register(Task{
			Name:            "event_logs_retention",
			Description:     fmt.Sprintf("Delete debug/info event_logs older than %d day(s)", d),
			IntervalSeconds: 24 * 60 * 60,
			Run: func(ctx context.Context) error {
				return repo.DeleteOldEventLogs(ctx, retentionCutoff(time.Now(), d))
			},
		}); err != nil {
			return err
		}
	}

	if d := sc.RecordingWebhookDeliveryRetentionDays; d > 0 {
		if err := s.Register(Task{
			Name:            "recording_webhook_deliveries_retention",
			Description:     fmt.Sprintf("Delete terminal recording-webhook deliveries older than %d day(s)", d),
			IntervalSeconds: 24 * 60 * 60,
			Run: func(ctx context.Context) error {
				return repo.DeleteOldRecordingWebhookDeliveries(ctx, retentionCutoff(time.Now(), d))
			},
		}); err != nil {
			return err
		}
	}

	if esvc != nil && sc.EventsubReconcileIntervalMinutes > 0 {
		if err := s.Register(Task{
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
				return esvc.ReconcileChannelSubs(ctx, ids)
			},
		}); err != nil {
			return err
		}
	} else if err := registerDisabledTask(s, taskEventSubReconcileChannels, "EventSub channel subscription reconcile disabled"); err != nil {
		return err
	}
	if esvc != nil && sc.EventsubIntervalMinutes > 0 {
		if err := s.Register(Task{
			Name:            taskEventSubSnapshot,
			Description:     "Poll Twitch EventSub subscriptions + record quota snapshot",
			IntervalSeconds: int64(sc.EventsubIntervalMinutes) * 60,
			Run: func(ctx context.Context) error {
				_, err := esvc.Snapshot(ctx)
				return err
			},
		}); err != nil {
			return err
		}
	} else if err := registerDisabledTask(s, taskEventSubSnapshot, "EventSub quota snapshot disabled"); err != nil {
		return err
	}

	if artsvc != nil {
		if m := sc.CategoryArtIntervalMinutes; m > 0 {
			if err := s.Register(Task{
				Name:            "category_art_sync",
				Description:     "Fetch box_art_url for categories the Hydrator couldn't fill eagerly",
				IntervalSeconds: int64(m) * 60,
				Run: func(ctx context.Context) error {
					synced, err := artsvc.SyncMissing(ctx)
					if synced > 0 {
						log.Info("category art sync: filled rows", "count", synced)
					}
					return err
				},
			}); err != nil {
				return err
			}
		}
	}

	if retentionsvc != nil {
		if m := sc.RecordingsRetentionIntervalMinutes; m > 0 {
			if err := s.Register(Task{
				Name:            "recordings_retention",
				Description:     "Delete recordings past their schedule's auto-delete window (is_delete_rediff)",
				IntervalSeconds: int64(m) * 60,
				Run: func(ctx context.Context) error {
					deleted, err := retentionsvc.Sweep(ctx, time.Now())
					if deleted > 0 {
						log.Info("recordings retention: deleted expired recordings", "count", deleted)
					}
					return err
				},
			}); err != nil {
				return err
			}
		}
	}

	log.Info("scheduler standard tasks registered")
	return nil
}

// retentionCutoff returns the instant `days` days before now; every daily
// retention task deletes or trims rows older than it. Pulled out of the task
// closures so the day-subtraction is asserted once (TestRetentionCutoff)
// instead of re-derived inline in each, and so the closures hand their clock to
// a tested unit the same way recordings_retention hands time.Now() to Sweep. A
// flipped sign (a future cutoff that purges live rows) or a wrong unit would be
// a quiet data-loss bug, so the arithmetic earns its own test.
func retentionCutoff(now time.Time, days int) time.Time {
	return now.AddDate(0, 0, -days)
}

func registerDisabledTask(s *Service, name, description string) error {
	// Keep the row registered but unscheduled. UpsertTask preserves is_enabled,
	// so an operator's dashboard pause/resume choice is not overwritten.
	return s.Register(Task{
		Name:            name,
		Description:     description,
		IntervalSeconds: 0,
		Run: func(context.Context) error {
			return nil
		},
	})
}

// EmitEventLog is a convenience for tasks to append a structured row
// to event_logs and publish to the SSE bus. Swallows errors (audit
// logging must not fail the caller) and logs them. bus may be nil —
// the row still lands in the DB.
func EmitEventLog(ctx context.Context, repo repository.Repository, bus *eventbus.Buses, log *slog.Logger, domain, eventType, severity, message string, data any) {
	var raw json.RawMessage
	var dataMap map[string]any
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			log.Warn("marshal event log data", "error", err)
		} else {
			raw = b
			// For the SSE payload we want a map so the client sees a
			// proper JSON object; re-unmarshal into a map so we don't
			// leak a Go-specific shape. Non-object payloads skip the
			// bus event (rare enough not to matter).
			_ = json.Unmarshal(b, &dataMap)
		}
	}
	row, err := repo.CreateEventLog(ctx, &repository.EventLogInput{
		Domain:    domain,
		EventType: eventType,
		Severity:  severity,
		Message:   message,
		Data:      raw,
	})
	if err != nil {
		log.Warn("append event log", "domain", domain, "type", eventType, "error", err)
		return
	}
	if bus != nil {
		bus.EventLogs.Publish(eventbus.EventLogEvent{
			ID:          row.ID,
			Domain:      row.Domain,
			EventType:   row.EventType,
			Severity:    row.Severity,
			Message:     row.Message,
			ActorUserID: row.ActorUserID,
			Data:        dataMap,
			CreatedAt:   row.CreatedAt,
		})
	}
}
