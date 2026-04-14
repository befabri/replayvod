package stream

import (
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes wires the stream.* tRPC reads. All are viewer-level.
// stream.followed hits Helix (user:read:follows), the rest read from
// the local streams table.
func RegisterRoutes(tr *trpcgo.Router, repo repository.Repository, tc *twitch.Client, log *slog.Logger, viewer *trpcgo.ProcedureBuilder) {
	h := NewHandler(New(repo, tc, log), log)
	trpcgo.MustVoidQuery(tr, "stream.active", h.Active, viewer)
	trpcgo.MustQuery(tr, "stream.byBroadcaster", h.ByBroadcaster, viewer)
	trpcgo.MustQuery(tr, "stream.lastLive", h.LastLive, viewer)
	trpcgo.MustVoidQuery(tr, "stream.followed", h.Followed, viewer)
	trpcgo.MustVoidQuery(tr, "stream.liveIds", h.LiveIds, viewer)
}
