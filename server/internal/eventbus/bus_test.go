package eventbus

import (
	"context"
	"testing"
)

// TestNewTopicBufferDefault pins the per-subscriber buffer sizing: a
// non-positive bufSize falls back to 16, and a positive value is used
// verbatim. The buffer is observable as the capacity of a subscribed channel.
// Without this the `bufSize <= 0` guard and the 16 default could flip with no
// test noticing, and a zero-capacity channel would turn every Publish into a
// synchronous send that the drop-on-full contract exists to avoid.
func TestNewTopicBufferDefault(t *testing.T) {
	cases := []struct {
		name    string
		bufSize int
		wantCap int
	}{
		{name: "zero falls back to 16", bufSize: 0, wantCap: 16},
		{name: "negative falls back to 16", bufSize: -5, wantCap: 16},
		{name: "one is kept", bufSize: 1, wantCap: 1},
		{name: "explicit size is kept", bufSize: 4, wantCap: 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			topic := NewTopic[int](tc.bufSize)
			ch := topic.Subscribe(t.Context())
			if got := cap(ch); got != tc.wantCap {
				t.Fatalf("cap(Subscribe ch) for bufSize %d = %d, want %d", tc.bufSize, got, tc.wantCap)
			}
		})
	}
}

// TestPublishDelivers pins the core contract: an event published after a
// subscribe reaches that subscriber. Publish buffers the send before
// returning, so the receive needs no synchronization beyond the channel
// itself; no sleep is involved.
func TestPublishDelivers(t *testing.T) {
	topic := NewTopic[int](4)
	ch := topic.Subscribe(t.Context())

	topic.Publish(42)

	if got := <-ch; got != 42 {
		t.Fatalf("received %d, want 42", got)
	}
}

// TestPublishFansOutToAllSubscribers pins that every current subscriber gets
// its own copy, not just the first one the snapshot loop happens to visit.
// Map iteration order is random, so both channels are checked after a single
// Publish; both buffers already hold the event by the time Publish returns.
func TestPublishFansOutToAllSubscribers(t *testing.T) {
	topic := NewTopic[string](4)
	a := topic.Subscribe(t.Context())
	b := topic.Subscribe(t.Context())

	topic.Publish("live")

	for i, ch := range []<-chan string{a, b} {
		if got := <-ch; got != "live" {
			t.Fatalf("subscriber %d received %q, want %q", i, got, "live")
		}
	}
}

// TestPublishDropsOnFullBuffer pins the non-blocking contract: once a
// subscriber's buffer is full, further publishes drop that subscriber's copy
// rather than block the publisher. With the select's default branch removed
// the third Publish would block forever (caught as a timeout); with it intact
// the buffer holds exactly the first two events in order and the third is
// gone.
func TestPublishDropsOnFullBuffer(t *testing.T) {
	topic := NewTopic[int](2)
	ch := topic.Subscribe(t.Context())

	topic.Publish(1)
	topic.Publish(2)
	topic.Publish(3) // buffer full: this copy is dropped, must not block

	if got := <-ch; got != 1 {
		t.Fatalf("first buffered event = %d, want 1", got)
	}
	if got := <-ch; got != 2 {
		t.Fatalf("second buffered event = %d, want 2", got)
	}
	select {
	case extra, ok := <-ch:
		t.Fatalf("buffer should be drained, got %d (open=%v)", extra, ok)
	default:
	}
}

// TestPublishWithNoSubscribers pins that publishing into an empty topic is a
// safe no-op. Producers (scheduler, downloader) publish unconditionally
// whether or not the dashboard is connected, so the zero-subscriber path is
// the common case, not an edge case. The snapshot slice must be sized from the
// live subscriber count; a wrong initial length would panic here precisely
// when there are no subscribers to absorb it.
func TestPublishWithNoSubscribers(t *testing.T) {
	topic := NewTopic[int](4)
	if got := topic.Count(); got != 0 {
		t.Fatalf("Count on fresh topic = %d, want 0", got)
	}

	topic.Publish(99) // no subscribers: must not panic
}

