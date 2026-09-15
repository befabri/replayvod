package pgadapter

import (
	"context"
	"fmt"

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
