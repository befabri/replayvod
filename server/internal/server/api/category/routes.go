package category

import (
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes constructs the category service + handler and
// registers the category.* tRPC procedures. All category reads are
// viewer-level — tags and categories are operational metadata the
// whole UI depends on.
func RegisterRoutes(tr *trpcgo.Router, repo repository.Repository, log *slog.Logger, viewer *trpcgo.ProcedureBuilder) {
	h := NewHandler(New(repo, log), log)
	trpcgo.MustQuery(tr, "category.getById", h.GetByID, viewer)
	trpcgo.MustVoidQuery(tr, "category.list", h.List, viewer)
}
