// Package tag implements tag.* tRPC procedures. Tags are name-keyed
// (text) with an auto int64 ID; the dashboard's schedule pickers need
// both to build a searchable multi-select.
package tag

import (
	"context"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// Service handles tRPC tag procedures.
type Service struct {
	repo repository.Repository
	log  *slog.Logger
}

// NewService builds a tag service.
func NewService(repo repository.Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With("domain", "tag"),
	}
}

// TagResponse is the wire shape for a tag row.
type TagResponse struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// List returns every tag, ordered by the repository's list query
// (name). Tag count is bounded by Twitch's real-world usage; no
// pagination needed for the forseeable future.
func (s *Service) List(ctx context.Context) ([]TagResponse, error) {
	rows, err := s.repo.ListTags(ctx)
	if err != nil {
		s.log.Error("list tags", "error", err)
		return nil, trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to list tags")
	}
	out := make([]TagResponse, len(rows))
	for i, r := range rows {
		out[i] = TagResponse{ID: r.ID, Name: r.Name, CreatedAt: r.CreatedAt}
	}
	return out, nil
}
