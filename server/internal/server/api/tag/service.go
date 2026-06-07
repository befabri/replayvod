package tag

import (
	"context"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
)

type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

func New(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log.With("domain", "tag")}
}

func (s *Service) List(ctx context.Context) ([]repository.Tag, error) {
	return s.repo.ListTags(ctx)
}
