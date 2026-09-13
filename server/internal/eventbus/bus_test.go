package eventbus

import (
	"context"
	"runtime"
	"sync"
	"testing"
)

func TestPublishConcurrentUnsubscribe(t *testing.T) {
	topic := NewTopic[int](1)
	ctx, cancel := context.WithCancel(t.Context())
	var publishers sync.WaitGroup
	panics := make(chan any, 4)
	for range cap(panics) {
		publishers.Go(func() {
			defer func() {
				if p := recover(); p != nil {
					panics <- p
				}
			}()
			for ctx.Err() == nil {
				topic.Publish(1)
				runtime.Gosched()
			}
		})
	}
	t.Cleanup(func() { cancel(); publishers.Wait() })
	for range 2000 {
		subCtx, unsubscribe := context.WithCancel(ctx)
		ch := topic.Subscribe(subCtx)
		runtime.Gosched()
		unsubscribe()
		for range ch {
		}
	}
	cancel()
	publishers.Wait()
	close(panics)
	for p := range panics {
		t.Errorf("publishing during disconnect panicked: %v", p)
	}
	if got := topic.Count(); got != 0 {
		t.Fatalf("disconnected subscribers remain: %d", got)
	}
}

// TestNewTopicBufferDefault prevents an unbuffered channel from dropping every publication.
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

func TestPublishDelivers(t *testing.T) {
	topic := NewTopic[int](4)
	ch := topic.Subscribe(t.Context())

	topic.Publish(42)

	if got := <-ch; got != 42 {
		t.Fatalf("received %d, want 42", got)
	}
}

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

// TestPublishDropsOnFullBuffer catches a blocking send when the subscriber stops reading.
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

func TestPublishWithNoSubscribers(t *testing.T) {
	topic := NewTopic[int](4)
	if got := topic.Count(); got != 0 {
		t.Fatalf("Count on fresh topic = %d, want 0", got)
	}

	topic.Publish(99) // no subscribers: must not panic
}

// TestCountTracksSubscribers guards against subscriber ID collisions.
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

func TestSubscribeUnregistersOnContextCancel(t *testing.T) {
	topic := NewTopic[int](4)
	ctx, cancel := context.WithCancel(context.Background())
	ch := topic.Subscribe(ctx)
	if got := topic.Count(); got != 1 {
		t.Fatalf("Count after subscribe = %d, want 1", got)
	}

	cancel()
	for range ch {
		// Channel closure follows removal under the topic lock.
	}

	if got := topic.Count(); got != 0 {
		t.Fatalf("Count after cancel = %d, want 0", got)
	}
}

// TestPublishAfterUnsubscribeSkipsClosedChannel catches sends to stale subscriber references.
func TestPublishAfterUnsubscribeSkipsClosedChannel(t *testing.T) {
	topic := NewTopic[int](4)
	ctx, cancel := context.WithCancel(context.Background())
	gone := topic.Subscribe(ctx)
	stay := topic.Subscribe(t.Context())

	cancel()
	for range gone {
		// Channel closure synchronizes the following publish with unsubscription.
	}
	if got := topic.Count(); got != 1 {
		t.Fatalf("Count after one cancel = %d, want 1", got)
	}

	topic.Publish(7) // must not panic on the now-closed gone channel

	if got := <-stay; got != 7 {
		t.Fatalf("live subscriber received %d, want 7", got)
	}
}

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

func TestVideoChangesCoalesceWithoutBlockingPublishers(t *testing.T) {
	bus := New()
	events := bus.VideoChanges.Subscribe(t.Context())
	for range 1000 {
		bus.NotifyVideoChange()
	}
	select {
	case <-events:
	default:
		t.Fatal("lost video invalidation")
	}
	select {
	case <-events:
		t.Fatal("video invalidations did not coalesce")
	default:
	}
	bus.NotifyVideoChange()
	select {
	case <-events:
	default:
		t.Fatal("lost transition after draining earlier invalidation")
	}
}

func TestVideoChangesOptionalBus(t *testing.T) {
	var absent *Buses
	absent.NotifyVideoChange()
	(&Buses{}).NotifyVideoChange()
}
