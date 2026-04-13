// Package task owns the scheduled-task operator surface (list, toggle,
// run-now). Execution itself is in internal/scheduler; this package is
// the operator-facing control plane.
package task

import (
	"context"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
)

// Service is the task operator-control service.
type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

// New builds the service.
func New(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log.With("domain", "task")}
}

// List returns every registered task ordered by name.
func (s *Service) List(ctx context.Context) ([]repository.Task, error) {
	return s.repo.ListTasks(ctx)
}

// SetEnabled pauses/resumes a task. Returns the reloaded row.
func (s *Service) SetEnabled(ctx context.Context, name string, enabled bool) (*repository.Task, error) {
	return s.repo.SetTaskEnabled(ctx, name, enabled)
}

// RunNow schedules a task to fire on the next scheduler tick
// (within ~15s). Non-blocking — we don't invoke synchronously
// because that would tie up the calling tRPC request for the
// task's full duration.
//
// Verifies the task exists up front so SetTaskNextRun doesn't
// silently update zero rows when an operator fat-fingers the name.
func (s *Service) RunNow(ctx context.Context, name string) (*repository.Task, error) {
	if _, err := s.repo.GetTask(ctx, name); err != nil {
		return nil, err
	}
	if err := s.repo.SetTaskNextRun(ctx, name); err != nil {
		return nil, err
	}
	return s.repo.GetTask(ctx, name)
}
