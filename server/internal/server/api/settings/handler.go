package settings

import (
	"context"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/trpcgo"
)

// Handler is the tRPC adapter for the settings domain.
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler wires a handler around a settings Service.
func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log.With("domain", "settings-api")}
}

type SettingsResponse struct {
	UserID         string    `json:"user_id"`
	Timezone       string    `json:"timezone"`
	DatetimeFormat string    `json:"datetime_format"`
	Language       string    `json:"language"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func toResponse(s *repository.Settings) SettingsResponse {
	return SettingsResponse{
		UserID:         s.UserID,
		Timezone:       s.Timezone,
		DatetimeFormat: s.DatetimeFormat,
		Language:       s.Language,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
	}
}

func (h *Handler) Get(ctx context.Context) (SettingsResponse, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return SettingsResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	row, err := h.svc.Get(ctx, user.ID)
	if err != nil {
		h.log.Error("get settings", "user_id", user.ID, "error", err)
		return SettingsResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load settings")
	}
	return toResponse(row), nil
}

type UpdateInput struct {
	Timezone       string `json:"timezone" validate:"required,min=1,max=64"`
	DatetimeFormat string `json:"datetime_format" validate:"required,oneof=ISO EU US"`
	Language       string `json:"language" validate:"required,oneof=en fr"`
}

func (h *Handler) Update(ctx context.Context, input UpdateInput) (SettingsResponse, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return SettingsResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	row, err := h.svc.Update(ctx, &repository.Settings{
		UserID:         user.ID,
		Timezone:       input.Timezone,
		DatetimeFormat: input.DatetimeFormat,
		Language:       input.Language,
	})
	if err != nil {
		h.log.Error("update settings", "user_id", user.ID, "error", err)
		return SettingsResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to update settings")
	}
	return toResponse(row), nil
}
