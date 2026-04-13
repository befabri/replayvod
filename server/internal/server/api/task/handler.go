package task

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// Handler is the tRPC adapter for the task domain.
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler wires a handler around a task Service.
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log.With("domain", "task-api")}
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

func (h *Handler) List(ctx context.Context) (ListResponse, error) {
	rows, err := h.svc.List(ctx)
	if err != nil {
		h.log.Error("list tasks", "error", err)
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

func (h *Handler) Toggle(ctx context.Context, input ToggleInput) (TaskResponse, error) {
	row, err := h.svc.SetEnabled(ctx, input.Name, input.Enabled)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return TaskResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "task not found")
		}
		h.log.Error("toggle task", "name", input.Name, "error", err)
		return TaskResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to toggle task")
	}
	return toResponse(row), nil
}

type RunNowInput struct {
	Name string `json:"name" validate:"required,min=1"`
}

func (h *Handler) RunNow(ctx context.Context, input RunNowInput) (TaskResponse, error) {
	row, err := h.svc.RunNow(ctx, input.Name)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return TaskResponse{}, trpcgo.NewError(trpcgo.CodeNotFound, "task not found")
		}
		h.log.Error("run task now", "name", input.Name, "error", err)
		return TaskResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to schedule run")
	}
	return toResponse(row), nil
}
