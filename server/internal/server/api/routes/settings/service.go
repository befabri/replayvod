// Package settings implements the settings.* tRPC procedures. Each
// user reads and writes their own row — no cross-user access.
package settings

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/trpcgo"
)

// Service handles tRPC settings procedures.
type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

// NewService builds a settings service.
func NewService(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With("domain", "settings"),
	}
}

// Defaults for lazy-created rows. Match the column defaults in the
// migration; duplicated here so Get can return sensible values
// without a write round-trip on fresh accounts.
const (
	defaultTimezone       = "UTC"
	defaultDatetimeFormat = "ISO"
	defaultLanguage       = "en"
)

// SettingsResponse is the wire shape. created_at / updated_at tell
// the dashboard whether the row is lazy-initialized or user-set.
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

// Get returns the caller's settings row, lazy-creating defaults on
// first access. Keeps the UI code from branching on "no row" — every
// authenticated user always has a settings object.
func (s *Service) Get(ctx context.Context) (SettingsResponse, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return SettingsResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	row, err := s.repo.GetSettings(ctx, user.ID)
	if err == nil {
		return toResponse(row), nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		s.log.Error("get settings", "user_id", user.ID, "error", err)
		return SettingsResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to load settings")
	}
	created, err := s.repo.UpsertSettings(ctx, &repository.Settings{
		UserID:         user.ID,
		Timezone:       defaultTimezone,
		DatetimeFormat: defaultDatetimeFormat,
		Language:       defaultLanguage,
	})
	if err != nil {
		s.log.Error("lazy-create settings", "user_id", user.ID, "error", err)
		return SettingsResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to create settings")
	}
	return toResponse(created), nil
}

// UpdateInput is the write payload. Datetime format and language are
// closed enums on the UI side; validation here mirrors that. Timezone
// is open-text (IANA names) — a full enum is impractical, so we rely
// on the client picker + graceful fallback to UTC when invalid.
type UpdateInput struct {
	Timezone       string `json:"timezone" validate:"required,min=1,max=64"`
	DatetimeFormat string `json:"datetime_format" validate:"required,oneof=ISO EU US"`
	Language       string `json:"language" validate:"required,oneof=en fr"`
}

// Update writes the caller's preferences.
func (s *Service) Update(ctx context.Context, input UpdateInput) (SettingsResponse, error) {
	user := middleware.GetUser(ctx)
	if user == nil {
		return SettingsResponse{}, trpcgo.NewError(trpcgo.CodeUnauthorized, "not authenticated")
	}
	row, err := s.repo.UpsertSettings(ctx, &repository.Settings{
		UserID:         user.ID,
		Timezone:       input.Timezone,
		DatetimeFormat: input.DatetimeFormat,
		Language:       input.Language,
	})
	if err != nil {
		s.log.Error("update settings", "user_id", user.ID, "error", err)
		return SettingsResponse{}, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to update settings")
	}
	return toResponse(row), nil
}
