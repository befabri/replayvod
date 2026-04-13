package video

import (
	"log/slog"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes wires the video.* tRPC procedures. Reads are viewer-
// level; triggers/cancels/progress are admin-only so regular viewers
// can't burn Twitch/Helix quota. Only the SSE progress stream keeps
// admin gating at subscribe time — the trpcgo middleware chain closes
// the stream if the session expires mid-flight.
func RegisterRoutes(tr *trpcgo.Router, repo repository.Repository, dl *downloader.Service, tc *twitch.Client, log *slog.Logger, viewer, admin *trpcgo.ProcedureBuilder) {
	h := NewHandler(New(repo, log), NewDownload(repo, dl, tc, log), log)

	trpcgo.MustQuery(tr, "video.list", h.List, viewer)
	trpcgo.MustQuery(tr, "video.getById", h.GetByID, viewer)
	trpcgo.MustQuery(tr, "video.byBroadcaster", h.ByBroadcaster, viewer)
	trpcgo.MustQuery(tr, "video.byCategory", h.ByCategory, viewer)
	trpcgo.MustVoidQuery(tr, "video.statistics", h.Statistics, viewer)
	trpcgo.MustMutation(tr, "video.triggerDownload", h.TriggerDownload, admin)
	trpcgo.MustMutation(tr, "video.cancel", h.Cancel, admin)
	trpcgo.MustSubscribe(tr, "video.downloadProgress", h.DownloadProgress, admin)
}
