// Package task is the tRPC-transport wrapper around taskservice.
package task

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/service/taskservice"
	"github.com/befabri/trpcgo"
)

type Service struct {
	svc *taskservice.Service
	log *slog.Logger
}

func NewService(svc *taskservice.Service, log *slog.Logger) *Service {
	return &Service{svc: svc, log: log.With("domain", "task-api")}
}

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

type ListResponse struct {
	Data []TaskResponse `json:"data"`
}

func (s *Service) List(ctx context.Context) (ListResponse, error) {
	rows, err := s.svc.List(ctx)
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

type ToggleInput struct {
	Name    string `json:"name" validate:"required,min=1"`
	Enabled bool   `json:"enabled"`
}

func (s *Service) Toggle(ctx context.Context, input ToggleInput) (TaskResponse, error) {
	row, err := s.svc.SetEnabled(ctx, input.Name, input.Enabled)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return TaskResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "task not found")
		}
		s.log.Error("toggle task", "name", input.Name, "error", err)
		return TaskResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to toggle task")
	}
	return toResponse(row), nil
}

type RunNowInput struct {
	Name string `json:"name" validate:"required,min=1"`
}

func (s *Service) RunNow(ctx context.Context, input RunNowInput) (TaskResponse, error) {
	row, err := s.svc.RunNow(ctx, input.Name)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return TaskResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "task not found")
		}
		s.log.Error("run task now", "name", input.Name, "error", err)
		return TaskResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to schedule run")
	}
	return toResponse(row), nil
}
