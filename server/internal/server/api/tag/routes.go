package tag

import (
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes constructs the tag service + handler and registers
// the tag.* tRPC procedures. Tags are viewer-level reads — created by
// the stream enrichment path, not user input.
func RegisterRoutes(tr *trpcgo.Router, repo repository.Repository, log *slog.Logger, viewer *trpcgo.ProcedureBuilder) {
	h := NewHandler(New(repo, log), log)
	trpcgo.MustVoidQuery(tr, "tag.list", h.List, viewer)
}
