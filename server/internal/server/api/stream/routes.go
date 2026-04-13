package stream

import (
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes wires the stream.* tRPC reads. All are viewer-level.
func RegisterRoutes(tr *trpcgo.Router, repo repository.Repository, log *slog.Logger, viewer *trpcgo.ProcedureBuilder) {
	h := NewHandler(New(repo, log), log)
	trpcgo.MustVoidQuery(tr, "stream.active", h.Active, viewer)
	trpcgo.MustQuery(tr, "stream.byBroadcaster", h.ByBroadcaster, viewer)
	trpcgo.MustQuery(tr, "stream.lastLive", h.LastLive, viewer)
}
