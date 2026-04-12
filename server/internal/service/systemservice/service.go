// Package systemservice owns owner-level admin business logic:
// user/role management, whitelist CRUD, fetch log reads, event log
// reads. Each method is a thin repo wrapper with the invariants the
// transport layer can't enforce on its own (e.g. "owner can't demote
// self").
//
// Transport-agnostic: no tRPC or HTTP types cross this boundary.
package systemservice

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/befabri/replayvod/server/internal/repository"
)

// ErrCannotDemoteSelf is returned when an owner attempts to assign
// themselves a non-owner role. Without this guard the system could
// get locked out of every owner-gated procedure.
var ErrCannotDemoteSelf = errors.New("systemservice: cannot demote yourself")

type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

func New(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log.With("domain", "system")}
}

// ListUsers returns every user.
func (s *Service) ListUsers(ctx context.Context) ([]repository.User, error) {
	return s.repo.ListUsers(ctx)
}

// UpdateUserRole assigns a role, returning the reloaded user row.
// callerID must be the currently-authenticated user's ID so the
// self-demotion guard can fire.
func (s *Service) UpdateUserRole(ctx context.Context, callerID, targetID, newRole string) (*repository.User, error) {
	if callerID == targetID && newRole != "owner" {
		return nil, ErrCannotDemoteSelf
	}
	if err := s.repo.UpdateUserRole(ctx, targetID, newRole); err != nil {
		return nil, err
	}
	return s.repo.GetUser(ctx, targetID)
}

// ListWhitelist returns every entry.
func (s *Service) ListWhitelist(ctx context.Context) ([]repository.WhitelistEntry, error) {
	return s.repo.ListWhitelist(ctx)
}

// AddToWhitelist is idempotent.
func (s *Service) AddToWhitelist(ctx context.Context, twitchUserID string) error {
	return s.repo.AddToWhitelist(ctx, twitchUserID)
}

// RemoveFromWhitelist is idempotent — missing entries return nil.
func (s *Service) RemoveFromWhitelist(ctx context.Context, twitchUserID string) error {
	return s.repo.RemoveFromWhitelist(ctx, twitchUserID)
}

// FetchLogsFilter carries the paginate-and-filter args for a fetch
// logs listing. FetchType empty means "all types."
type FetchLogsFilter struct {
	Limit     int
	Offset    int
	FetchType string
}

