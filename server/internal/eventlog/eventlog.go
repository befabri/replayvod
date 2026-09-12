// Package eventlog appends operator-visible rows to event_logs and mirrors
// them onto the SSE bus.
package eventlog

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
)

// Emit appends a structured row to event_logs and publishes it to the SSE bus.
// Errors are swallowed and logged: audit logging must not fail the caller. bus
// may be nil, in which case the row still lands in the database.
func Emit(ctx context.Context, repo repository.Repository, bus *eventbus.Buses, log *slog.Logger, domain, eventType, severity, message string, data any) {
	EmitBy(ctx, repo, bus, log, nil, domain, eventType, severity, message, data)
}

// EmitBy is Emit with the acting user recorded on the row.
func EmitBy(ctx context.Context, repo repository.Repository, bus *eventbus.Buses, log *slog.Logger, actorUserID *string, domain, eventType, severity, message string, data any) {
	var raw json.RawMessage
	var dataMap map[string]any
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			log.Warn("marshal event log data", "error", err)
		} else {
			raw = b
			// The SSE payload wants a JSON object; non-object payloads skip it.
			_ = json.Unmarshal(b, &dataMap)
		}
	}
	row, err := repo.CreateEventLog(ctx, &repository.EventLogInput{
		Domain:      domain,
		EventType:   eventType,
		Severity:    severity,
		Message:     message,
		ActorUserID: actorUserID,
		Data:        raw,
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
