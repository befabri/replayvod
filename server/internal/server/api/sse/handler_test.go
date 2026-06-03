package sse

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/eventbus"
)

func newLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNilBus_ChannelsPreClosed(t *testing.T) {
	h := NewHandler(nil, newLog())

	t.Run("SystemEvents", func(t *testing.T) {
		ch, err := h.SystemEvents(context.Background())
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		assertClosed(t, func() bool {
			select {
			case _, ok := <-ch:
				return !ok
			default:
				return false
			}
		})
	})

	t.Run("StreamLive", func(t *testing.T) {
		ch, err := h.StreamLive(context.Background())
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		assertClosed(t, func() bool {
			select {
			case _, ok := <-ch:
				return !ok
			default:
				return false
			}
		})
	})

	t.Run("StreamStatus", func(t *testing.T) {
		ch, err := h.StreamStatus(context.Background())
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		assertClosed(t, func() bool {
			select {
			case _, ok := <-ch:
				return !ok
			default:
				return false
			}
		})
	})

	t.Run("TaskStatus", func(t *testing.T) {
		ch, err := h.TaskStatus(context.Background())
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		assertClosed(t, func() bool {
			select {
			case _, ok := <-ch:
				return !ok
			default:
				return false
			}
		})
	})
}

func assertClosed(t *testing.T, check func() bool) {
	t.Helper()
	if !check() {
		t.Error("channel was not pre-closed")
	}
}

func TestLiveBus_SubscribeAndReceive(t *testing.T) {
	bus := eventbus.New()
	h := NewHandler(bus, newLog())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("SystemEvents receives EventLogEvent", func(t *testing.T) {
		ch, err := h.SystemEvents(ctx)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		want := eventbus.EventLogEvent{ID: 1, Domain: "test", Message: "hello"}
		bus.EventLogs.Publish(want)
		select {
		case got := <-ch:
			if got.ID != want.ID || got.Message != want.Message {
				t.Errorf("got %+v, want %+v", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for EventLogEvent")
		}
	})

	t.Run("StreamLive receives StreamLiveEvent", func(t *testing.T) {
		ch, err := h.StreamLive(ctx)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		want := eventbus.StreamLiveEvent{BroadcasterID: "bc1", BroadcasterLogin: "alice"}
		bus.StreamLive.Publish(want)
		select {
		case got := <-ch:
			if got.BroadcasterID != want.BroadcasterID {
				t.Errorf("BroadcasterID = %q, want %q", got.BroadcasterID, want.BroadcasterID)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for StreamLiveEvent")
		}
	})

	t.Run("StreamStatus receives StreamStatusEvent", func(t *testing.T) {
		ch, err := h.StreamStatus(ctx)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		want := eventbus.StreamStatusEvent{
			Kind:          eventbus.StreamStatusOnline,
			BroadcasterID: "bc2",
		}
		bus.StreamStatus.Publish(want)
		select {
		case got := <-ch:
			if got.Kind != want.Kind || got.BroadcasterID != want.BroadcasterID {
				t.Errorf("got %+v, want %+v", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for StreamStatusEvent")
		}
	})

	t.Run("TaskStatus receives TaskStatusEvent", func(t *testing.T) {
		ch, err := h.TaskStatus(ctx)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		want := eventbus.TaskStatusEvent{Name: "cleanup", Status: "running"}
		bus.TaskStatus.Publish(want)
		select {
		case got := <-ch:
			if got.Name != want.Name || got.Status != want.Status {
				t.Errorf("got %+v, want %+v", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for TaskStatusEvent")
		}
	})
}

func TestLiveBus_ContextCancelClosesChannel(t *testing.T) {
	bus := eventbus.New()
	h := NewHandler(bus, newLog())
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := h.SystemEvents(ctx)
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	cancel()

	// Closure happens asynchronously after ctx.Done.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("channel not closed after context cancel")
		}
	}
}
