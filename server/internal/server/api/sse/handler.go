// Package sse exposes eventbus subscriptions through the trpcgo transport, which
// owns the wire protocol and reconnect acknowledgements.
package sse

import (
	"context"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/eventbus"
)

// Handler subscribes requests to process topics until their contexts end.
type Handler struct {
	bus *eventbus.Buses
	log *slog.Logger
}

// NewHandler accepts a nil bus, which produces closed subscription channels.
func NewHandler(bus *eventbus.Buses, log *slog.Logger) *Handler {
	return &Handler{
		bus: bus,
		log: log.With("domain", "sse"),
	}
}

// SystemEvents streams application activity and requires an owner-only route.
func (h *Handler) SystemEvents(ctx context.Context) (<-chan eventbus.EventLogEvent, error) {
	if h.bus == nil {
		ch := make(chan eventbus.EventLogEvent)
		close(ch)
		return ch, nil
	}
	return h.bus.EventLogs.Subscribe(ctx), nil
}

// StreamLive streams successful schedule triggers; clients filter by followed
// channels, and the route permits viewers.
func (h *Handler) StreamLive(ctx context.Context) (<-chan eventbus.StreamLiveEvent, error) {
	if h.bus == nil {
		ch := make(chan eventbus.StreamLiveEvent)
		close(ch)
		return ch, nil
	}
	return h.bus.StreamLive.Subscribe(ctx), nil
}

// StreamStatus streams online and offline transitions to combine with an initial
// stream.liveIds snapshot.
func (h *Handler) StreamStatus(ctx context.Context) (<-chan eventbus.StreamStatusEvent, error) {
	if h.bus == nil {
		ch := make(chan eventbus.StreamStatusEvent)
		close(ch)
		return ch, nil
	}
	return h.bus.StreamStatus.Subscribe(ctx), nil
}

// TaskStatus streams committed task transitions.
func (h *Handler) TaskStatus(ctx context.Context) (<-chan eventbus.TaskStatusEvent, error) {
	if h.bus == nil {
		ch := make(chan eventbus.TaskStatusEvent)
		close(ch)
		return ch, nil
	}
	return h.bus.TaskStatus.Subscribe(ctx), nil
}

// StorageStatus streams storage readiness transitions so the banner and the
// System card update without polling. The event contains only state and time;
// owners refetch storage.details for diagnostics through its separate role gate.
func (h *Handler) StorageStatus(ctx context.Context) (<-chan eventbus.StorageStatusEvent, error) {
	if h.bus == nil {
		ch := make(chan eventbus.StorageStatusEvent)
		close(ch)
		return ch, nil
	}
	return h.bus.StorageStatus.Subscribe(ctx), nil
}

// ArchiveQueue streams archive queue membership changes: enqueued, started,
// completed, failed, dequeued, retry cancelled. Viewer-level like the queue
// itself; subscribers refetch archive.queue on every event.
func (h *Handler) ArchiveQueue(ctx context.Context) (<-chan eventbus.ArchiveQueueEvent, error) {
	if h.bus == nil {
		ch := make(chan eventbus.ArchiveQueueEvent)
		close(ch)
		return ch, nil
	}
	return h.bus.ArchiveQueue.Subscribe(ctx), nil
}

// VideoChanges sends viewer-safe invalidations for committed recording, intent,
// and removal transitions. Subscribe before the transport acknowledges startup.
func (h *Handler) VideoChanges(ctx context.Context) (<-chan eventbus.VideoChangeEvent, error) {
	if h.bus == nil || h.bus.VideoChanges == nil {
		ch := make(chan eventbus.VideoChangeEvent)
		close(ch)
		return ch, nil
	}
	return h.bus.VideoChanges.Subscribe(ctx), nil
}
