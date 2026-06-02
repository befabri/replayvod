package downloader

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
)

type fakeChannelSubs struct {
	mu           sync.Mutex
	subscribed   []string
	unsubscribed []unsubCall
	subscribeErr error
}

type staticTitleTrackingOffset struct {
	seconds float64
	ok      bool
}

func (s staticTitleTrackingOffset) MediaOffsetSeconds() (float64, bool) {
	return s.seconds, s.ok
}

type unsubCall struct {
	broadcasterID string
	reason        string
}

func (f *fakeChannelSubs) SubscribeChannelUpdate(_ context.Context, broadcasterID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.subscribeErr != nil {
		return f.subscribeErr
	}
	f.subscribed = append(f.subscribed, broadcasterID)
	return nil
}

func (f *fakeChannelSubs) UnsubscribeChannelUpdate(_ context.Context, broadcasterID, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unsubscribed = append(f.unsubscribed, unsubCall{broadcasterID, reason})
	return nil
}

func (f *fakeChannelSubs) subscribeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.subscribed)
}

func (f *fakeChannelSubs) unsubscribeCalls() []unsubCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]unsubCall(nil), f.unsubscribed...)
}

type watchCall struct {
	broadcasterID string
	videoID       int64
	initial       streammeta.WatchInitial
}

// fakeTitleWatcher records a Watch call and then blocks until the context is
// cancelled, mirroring the real MetadataWatcher's lifetime.
type fakeTitleWatcher struct {
	started chan watchCall
}

func (f *fakeTitleWatcher) Watch(ctx context.Context, broadcasterID string, videoID int64, initial streammeta.WatchInitial) {
	f.started <- watchCall{broadcasterID, videoID, initial}
	<-ctx.Done()
}

func newTitleTrackingService(mode string, subs ChannelUpdateSubscriber, watcher titleWatcher) *Service {
	s := &Service{
		cfg:         &config.Config{ServerMode: config.ServerModeConfig{Mode: mode}},
		channelSubs: subs,
	}
	if watcher != nil {
		s.metaWatcher = watcher
	}
	return s
}

// TestStartTitleTracking_WebhookSubscribesAndCleanupUnsubscribes pins the
// direct/relay path: a channel.update subscribe fires on start and the returned
// cleanup unsubscribes. No poll watcher is registered.
func TestStartTitleTracking_WebhookSubscribesAndCleanupUnsubscribes(t *testing.T) {
	subs := &fakeChannelSubs{}
	s := newTitleTrackingService(config.ServerModeDirect, subs, nil)

	var pollers int
	cleanup := s.startTitleTracking(context.Background(), Params{BroadcasterID: "b-1"}, 7, discardLog(),
		func(context.CancelFunc) { pollers++ }, nil)

	if subs.subscribeCount() != 1 {
		t.Fatalf("subscribe count = %d, want 1", subs.subscribeCount())
	}
	if pollers != 0 {
		t.Fatalf("poll watcher registered in webhook mode: %d", pollers)
	}

	cleanup()
	calls := subs.unsubscribeCalls()
	if len(calls) != 1 || calls[0].broadcasterID != "b-1" || calls[0].reason != "recording ended" {
		t.Fatalf("unsubscribe calls = %+v, want one for b-1 with reason 'recording ended'", calls)
	}
}

// TestStartTitleTracking_WebhookSubscribeFailureHasNoPollFallback pins that a
// failed subscribe does not silently fall back to the poll watcher (poll-mode
// title tracking only exists in poll mode), even when a watcher dependency is
// present. The recording keeps just its at-start title and the cleanup is a
// no-op (nothing to unsubscribe).
func TestStartTitleTracking_WebhookSubscribeFailureHasNoPollFallback(t *testing.T) {
	subs := &fakeChannelSubs{subscribeErr: errors.New("twitch 400")}
	watcher := &fakeTitleWatcher{started: make(chan watchCall, 1)}
	s := newTitleTrackingService(config.ServerModeDirect, subs, watcher)

	var pollers int
	cleanup := s.startTitleTracking(context.Background(), Params{BroadcasterID: "b-1"}, 7, discardLog(),
		func(context.CancelFunc) { pollers++ }, nil)
	cleanup()

	if pollers != 0 {
		t.Fatalf("poll watcher registered after webhook subscribe failure: %d", pollers)
	}
	select {
	case c := <-watcher.started:
		t.Fatalf("title watcher started after subscribe failure: %+v", c)
	case <-time.After(100 * time.Millisecond):
	}
	if calls := subs.unsubscribeCalls(); len(calls) != 0 {
		t.Fatalf("unsubscribe called after a failed subscribe: %+v", calls)
	}
}

// TestStartTitleTracking_PollStartsWatcher pins the poll path: the Helix watcher
// is launched with the recording's seed title/category, and its cancel is handed
// to registerPoller (torn down with the other media pollers, not via the
// returned cleanup).
func TestStartTitleTracking_PollStartsWatcher(t *testing.T) {
	watcher := &fakeTitleWatcher{started: make(chan watchCall, 1)}
	s := newTitleTrackingService(config.ServerModePoll, nil, watcher)

	var registered []context.CancelFunc
	offsetProvider := staticTitleTrackingOffset{seconds: 12.5, ok: true}
	cleanup := s.startTitleTracking(context.Background(),
		Params{BroadcasterID: "b-1", Title: "Opening", CategoryID: "cat-1"}, 42, discardLog(),
		func(c context.CancelFunc) { registered = append(registered, c) }, offsetProvider)

	select {
	case got := <-watcher.started:
		if got.broadcasterID != "b-1" || got.videoID != 42 {
			t.Fatalf("Watch args = %+v, want b-1/42", got)
		}
		if got.initial.Title != "Opening" || got.initial.CategoryID != "cat-1" {
			t.Fatalf("Watch initial = %+v, want seed title/category", got.initial)
		}
		if got.initial.MediaOffset == nil {
			t.Fatalf("Watch initial media offset provider is nil")
		}
		offset, ok := got.initial.MediaOffset.MediaOffsetSeconds()
		if !ok || offset != 12.5 {
			t.Fatalf("Watch initial media offset = %v/%v, want 12.5/true", offset, ok)
		}
	case <-time.After(time.Second):
		t.Fatal("poll watcher was not started")
	}
	if len(registered) != 1 {
		t.Fatalf("registered pollers = %d, want 1", len(registered))
	}
	// The returned cleanup is a no-op for poll mode; the watcher is stopped via
	// the registered cancel so the goroutine doesn't leak.
	cleanup()
	for _, c := range registered {
		c()
	}
}

// TestStartTitleTracking_OffDoesNothing pins that off mode neither subscribes nor
// polls even when both dependencies are wired.
func TestStartTitleTracking_OffDoesNothing(t *testing.T) {
	subs := &fakeChannelSubs{}
	watcher := &fakeTitleWatcher{started: make(chan watchCall, 1)}
	s := newTitleTrackingService(config.ServerModeOff, subs, watcher)

	var pollers int
	cleanup := s.startTitleTracking(context.Background(), Params{BroadcasterID: "b-1"}, 7, discardLog(),
		func(context.CancelFunc) { pollers++ }, nil)
	cleanup()

	if subs.subscribeCount() != 0 || pollers != 0 {
		t.Fatalf("off mode did work: subscribes=%d pollers=%d", subs.subscribeCount(), pollers)
	}
	select {
	case c := <-watcher.started:
		t.Fatalf("watcher started in off mode: %+v", c)
	case <-time.After(100 * time.Millisecond):
	}
}
