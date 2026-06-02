package system

import (
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes wires system.* tRPC procedures. All owner-only.
func RegisterRoutes(tr *trpcgo.Router, repo repository.Repository, log *slog.Logger, owner *trpcgo.ProcedureBuilder) {
	h := NewHandler(New(repo, log), log)

	trpcgo.MustQuery(tr, "system.fetchLogs", h.FetchLogs, owner)
	trpcgo.MustVoidQuery(tr, "system.listUsers", h.ListUsers, owner)
	trpcgo.MustMutation(tr, "system.updateUserRole", h.UpdateUserRole, owner)
	trpcgo.MustVoidQuery(tr, "system.listWhitelist", h.ListWhitelist, owner)
	trpcgo.MustMutation(tr, "system.addWhitelist", h.AddWhitelist, owner)
	trpcgo.MustMutation(tr, "system.removeWhitelist", h.RemoveWhitelist, owner)
	trpcgo.MustVoidQuery(tr, "system.playbackCacheConfig", h.PlaybackCacheConfig, owner)
	trpcgo.MustMutation(tr, "system.updatePlaybackCacheConfig", h.UpdatePlaybackCacheConfig, owner)

	// Event logs — separate from ListEventLogs because the output shape
	// differs (ranked + rank field) and the UI handles them in distinct
	// tabs.
	trpcgo.MustQuery(tr, "system.eventLogs", h.EventLogs, owner)
	trpcgo.MustQuery(tr, "system.searchEventLogs", h.SearchEventLogs, owner)
}
