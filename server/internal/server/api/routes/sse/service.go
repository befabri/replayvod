// Package sse hosts the Server-Sent Events subscription procedures
// that fan out to the trpcgo subscription transport. Every procedure
// here subscribes to an eventbus Topic and relays events onto the
// returned channel until ctx is cancelled.
//
// These are tRPC subscription procedures, not Chi SSE endpoints —
// trpcgo handles the wire protocol and reconnection shape.
package sse

import (
	"context"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/eventbus"
)

// Service wraps the bus for tRPC procedure signatures.
type Service struct {
	bus *eventbus.Buses
	log *slog.Logger
}

// NewService builds an SSE service bound to a topic set. bus may be
// nil; each Subscribe method then returns a pre-closed channel.
func NewService(bus *eventbus.Buses, log *slog.Logger) *Service {
	return &Service{
		bus: bus,
		log: log.With("domain", "sse"),
	}
}

// SystemEvents streams event_logs rows as they're written. Owner-only
// per router wiring; these surface app-level activity that's not meant
// for regular viewers.
func (s *Service) SystemEvents(ctx context.Context) (<-chan eventbus.EventLogEvent, error) {
	if s.bus == nil {
		ch := make(chan eventbus.EventLogEvent)
		close(ch)
		return ch, nil
	}
	return s.bus.EventLogs.Subscribe(ctx), nil
}

// StreamLive streams channels-went-live notifications. Viewer-level:
// any authenticated user can see which followed channels are live;
// filtering by follow happens client-side.
func (s *Service) StreamLive(ctx context.Context) (<-chan eventbus.StreamLiveEvent, error) {
	if s.bus == nil {
		ch := make(chan eventbus.StreamLiveEvent)
		close(ch)
		return ch, nil
	}
	return s.bus.StreamLive.Subscribe(ctx), nil
}

// TaskStatus streams scheduler task lifecycle transitions. Owner-only.
func (s *Service) TaskStatus(ctx context.Context) (<-chan eventbus.TaskStatusEvent, error) {
	if s.bus == nil {
		ch := make(chan eventbus.TaskStatusEvent)
		close(ch)
		return ch, nil
	}
	return s.bus.TaskStatus.Subscribe(ctx), nil
}
