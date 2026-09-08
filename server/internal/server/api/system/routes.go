package system

import (
	"log/slog"

	"github.com/befabri/replayvod/server/internal/invite"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes registers admin account management and owner-only server
// settings.
func RegisterRoutes(tr *trpcgo.Router, repo repository.Repository, invites *invite.Service, log *slog.Logger, admin, owner *trpcgo.ProcedureBuilder) {
	h := NewHandler(New(repo, log), invites, log)

	trpcgo.MustVoidQuery(tr, "system.listUsers", h.ListUsers, admin)
	trpcgo.MustMutation(tr, "system.updateUserRole", h.UpdateUserRole, admin)
	trpcgo.MustVoidQuery(tr, "system.listWhitelist", h.ListWhitelist, admin)
	trpcgo.MustMutation(tr, "system.addWhitelist", h.AddWhitelist, admin)
	trpcgo.MustMutation(tr, "system.removeWhitelist", h.RemoveWhitelist, admin)
	trpcgo.MustMutation(tr, "system.createInvite", h.CreateInvite, admin)
	trpcgo.MustVoidQuery(tr, "system.listInvites", h.ListInvites, admin)
	trpcgo.MustMutation(tr, "system.revokeInvite", h.RevokeInvite, admin)
	trpcgo.MustMutation(tr, "system.rotateInvite", h.RotateInvite, admin)

	trpcgo.MustQuery(tr, "system.fetchLogs", h.FetchLogs, owner)
	trpcgo.MustVoidQuery(tr, "system.playbackCacheConfig", h.PlaybackCacheConfig, owner)
	trpcgo.MustMutation(tr, "system.updatePlaybackCacheConfig", h.UpdatePlaybackCacheConfig, owner)

	// Event logs — separate from ListEventLogs because the output shape
	// differs (ranked + rank field) and the UI handles them in distinct
	// tabs.
	trpcgo.MustQuery(tr, "system.eventLogs", h.EventLogs, owner)
	trpcgo.MustQuery(tr, "system.searchEventLogs", h.SearchEventLogs, owner)
}
