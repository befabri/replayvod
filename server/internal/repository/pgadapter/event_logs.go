package pgadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) CreateEventLog(ctx context.Context, input *repository.EventLogInput) (*repository.EventLog, error) {
	row, err := a.queries.CreateEventLog(ctx, pggen.CreateEventLogParams{
		Domain:      input.Domain,
		EventType:   input.EventType,
		Severity:    input.Severity,
		Message:     input.Message,
		ActorUserID: input.ActorUserID,
		Data:        input.Data,
	})
	if err != nil {
		return nil, fmt.Errorf("pg create event log: %w", err)
	}
	return pgEventLogToDomain(row), nil
}

func (a *PGAdapter) ListEventLogs(ctx context.Context, limit, offset int) ([]repository.EventLog, error) {
	rows, err := a.queries.ListEventLogs(ctx, pggen.ListEventLogsParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list event logs: %w", err)
	}
	return pgEventLogsToDomain(rows), nil
}

func (a *PGAdapter) ListEventLogsByDomain(ctx context.Context, domain string, limit, offset int) ([]repository.EventLog, error) {
	rows, err := a.queries.ListEventLogsByDomain(ctx, pggen.ListEventLogsByDomainParams{
		Domain: domain,
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list event logs by domain: %w", err)
	}
	return pgEventLogsToDomain(rows), nil
}

func (a *PGAdapter) ListEventLogsBySeverity(ctx context.Context, severity string, limit, offset int) ([]repository.EventLog, error) {
	rows, err := a.queries.ListEventLogsBySeverity(ctx, pggen.ListEventLogsBySeverityParams{
		Severity: severity,
		Limit:    int32(limit),
		Offset:   int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list event logs by severity: %w", err)
	}
	return pgEventLogsToDomain(rows), nil
}

func (a *PGAdapter) CountEventLogs(ctx context.Context) (int64, error) {
	return a.queries.CountEventLogs(ctx)
}

func (a *PGAdapter) CountEventLogsByDomain(ctx context.Context, domain string) (int64, error) {
	return a.queries.CountEventLogsByDomain(ctx, domain)
}

func (a *PGAdapter) DeleteOldEventLogs(ctx context.Context, before time.Time) error {
	return a.queries.DeleteOldEventLogs(ctx, before)
}

func pgEventLogToDomain(e pggen.EventLog) *repository.EventLog {
	return &repository.EventLog{
		ID:          e.ID,
		Domain:      e.Domain,
		EventType:   e.EventType,
		Severity:    e.Severity,
		Message:     e.Message,
		ActorUserID: e.ActorUserID,
		Data:        e.Data,
		CreatedAt:   e.CreatedAt,
	}
}

func pgEventLogsToDomain(rows []pggen.EventLog) []repository.EventLog {
	out := make([]repository.EventLog, len(rows))
	for i, r := range rows {
		out[i] = *pgEventLogToDomain(r)
	}
	return out
}
