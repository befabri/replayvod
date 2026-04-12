// Package tag is the tRPC-transport wrapper for tag reads. Business
// logic lives in internal/service/tagservice.
package tag

import (
	"context"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/service/tagservice"
	"github.com/befabri/trpcgo"
)

type Service struct {
	svc *tagservice.Service
	log *slog.Logger
}

func NewService(svc *tagservice.Service, log *slog.Logger) *Service {
	return &Service{svc: svc, log: log.With("domain", "tag-api")}
}

// TagResponse is the wire shape for a tag row.
type TagResponse struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) List(ctx context.Context) ([]TagResponse, error) {
	rows, err := s.svc.List(ctx)
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
