package video

import (
	"log/slog"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/channel"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes uses viewer for library reads and per-user state, and admin
// for recording controls. store must be the shared media reader.
func RegisterRoutes(tr *trpcgo.Router, repo repository.Repository, dl *downloader.Service, tc *twitch.Client, hydrator *streammeta.Hydrator, deletion RecordingDeletionRequester, restorer RecordingRestorer, store storage.Reader, log *slog.Logger, viewer, admin *trpcgo.ProcedureBuilder) {
	archive := NewArchive(repo, dl, tc, channel.New(repo, tc, log), log)
	h := NewHandler(New(repo, log), NewDownload(repo, dl, tc, hydrator, log), archive, deletion, restorer, store, log)

	trpcgo.MustQuery(tr, "video.list", h.List, viewer)
	trpcgo.MustQuery(tr, "video.listPage", h.ListPage, viewer)
	trpcgo.MustQuery(tr, "video.search", h.Search, viewer)
	trpcgo.MustQuery(tr, "video.continueWatching", h.ContinueWatching, viewer)
	trpcgo.MustQuery(tr, "video.getById", h.GetByID, viewer)
	trpcgo.MustQuery(tr, "video.relatedRecordings", h.RelatedRecordings, viewer)
	trpcgo.MustQuery(tr, "video.titles", h.Titles, viewer)
	trpcgo.MustQuery(tr, "video.categories", h.Categories, viewer)
	trpcgo.MustQuery(tr, "video.timeline", h.Timeline, viewer)
	trpcgo.MustQuery(tr, "video.snapshots", h.Snapshots, viewer)
	trpcgo.MustQuery(tr, "video.byBroadcaster", h.ByBroadcaster, viewer)
	trpcgo.MustQuery(tr, "video.byCategory", h.ByCategory, viewer)
	trpcgo.MustVoidQuery(tr, "video.statistics", h.Statistics, viewer)
	trpcgo.MustQuery(tr, "video.statisticsByBroadcaster", h.StatisticsByBroadcaster, viewer)
	trpcgo.MustVoidQuery(tr, "video.historyCounts", h.HistoryCounts, viewer)
	trpcgo.MustVoidQuery(tr, "video.downloadCapacity", h.DownloadCapacity, viewer)
	trpcgo.MustVoidQuery(tr, "video.activeDownloads", h.ActiveDownloads, viewer)
	trpcgo.MustVoidSubscribe(tr, "video.activeDownloadsLive", h.ActiveDownloadsLive, viewer)
	trpcgo.MustQuery(tr, "video.liveRenditions", h.LiveRenditions, admin)
	trpcgo.MustMutation(tr, "video.triggerDownload", h.TriggerDownload, admin)
	trpcgo.MustMutation(tr, "video.cancel", h.Cancel, admin)
	trpcgo.MustMutation(tr, "video.delete", h.Delete, admin)
	trpcgo.MustMutation(tr, "video.restore", h.Restore, admin)
	trpcgo.MustMutation(tr, "video.setWatchLater", h.SetWatchLater, viewer)
	trpcgo.MustMutation(tr, "video.updateWatchProgress", h.UpdateWatchProgress, viewer)
	trpcgo.MustSubscribe(tr, "video.downloadProgress", h.DownloadProgress, admin)

	trpcgo.MustQuery(tr, "archive.listChannelVods", h.ListChannelVODs, admin)
	trpcgo.MustMutation(tr, "archive.enqueue", h.EnqueueArchive, admin)
	trpcgo.MustVoidQuery(tr, "archive.queue", h.ArchiveQueue, viewer)
	trpcgo.MustMutation(tr, "archive.dequeue", h.DequeueArchive, admin)
	trpcgo.MustMutation(tr, "archive.retry", h.RetryArchive, admin)
	trpcgo.MustMutation(tr, "archive.cancelRetry", h.CancelArchiveRetry, admin)
}
