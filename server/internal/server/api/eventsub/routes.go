package eventsub

import (
	"log/slog"

	"github.com/befabri/replayvod/server/internal/service/eventsub"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes wires eventsub.* tRPC procedures. All owner-only.
// The domain service is passed in rather than constructed here so the
// scheduler cron task shares the same instance.
func RegisterRoutes(tr *trpcgo.Router, svc *eventsub.Service, log *slog.Logger, owner *trpcgo.ProcedureBuilder) {
	h := NewHandler(svc, log)
	trpcgo.MustQuery(tr, "eventsub.listSubscriptions", h.ListSubscriptions, owner)
	trpcgo.MustQuery(tr, "eventsub.listSnapshots", h.ListSnapshots, owner)
	trpcgo.MustVoidQuery(tr, "eventsub.latestSnapshot", h.LatestSnapshot, owner)
	trpcgo.MustVoidMutation(tr, "eventsub.snapshot", h.Snapshot, owner)
	trpcgo.MustMutation(tr, "eventsub.subscribeStreamOnline", h.SubscribeStreamOnline, owner)
	trpcgo.MustMutation(tr, "eventsub.unsubscribe", h.Unsubscribe, owner)
}
