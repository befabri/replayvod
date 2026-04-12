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

