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
	"github.com/befabri/replayvod/server/internal/service/eventsub"
)

// RegisterStandardTasks wires the default scheduled jobs against a
// scheduler Service. Each task reads its interval from
// cfg.App.Scheduler; a zero interval skips registration so operators
// can disable a task by zeroing the value (distinct from is_enabled,
// which persists across the DB via the dashboard toggle).
func RegisterStandardTasks(s *Service, cfg *config.Config, repo repository.Repository, esvc *eventsub.Service, log *slog.Logger) error {
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
				cutoff := time.Now().AddDate(0, 0, -d)
				return repo.DeleteOldFetchLogs(ctx, cutoff)
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
				cutoff := time.Now().AddDate(0, 0, -d)
				return repo.ClearWebhookEventPayload(ctx, cutoff)
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
				cutoff := time.Now().AddDate(0, 0, -d)
				return repo.DeleteOldEventLogs(ctx, cutoff)
			},
		}); err != nil {
			return err
		}
	}

	if esvc != nil {
		if m := sc.EventsubIntervalMinutes; m > 0 {
			if err := s.Register(Task{
				Name:            "eventsub_snapshot",
				Description:     "Poll Twitch EventSub subscriptions + record quota snapshot",
				IntervalSeconds: int64(m) * 60,
				Run: func(ctx context.Context) error {
					_, err := esvc.Snapshot(ctx)
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