// ListFetchLogs returns a paginated list of Helix call records plus
// a total count (for the same filter scope). Service-layer method
// because the count-by-filter branching isn't purely repo-shaped.
func (s *Service) ListFetchLogs(ctx context.Context, f FetchLogsFilter) ([]repository.FetchLog, int64, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if f.FetchType != "" {
		logs, err := s.repo.ListFetchLogsByType(ctx, f.FetchType, limit, f.Offset)
		if err != nil {
			return nil, 0, err
		}
		total, err := s.repo.CountFetchLogsByType(ctx, f.FetchType)
		return logs, total, err
	}
	logs, err := s.repo.ListFetchLogs(ctx, limit, f.Offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.CountFetchLogs(ctx)
	return logs, total, err
}

// EventLogsFilter carries the paginate-and-filter args for event
// logs. Domain and Severity are mutually exclusive at the SQL level.
type EventLogsFilter struct {
	Limit    int
	Offset   int
	Domain   string
	Severity string
}

// ListEventLogs returns a paginated list of app-side event log rows.
// For the severity-only filter we return the overall count (a
// severity-scoped count query would bloat sqlc for a UI-only hint).
func (s *Service) ListEventLogs(ctx context.Context, f EventLogsFilter) ([]repository.EventLog, int64, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	switch {
	case f.Domain != "":
		rows, err := s.repo.ListEventLogsByDomain(ctx, f.Domain, limit, f.Offset)
		if err != nil {
			return nil, 0, err
		}
		total, err := s.repo.CountEventLogsByDomain(ctx, f.Domain)
		return rows, total, err
	case f.Severity != "":
		rows, err := s.repo.ListEventLogsBySeverity(ctx, f.Severity, limit, f.Offset)
		if err != nil {
			return nil, 0, err
		}
		total, err := s.repo.CountEventLogs(ctx)
		return rows, total, err
	default:
		rows, err := s.repo.ListEventLogs(ctx, limit, f.Offset)
		if err != nil {
			return nil, 0, err
		}
		total, err := s.repo.CountEventLogs(ctx)
		return rows, total, err
	}
}

// EventLogSearchResult pairs a matched event log with its ranking
// score. On backends without full-text support (SQLite), Rank is 0
// and rows are returned in the same order ListEventLogs would give.
type EventLogSearchResult struct {
	repository.EventLog
	Rank float64
}

// EventLogSearchResponse bundles the page + total count + a flag the
// UI uses to decide whether to show relevance highlighting.
type EventLogSearchResponse struct {
	Results []EventLogSearchResult
	Total   int64
	// Ranked is true when the backing query was full-text. When false,
	// the fallback path ran a substring LIKE scan and `Rank` is zero —
	// the dashboard should hide the relevance column in that case so
	// operators don't read meaning into identical-zero scores.
	Ranked bool
}

// SearchEventLogs runs a relevance-ordered search over event_logs. On
// Postgres this goes through the FullTextSearcher capability with
// websearch_to_tsquery semantics; on SQLite it falls back to a
// substring LIKE over message+event_type+domain so the dashboard still
// has something to show, just without ranking.
//
// Empty queries return an empty result rather than "all rows" — full
// listing goes through ListEventLogs.
func (s *Service) SearchEventLogs(ctx context.Context, query string, limit, offset int) (*EventLogSearchResponse, error) {
	if limit <= 0 {
		limit = 50
	}
	if query == "" {
		return &EventLogSearchResponse{}, nil
	}

	if fts, ok := s.repo.(repository.FullTextSearcher); ok {
		rows, total, err := fts.SearchEventLogs(ctx, query, limit, offset)
		if err != nil {
			return nil, err
		}
		out := make([]EventLogSearchResult, len(rows))
		for i, r := range rows {
			out[i] = EventLogSearchResult{EventLog: r.EventLog, Rank: r.Rank}
		}
		return &EventLogSearchResponse{Results: out, Total: total, Ranked: true}, nil
	}

	// Portable fallback: scan the full (retention-bounded) event_logs
	// table in memory, filter to substring matches across message +
	// event_type + domain, then apply the caller's offset/limit on the
	// filtered result. For the SQLite homelab case event_logs is small
	// (retention task prunes), so the simple path is cheaper than
	// maintaining a third adapter-level LIKE query.
	needle := strings.ToLower(query)
	matched, err := s.scanEventLogsSubstring(ctx, needle)
	if err != nil {
		return nil, err
	}

	total := int64(len(matched))
	start := min(offset, len(matched))
	end := min(start+limit, len(matched))
	return &EventLogSearchResponse{
		Results: matched[start:end],
		Total:   total,
		Ranked:  false,
	}, nil
}

// scanEventLogsSubstring walks event_logs in newest-first pages,
// keeping rows whose message/event_type/domain contain needle. The
// pagination loop is there so a future large table doesn't trip on a
// single 50-row ListEventLogs call — it keeps pulling until the repo
// reports a short read.
func (s *Service) scanEventLogsSubstring(ctx context.Context, needle string) ([]EventLogSearchResult, error) {
	const pageSize = 500
	var out []EventLogSearchResult
	for offset := 0; ; offset += pageSize {
		rows, err := s.repo.ListEventLogs(ctx, pageSize, offset)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if strings.Contains(strings.ToLower(r.Message), needle) ||
				strings.Contains(strings.ToLower(r.EventType), needle) ||
				strings.Contains(strings.ToLower(r.Domain), needle) {
				out = append(out, EventLogSearchResult{EventLog: r})
			}
		}
		if len(rows) < pageSize {
			return out, nil
		}
	}
}

