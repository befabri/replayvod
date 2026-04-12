package sqliteadapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) CreateEventLog(ctx context.Context, input *repository.EventLogInput) (*repository.EventLog, error) {
	var data sql.NullString
	if len(input.Data) > 0 {
		data = sql.NullString{String: string(input.Data), Valid: true}
	}
	row, err := a.queries.CreateEventLog(ctx, sqlitegen.CreateEventLogParams{
		Domain:      input.Domain,
		EventType:   input.EventType,
		Severity:    input.Severity,
		Message:     input.Message,
		ActorUserID: stringPtrToNullString(input.ActorUserID),
		Data:        data,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite create event log: %w", err)
	}
	return sqliteEventLogToDomain(row), nil
}

func (a *SQLiteAdapter) ListEventLogs(ctx context.Context, limit, offset int) ([]repository.EventLog, error) {
	rows, err := a.queries.ListEventLogs(ctx, sqlitegen.ListEventLogsParams{
		Limit:  int64(limit),
		Offset: int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list event logs: %w", err)
	}
	return sqliteEventLogsToDomain(rows), nil
}

func (a *SQLiteAdapter) ListEventLogsByDomain(ctx context.Context, domain string, limit, offset int) ([]repository.EventLog, error) {
	rows, err := a.queries.ListEventLogsByDomain(ctx, sqlitegen.ListEventLogsByDomainParams{
		Domain: domain,
		Limit:  int64(limit),
		Offset: int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list event logs by domain: %w", err)
	}
	return sqliteEventLogsToDomain(rows), nil
}

func (a *SQLiteAdapter) ListEventLogsBySeverity(ctx context.Context, severity string, limit, offset int) ([]repository.EventLog, error) {
	rows, err := a.queries.ListEventLogsBySeverity(ctx, sqlitegen.ListEventLogsBySeverityParams{
		Severity: severity,
		Limit:    int64(limit),
		Offset:   int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list event logs by severity: %w", err)
	}
	return sqliteEventLogsToDomain(rows), nil
}

func (a *SQLiteAdapter) CountEventLogs(ctx context.Context) (int64, error) {
	return a.queries.CountEventLogs(ctx)
}

func (a *SQLiteAdapter) CountEventLogsByDomain(ctx context.Context, domain string) (int64, error) {
	return a.queries.CountEventLogsByDomain(ctx, domain)
}

func (a *SQLiteAdapter) DeleteOldEventLogs(ctx context.Context, before time.Time) error {
	return a.queries.DeleteOldEventLogs(ctx, formatTime(before))
}

func sqliteEventLogToDomain(e sqlitegen.EventLog) *repository.EventLog {
	var data json.RawMessage
	if e.Data.Valid {
		data = json.RawMessage(e.Data.String)
	}
	return &repository.EventLog{
		ID:          e.ID,
		Domain:      e.Domain,
		EventType:   e.EventType,
		Severity:    e.Severity,
		Message:     e.Message,
		ActorUserID: fromNullString(e.ActorUserID),
		Data:        data,
		CreatedAt:   parseTime(e.CreatedAt),
	}
}

func sqliteEventLogsToDomain(rows []sqlitegen.EventLog) []repository.EventLog {
	out := make([]repository.EventLog, len(rows))
	for i, r := range rows {
		out[i] = *sqliteEventLogToDomain(r)
	}
	return out
}
