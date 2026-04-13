package sse

import (
	"log/slog"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes wires the SSE tRPC subscriptions. system.events and
// task.status are owner-level (operator telemetry); stream.live is
// viewer-level so any signed-in user can watch for channels going
// live.
func RegisterRoutes(tr *trpcgo.Router, bus *eventbus.Buses, log *slog.Logger, viewer, owner *trpcgo.ProcedureBuilder) {
	h := NewHandler(bus, log)
	trpcgo.MustVoidSubscribe(tr, "system.events", h.SystemEvents, owner)
	trpcgo.MustVoidSubscribe(tr, "task.status", h.TaskStatus, owner)
	trpcgo.MustVoidSubscribe(tr, "stream.live", h.StreamLive, viewer)
}
