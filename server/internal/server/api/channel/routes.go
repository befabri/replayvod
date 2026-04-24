package channel

import (
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes wires channel.* tRPC procedures. Reads are viewer-
// level; syncFromTwitch is owner-only because it burns Helix quota.
func RegisterRoutes(tr *trpcgo.Router, repo repository.Repository, tc *twitch.Client, log *slog.Logger, viewer, owner *trpcgo.ProcedureBuilder) {
	h := NewHandler(New(repo, tc, log), log)
	trpcgo.MustQuery(tr, "channel.getById", h.GetByID, viewer)
	trpcgo.MustQuery(tr, "channel.getByLogin", h.GetByLogin, viewer)
	trpcgo.MustVoidQuery(tr, "channel.list", h.List, viewer)
	trpcgo.MustQuery(tr, "channel.listPage", h.ListPage, viewer)
	trpcgo.MustVoidQuery(tr, "channel.listFollowed", h.ListFollowed, viewer)
	trpcgo.MustQuery(tr, "channel.search", h.Search, viewer)
	trpcgo.MustQuery(tr, "channel.latestLive", h.LatestLive, viewer)
	trpcgo.MustMutation(tr, "channel.syncFromTwitch", h.SyncFromTwitch, owner)
}