// TestCountTracksSubscribers pins that Count reflects each live subscriber and
// that distinct subscribes are assigned distinct ids. A dropped `next++` would
// collapse both subscribers onto the same map key, leaving Count at 1.
func TestCountTracksSubscribers(t *testing.T) {
	topic := NewTopic[int](4)
	if got := topic.Count(); got != 0 {
		t.Fatalf("Count on fresh topic = %d, want 0", got)
	}

	topic.Subscribe(t.Context())
	topic.Subscribe(t.Context())

	if got := topic.Count(); got != 2 {
		t.Fatalf("Count after two subscribes = %d, want 2", got)
	}
}

// TestSubscribeUnregistersOnContextCancel pins the auto-cleanup: cancelling a
// subscriber's context closes its channel and removes it from the subscriber
// set. Ranging the channel to completion is the synchronization point. It
// returns only once the unregister goroutine has closed the channel, and the
// delete runs under the same lock just before that close, so the subsequent
// Count is guaranteed to observe the removal without any sleep.
func TestSubscribeUnregistersOnContextCancel(t *testing.T) {
	topic := NewTopic[int](4)
	ctx, cancel := context.WithCancel(context.Background())
	ch := topic.Subscribe(ctx)
	if got := topic.Count(); got != 1 {
		t.Fatalf("Count after subscribe = %d, want 1", got)
	}

	cancel()
	for range ch {
		// Block until the unregister goroutine closes ch.
	}

	if got := topic.Count(); got != 0 {
		t.Fatalf("Count after cancel = %d, want 0", got)
	}
}

// TestPublishAfterUnsubscribeSkipsClosedChannel pins that an unregistered
// subscriber is genuinely removed from the set before its channel is closed:
// were the delete skipped, the next Publish would send on a closed channel and
// panic. A second, still-live subscriber must keep receiving, confirming
// fan-out continues from the trimmed set.
func TestPublishAfterUnsubscribeSkipsClosedChannel(t *testing.T) {
	topic := NewTopic[int](4)
	ctx, cancel := context.WithCancel(context.Background())
	gone := topic.Subscribe(ctx)
	stay := topic.Subscribe(t.Context())

	cancel()
	for range gone {
		// Block until the cancelled subscriber's channel closes.
	}
	if got := topic.Count(); got != 1 {
		t.Fatalf("Count after one cancel = %d, want 1", got)
	}

	topic.Publish(7) // must not panic on the now-closed gone channel

	if got := <-stay; got != 7 {
		t.Fatalf("live subscriber received %d, want 7", got)
	}
}

// TestNewWiresEveryTopicWithItsBufferSize pins that New constructs all four
// topics and gives each the buffer size the dashboard feeds were tuned for.
// The size is read back as the capacity of a subscribed channel, so a flipped
// constant or a topic left nil surfaces here.
func TestNewWiresEveryTopicWithItsBufferSize(t *testing.T) {
	buses := New()
	if buses == nil {
		t.Fatal("New returned nil")
	}
	ctx := t.Context()

	if buses.EventLogs == nil {
		t.Fatal("EventLogs topic is nil")
	}
	if got := cap(buses.EventLogs.Subscribe(ctx)); got != 32 {
		t.Errorf("EventLogs buffer cap = %d, want 32", got)
	}

	if buses.StreamLive == nil {
		t.Fatal("StreamLive topic is nil")
	}
	if got := cap(buses.StreamLive.Subscribe(ctx)); got != 16 {
		t.Errorf("StreamLive buffer cap = %d, want 16", got)
	}

	if buses.StreamStatus == nil {
		t.Fatal("StreamStatus topic is nil")
	}
	if got := cap(buses.StreamStatus.Subscribe(ctx)); got != 32 {
		t.Errorf("StreamStatus buffer cap = %d, want 32", got)
	}

	if buses.TaskStatus == nil {
		t.Fatal("TaskStatus topic is nil")
	}
	if got := cap(buses.TaskStatus.Subscribe(ctx)); got != 32 {
		t.Errorf("TaskStatus buffer cap = %d, want 32", got)
	}
}
