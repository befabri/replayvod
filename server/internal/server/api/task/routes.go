package task

import (
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes wires task.* tRPC procedures. Owner-only: toggling a
// task pauses it system-wide; run-now triggers an immediate run on
// the next scheduler tick (not synchronous — keeps the tRPC call
// fast).
func RegisterRoutes(tr *trpcgo.Router, repo repository.Repository, log *slog.Logger, owner *trpcgo.ProcedureBuilder) {
	h := NewHandler(New(repo, log), log)
	trpcgo.MustVoidQuery(tr, "task.list", h.List, owner)
	trpcgo.MustMutation(tr, "task.toggle", h.Toggle, owner)
	trpcgo.MustMutation(tr, "task.runNow", h.RunNow, owner)
}
