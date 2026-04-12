package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
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

	// Total count of matching rows, shared between the two queries so
	// the dashboard can paginate even when the page is smaller than the
	// result set. websearch_to_tsquery + @@ operator lets PG use the
	// GIN index for both count and select.
	var total int64
	if err := a.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM event_logs
		WHERE search_vector @@ websearch_to_tsquery('simple', $1)
	`, query).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("pg count event log search: %w", err)
	}
	if total == 0 {
		return nil, 0, nil
	}

	rows, err := a.db.Query(ctx, `
		SELECT
			id, domain, event_type, severity, message, actor_user_id, data, created_at,
			ts_rank_cd(search_vector, websearch_to_tsquery('simple', $1)) AS rank
		FROM event_logs
		WHERE search_vector @@ websearch_to_tsquery('simple', $1)
		ORDER BY rank DESC, created_at DESC
		LIMIT $2 OFFSET $3
	`, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("pg search event logs: %w", err)
	}
	defer rows.Close()

	results := make([]repository.EventLogSearchResult, 0, limit)
	for rows.Next() {
		var r repository.EventLogSearchResult
		if err := rows.Scan(
			&r.ID,
			&r.Domain,
			&r.EventType,
			&r.Severity,
			&r.Message,
			&r.ActorUserID,
			&r.Data,
			&r.CreatedAt,
			&r.Rank,
		); err != nil {
			return nil, 0, fmt.Errorf("pg scan event log search row: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("pg iterate event log search rows: %w", err)
	}
	return results, total, nil
}

// compile-time guarantee that PGAdapter satisfies the optional
// full-text capability. Grep-visible so refactors that accidentally
// break the assertion fail at build time instead of at a type-switch
// in production.
var _ repository.FullTextSearcher = (*PGAdapter)(nil)
