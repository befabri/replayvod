// Package task implements the task.* tRPC procedures. Owner-only —
// toggling a task or forcing an immediate run affects the whole system.
package task

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// Service handles tRPC task procedures.
type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

// NewService builds a task service.
func NewService(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With("domain", "task-api"),
	}
}

// TaskResponse is the wire shape for a scheduled task row. Includes
// runtime state so the dashboard can show "last run 5m ago, took
// 120ms, status success" without a secondary round-trip.
type TaskResponse struct {
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	IntervalSeconds int32      `json:"interval_seconds"`
	IsEnabled       bool       `json:"is_enabled"`
	LastRunAt       *time.Time `json:"last_run_at,omitempty"`
	LastDurationMs  int32      `json:"last_duration_ms"`
	LastStatus      string     `json:"last_status"`
	LastError       *string    `json:"last_error,omitempty"`
	NextRunAt       *time.Time `json:"next_run_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func toResponse(t *repository.Task) TaskResponse {
	return TaskResponse{
		Name:            t.Name,
		Description:     t.Description,
		IntervalSeconds: t.IntervalSeconds,
		IsEnabled:       t.IsEnabled,
		LastRunAt:       t.LastRunAt,
		LastDurationMs:  t.LastDurationMs,
		LastStatus:      t.LastStatus,
		LastError:       t.LastError,
		NextRunAt:       t.NextRunAt,
		CreatedAt:       t.CreatedAt,
		UpdatedAt:       t.UpdatedAt,
	}
}

// ListResponse is the wire shape for task.list.
type ListResponse struct {
	Data []TaskResponse `json:"data"`
}

// List returns every registered task, sorted by name. No pagination —
// task count is bounded by code (a handful of tasks).
func (s *Service) List(ctx context.Context) (ListResponse, error) {
	rows, err := s.repo.ListTasks(ctx)
	if err != nil {
		s.log.Error("list tasks", "error", err)
		return ListResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list tasks")
	}
	data := make([]TaskResponse, len(rows))
	for i := range rows {
		data[i] = toResponse(&rows[i])
	}
	return ListResponse{Data: data}, nil
}

// ToggleInput flips is_enabled on a registered task. Input validates
// name as a non-empty string; the server checks existence.
type ToggleInput struct {
	Name    string `json:"name" validate:"required,min=1"`
	Enabled bool   `json:"enabled"`
}

// Toggle pauses/resumes a task without requiring a restart. The
// scheduler's due query filters on is_enabled, so a paused task
// simply stops firing on its cadence.
func (s *Service) Toggle(ctx context.Context, input ToggleInput) (TaskResponse, error) {
	row, err := s.repo.SetTaskEnabled(ctx, input.Name, input.Enabled)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return TaskResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "task not found")
		}
		s.log.Error("toggle task", "name", input.Name, "error", err)
		return TaskResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to toggle task")
	}
	return toResponse(row), nil
}

// RunNowInput triggers an immediate run on the next scheduler tick.
type RunNowInput struct {
	Name string `json:"name" validate:"required,min=1"`
}

// RunNow sets next_run_at to NOW() so the scheduler picks the task up
// on its next poll (within ~15s). We don't synchronously invoke —
// running a task in the tRPC context would risk a 30-second timeout
// and tie up the response to the dashboard.
func (s *Service) RunNow(ctx context.Context, input RunNowInput) (TaskResponse, error) {
	// Ensure the task exists before scheduling an immediate run;
	// otherwise SetTaskNextRun silently updates zero rows.
	if _, err := s.repo.GetTask(ctx, input.Name); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return TaskResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "task not found")
		}
		return TaskResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load task")
	}
	if err := s.repo.SetTaskNextRun(ctx, input.Name); err != nil {
		s.log.Error("run task now", "name", input.Name, "error", err)
		return TaskResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to schedule run")
	}
	row, err := s.repo.GetTask(ctx, input.Name)
	if err != nil {
		s.log.Error("reload task after run-now", "name", input.Name, "error", err)
		return TaskResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load task")
	}
	return toResponse(row), nil
}
