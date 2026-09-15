package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"

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
