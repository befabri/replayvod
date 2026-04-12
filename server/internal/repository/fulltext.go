package repository

import (
	"context"
	"time"
)

// FullTextSearcher is an optional capability a repository adapter MAY
// implement. Services that need full-text search type-assert against
// this interface; callers MUST have a sensible fallback when the
// assertion fails because only the Postgres adapter satisfies it (SQLite
// has no tsvector equivalent that's portable across modernc.org/sqlite
// and the stdlib driver).
//
// Usage:
//
//	if fts, ok := repo.(repository.FullTextSearcher); ok {
//	    results, total, err := fts.SearchEventLogs(ctx, q, limit, offset)
//	    ...
//	}
//	// fallback: ListEventLogs + LIKE or server-side filter
//
// Keep this interface narrow. Each new PG-only surface (LISTEN/NOTIFY
// feeds, GIN-backed JSON path queries, etc.) gets its own optional
// interface rather than being bundled in — that way adapters advertise
// exactly the capabilities they support.
type FullTextSearcher interface {
	// SearchEventLogs runs a full-text query over event_logs and returns
	// matching rows ordered by relevance. query is passed through
	// websearch_to_tsquery so operators can use natural syntax (quotes
	// for phrases, OR/AND, -exclusion).
	SearchEventLogs(ctx context.Context, query string, limit, offset int) ([]EventLogSearchResult, int64, error)
}

// EventLogSearchResult pairs an EventLog with its ts_rank score. Rows
// are returned newest-first within equal ranks so exact matches on
// recent activity float to the top.
type EventLogSearchResult struct {
	EventLog
	Rank float64
}

// EventLogSearchPaged is a convenience wrapper that mirrors the shape
// of ListEventLogs + CountEventLogs — service layer can assemble the
// same response envelope whether the result came from FTS or LIKE.
type EventLogSearchPaged struct {
	Results []EventLogSearchResult
	Total   int64
	// Ranked is true when the backing call was the full-text path.
	// Dashboards can highlight matches only when Ranked is true.
	Ranked bool
	// QueriedAt is the timestamp the search ran, useful if a caller
	// wants to show "as of" context for a long-lived tab.
	QueriedAt time.Time
}
