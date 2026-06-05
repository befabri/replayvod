package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

// SearchEventLogs implements repository.FullTextSearcher for Postgres.
//
// Uses websearch_to_tsquery so operators get natural-language syntax:
//
//	"download failed" → phrase match
//	auth OR session  → boolean OR
//	-retry           → negation
//
// The query runs against event_logs.search_vector, a GENERATED tsvector
// column weighted A = message, B = event_type, C = domain (see
// migrations/postgres/025_event_logs_fts.up.sql). Results are ordered
// by ts_rank_cd desc, then created_at desc so ties break newest-first
// instead of by storage order. ts_rank_cd is the cover-density variant
// — slightly slower than ts_rank but values phrase proximity, which
// matches how operators actually scan log messages.
//
// The empty query is treated as "no rows" rather than "everything":
// full-text search with an empty query in PG is a runtime error, and
// an empty-input "match-all" would invite accidental unbounded scans
// from the UI. Callers that want the unfiltered list must go through
// ListEventLogs.
func (a *PGAdapter) SearchEventLogs(ctx context.Context, query string, limit, offset int) ([]repository.EventLogSearchResult, int64, error) {
	if query == "" {
		return nil, 0, nil
	}

	total, err := a.queries.CountSearchEventLogs(ctx, query)
	if err != nil {
		return nil, 0, fmt.Errorf("pg count event log search: %w", err)
	}
	if total == 0 {
		return nil, 0, nil
	}

	rows, err := a.queries.SearchEventLogs(ctx, pggen.SearchEventLogsParams{
		Query:     query,
		RowLimit:  int32(limit),
		RowOffset: int32(offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("pg search event logs: %w", err)
	}

	results := make([]repository.EventLogSearchResult, len(rows))
	for i, row := range rows {
		results[i] = pgEventLogSearchResultToDomain(row)
	}
	return results, total, nil
}

func pgEventLogSearchResultToDomain(row pggen.SearchEventLogsRow) repository.EventLogSearchResult {
	return repository.EventLogSearchResult{
		EventLog: repository.EventLog{
			ID:          row.ID,
			Domain:      row.Domain,
			EventType:   row.EventType,
			Severity:    row.Severity,
			Message:     row.Message,
			ActorUserID: row.ActorUserID,
			Data:        row.Data,
			CreatedAt:   row.CreatedAt,
		},
		Rank: float64(row.Rank),
	}
}

// compile-time guarantee that PGAdapter satisfies the optional
// full-text capability. Grep-visible so refactors that accidentally
// break the assertion fail at build time instead of at a type-switch
// in production.
var _ repository.FullTextSearcher = (*PGAdapter)(nil)
