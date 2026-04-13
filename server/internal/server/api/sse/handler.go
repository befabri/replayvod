// Package sse hosts the SSE subscription procedures that fan out to
// the trpcgo subscription transport. Every procedure here subscribes
// to an eventbus Topic and relays events onto the returned channel
// until ctx is cancelled.
//
// These are tRPC subscription procedures, not Chi SSE endpoints —
// trpcgo handles the wire protocol and reconnection shape.
package sse

import (
	"context"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/eventbus"
)

// Handler wraps the bus for tRPC procedure signatures.
type Handler struct {
	bus *eventbus.Buses
	log *slog.Logger
}

// NewHandler builds an SSE handler bound to a topic set. bus may be
// nil; each Subscribe method then returns a pre-closed channel.
func NewHandler(bus *eventbus.Buses, log *slog.Logger) *Handler {
	return &Handler{
		bus: bus,
		log: log.With("domain", "sse"),
	}
}

// SystemEvents streams event_logs rows as they're written. Owner-only
// per router wiring; these surface app-level activity that's not meant
// for regular viewers.
func (h *Handler) SystemEvents(ctx context.Context) (<-chan eventbus.EventLogEvent, error) {
	if h.bus == nil {
		ch := make(chan eventbus.EventLogEvent)
		close(ch)
		return ch, nil
	}
	return h.bus.EventLogs.Subscribe(ctx), nil
}

// StreamLive streams channels-went-live notifications. Viewer-level:
// any authenticated user can see which followed channels are live;
// filtering by follow happens client-side.
func (h *Handler) StreamLive(ctx context.Context) (<-chan eventbus.StreamLiveEvent, error) {
	if h.bus == nil {
		ch := make(chan eventbus.StreamLiveEvent)
		close(ch)
		return ch, nil
	}
	return h.bus.StreamLive.Subscribe(ctx), nil
}

// TaskStatus streams scheduler task lifecycle transitions. Owner-only.
func (h *Handler) TaskStatus(ctx context.Context) (<-chan eventbus.TaskStatusEvent, error) {
	if h.bus == nil {
		ch := make(chan eventbus.TaskStatusEvent)
		close(ch)
		return ch, nil
	}
	return h.bus.TaskStatus.Subscribe(ctx), nil
}
