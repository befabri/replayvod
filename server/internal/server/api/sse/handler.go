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

type Handler struct {
	bus *eventbus.Buses
	log *slog.Logger
}

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

// StreamLive streams schedule-match notifications (our "Just went
// live AND we started a recording" feed). Viewer-level; filtering by
// follow happens client-side. See also StreamStatus for the broader
// online/offline delta feed.
func (h *Handler) StreamLive(ctx context.Context) (<-chan eventbus.StreamLiveEvent, error) {
	if h.bus == nil {
		ch := make(chan eventbus.StreamLiveEvent)
		close(ch)
		return ch, nil
	}
	return h.bus.StreamLive.Subscribe(ctx), nil
}

// StreamStatus streams online/offline transitions for every
// stream.online and stream.offline EventSub webhook, regardless of
// schedule-match. Subscribers maintain a "currently live" Set by
// composing this feed with an initial stream.liveIds snapshot. No
// polling required once the initial load settles.
func (h *Handler) StreamStatus(ctx context.Context) (<-chan eventbus.StreamStatusEvent, error) {
	if h.bus == nil {
		ch := make(chan eventbus.StreamStatusEvent)
		close(ch)
		return ch, nil
	}
	return h.bus.StreamStatus.Subscribe(ctx), nil
}

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

func (h *Handler) VideoRemovals(ctx context.Context) (<-chan eventbus.VideoRemovalEvent, error) {
	if h.bus == nil || h.bus.VideoRemovals == nil {
		ch := make(chan eventbus.VideoRemovalEvent)
		close(ch)
		return ch, nil
	}
	return h.bus.VideoRemovals.Subscribe(ctx), nil
}
