package video

import (
	"log/slog"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes wires the video.* tRPC procedures. Reads are viewer-
// level; triggers/cancels/progress are admin-only so regular viewers
// can't burn Twitch/Helix quota. Only the SSE progress stream keeps
// admin gating at subscribe time — the trpcgo middleware chain closes
// the stream if the session expires mid-flight.
//
// store is used by the Snapshots endpoint to probe for the live-
// preview JPEGs saved during recording. Passed through so the handler
// can call Exists() at request time without holding the Storage on
// the domain Service (which is read-only by design).
func RegisterRoutes(tr *trpcgo.Router, repo repository.Repository, dl *downloader.Service, tc *twitch.Client, hydrator *streammeta.Hydrator, store storage.Storage, log *slog.Logger, viewer, admin *trpcgo.ProcedureBuilder) {
	h := NewHandler(New(repo, log), NewDownload(repo, dl, tc, hydrator, log), store, log)

	trpcgo.MustQuery(tr, "video.list", h.List, viewer)
	trpcgo.MustQuery(tr, "video.getById", h.GetByID, viewer)
	trpcgo.MustQuery(tr, "video.titles", h.Titles, viewer)
	trpcgo.MustQuery(tr, "video.categories", h.Categories, viewer)
	trpcgo.MustQuery(tr, "video.snapshots", h.Snapshots, viewer)
	trpcgo.MustQuery(tr, "video.byBroadcaster", h.ByBroadcaster, viewer)
	trpcgo.MustQuery(tr, "video.byCategory", h.ByCategory, viewer)
	trpcgo.MustVoidQuery(tr, "video.statistics", h.Statistics, viewer)
	trpcgo.MustMutation(tr, "video.triggerDownload", h.TriggerDownload, admin)
	trpcgo.MustMutation(tr, "video.cancel", h.Cancel, admin)
	trpcgo.MustSubscribe(tr, "video.downloadProgress", h.DownloadProgress, admin)
}
