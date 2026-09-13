package settings

import (
	"context"
	"errors"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
)

type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

func New(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log.With("domain", "settings")}
}

// Get returns saved settings, inserting database defaults only if none exist.
func (s *Service) Get(ctx context.Context, userID string) (*repository.Settings, error) {
	row, err := s.repo.GetSettings(ctx, userID)
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	return s.repo.EnsureSettings(ctx, userID)
}

// Update changes locale preferences without replacing playback preferences.
func (s *Service) Update(ctx context.Context, input *repository.Settings) (*repository.Settings, error) {
	return s.repo.UpsertSettings(ctx, input)
}

// UpdatePlayback changes resume thresholds without replacing locale preferences.
func (s *Service) UpdatePlayback(ctx context.Context, input *repository.Settings) (*repository.Settings, error) {
	return s.repo.UpdatePlaybackSettings(ctx, input)
}
