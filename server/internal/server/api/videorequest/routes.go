package videorequest

import (
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes wires the videorequest.* tRPC procedures. Viewer-level
// — users manage only their own requests.
func RegisterRoutes(tr *trpcgo.Router, repo repository.Repository, log *slog.Logger, viewer *trpcgo.ProcedureBuilder) {
	h := NewHandler(New(repo, log), log)
	trpcgo.MustQuery(tr, "videorequest.mine", h.Mine, viewer)
	trpcgo.MustMutation(tr, "videorequest.request", h.Request, viewer)
}
