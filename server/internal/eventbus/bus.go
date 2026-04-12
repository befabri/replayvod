// Package eventbus is an in-process pub/sub used to fan out async
// events from producer code (scheduler, schedule processor, downloader
// wrappers) to tRPC subscription handlers that ship them to the
// dashboard over SSE.
//
// Design choices:
//   - Topic is the type parameter, not a runtime string, so mis-typed
//     subscriptions are a compile error instead of a silent empty stream.
//   - Bounded buffered channels per subscriber; a slow subscriber drops
//     events rather than blocking the publisher. Dashboard reconnect
//     is cheap; a wedged producer is not.
//   - Subscribers auto-unregister when their ctx is Done — the tRPC SSE
//     layer cancels ctx on client disconnect.
package eventbus

import (
	"context"
	"sync"
)

// Topic is a typed pub/sub channel. Each Topic instance is independent
// — two topics with the same generic parameter still don't share
// subscribers.
type Topic[T any] struct {
	mu      sync.Mutex
	next    int
	subs    map[int]chan T
	bufSize int
}

// NewTopic creates a topic. bufSize is the per-subscriber buffer;
// publishes to a full buffer drop that subscriber's event, which is
// the right tradeoff for UI dashboards where "latest N events" matter
// more than "every event." Pick a small number (8-32).
func NewTopic[T any](bufSize int) *Topic[T] {
	if bufSize <= 0 {
		bufSize = 16
	}
	return &Topic[T]{
		subs:    make(map[int]chan T),
		bufSize: bufSize,
	}
}

// Subscribe registers a new receiver. The returned channel closes
// when ctx is cancelled. Safe to call concurrently.
func (t *Topic[T]) Subscribe(ctx context.Context) <-chan T {
	ch := make(chan T, t.bufSize)
	t.mu.Lock()
	id := t.next
	t.next++
	t.subs[id] = ch
	t.mu.Unlock()

	go func() {
		<-ctx.Done()
		t.mu.Lock()
		if existing, ok := t.subs[id]; ok {
			delete(t.subs, id)
			close(existing)
		}
		t.mu.Unlock()
	}()

	return ch
}

// Publish fans an event out to every current subscriber. Non-blocking:
// a full subscriber buffer silently drops that subscriber's copy. The
// contract is "latest events delivered" not "every event delivered."
func (t *Topic[T]) Publish(event T) {
	t.mu.Lock()
	targets := make([]chan T, 0, len(t.subs))
	for _, ch := range t.subs {
		targets = append(targets, ch)
	}
	t.mu.Unlock()

	for _, ch := range targets {
		select {
		case ch <- event:
		default:
			// Drop — slow consumer. No logging; this fires per-event
			// at normal operation for disconnected clients and would
			// flood the log.
		}
	}
}

// Count returns the current subscriber count. Test/debug only.
func (t *Topic[T]) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.subs)
}
