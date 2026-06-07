package settings

import (
	"context"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/apierr"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
)

type Handler struct {
	svc *Service
	log *slog.Logger
}

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
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return SettingsResponse{}, err
	}
	row, err := h.svc.Get(ctx, user.ID)
	if err != nil {
		return SettingsResponse{}, apierr.Map(h.log, err, "load settings")
	}
	return toResponse(row), nil
}

type UpdateInput struct {
	Timezone       string `json:"timezone" validate:"required,min=1,max=64"`
	DatetimeFormat string `json:"datetime_format" validate:"required,oneof=ISO EU US"`
	Language       string `json:"language" validate:"required,oneof=en fr"`
}

func (h *Handler) Update(ctx context.Context, input UpdateInput) (SettingsResponse, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return SettingsResponse{}, err
	}
	row, err := h.svc.Update(ctx, &repository.Settings{
		UserID:         user.ID,
		Timezone:       input.Timezone,
		DatetimeFormat: input.DatetimeFormat,
		Language:       input.Language,
	})
	if err != nil {
		return SettingsResponse{}, apierr.Map(h.log, err, "update settings")
	}
	return toResponse(row), nil
}
