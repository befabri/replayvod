// Package settings owns per-user display preferences. Each user reads
// and writes their own row; no cross-user access.
package settings

import (
	"context"
	"errors"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
)

// Defaults for lazy-created rows. Match the column defaults in the
// migration; duplicated here so Get can return sensible values without
// a write round-trip on fresh accounts.
const (
	defaultTimezone       = "UTC"
	defaultDatetimeFormat = "ISO"
	defaultLanguage       = "en"
)

// Service is the settings domain service.
type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

// New builds the service.
func New(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log.With("domain", "settings")}
}

// Get returns the user's settings row, lazy-creating defaults on
// first access so the UI never has to branch on "no row."
func (s *Service) Get(ctx context.Context, userID string) (*repository.Settings, error) {
	row, err := s.repo.GetSettings(ctx, userID)
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	return s.repo.UpsertSettings(ctx, &repository.Settings{
		UserID:         userID,
		Timezone:       defaultTimezone,
		DatetimeFormat: defaultDatetimeFormat,
		Language:       defaultLanguage,
	})
}

// Update writes the caller's preferences.
func (s *Service) Update(ctx context.Context, input *repository.Settings) (*repository.Settings, error) {
	return s.repo.UpsertSettings(ctx, input)
}
