package schedule

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/befabri/replayvod/server/internal/twitch"
)

type fakeStreamFetcher struct {
	streams    []twitch.Stream
	err        error
	calls      int
	lastParams *twitch.GetStreamsParams
}

func (f *fakeStreamFetcher) GetStreams(_ context.Context, params *twitch.GetStreamsParams) ([]twitch.Stream, twitch.Pagination, error) {
	f.calls++
	f.lastParams = params
	return f.streams, twitch.Pagination{}, f.err
}

type fakeTargetedDispatcher struct {
	calls      int
	scheduleID int64
	streamID   string
	err        error
}

func (f *fakeTargetedDispatcher) DispatchStreamOnlineFromStreamForSchedule(_ context.Context, stream twitch.Stream, scheduleID int64) error {
	f.calls++
	f.scheduleID = scheduleID
	f.streamID = stream.ID
	return f.err
}

func TestImmediateTrigger_OfflineIsNoop(t *testing.T) {
	fetcher := &fakeStreamFetcher{}
	dispatcher := &fakeTargetedDispatcher{}
	trigger := NewImmediateTrigger(fetcher, dispatcher, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := trigger.TriggerScheduleIfLive(context.Background(), 42, "b-1"); err != nil {
		t.Fatalf("TriggerScheduleIfLive: %v", err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("fetcher calls = %d, want 1", fetcher.calls)
	}
	assertStreamProbeParams(t, fetcher, "b-1")
	if dispatcher.calls != 0 {
		t.Fatalf("dispatcher calls = %d, want 0", dispatcher.calls)
	}
}

func TestImmediateTrigger_OnlineDispatchesTargetSchedule(t *testing.T) {
	fetcher := &fakeStreamFetcher{streams: []twitch.Stream{{ID: "s-1", UserID: "b-1"}}}
	dispatcher := &fakeTargetedDispatcher{}
	trigger := NewImmediateTrigger(fetcher, dispatcher, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := trigger.TriggerScheduleIfLive(context.Background(), 42, "b-1"); err != nil {
		t.Fatalf("TriggerScheduleIfLive: %v", err)
	}
	assertStreamProbeParams(t, fetcher, "b-1")
	if dispatcher.calls != 1 {
		t.Fatalf("dispatcher calls = %d, want 1", dispatcher.calls)
	}
	if dispatcher.scheduleID != 42 || dispatcher.streamID != "s-1" {
		t.Fatalf("dispatcher target = schedule %d stream %q, want 42/s-1", dispatcher.scheduleID, dispatcher.streamID)
	}
}

func TestImmediateTrigger_PropagatesProbeAndDispatchErrors(t *testing.T) {
	probeErr := errors.New("probe failed")
	trigger := NewImmediateTrigger(&fakeStreamFetcher{err: probeErr}, &fakeTargetedDispatcher{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := trigger.TriggerScheduleIfLive(context.Background(), 42, "b-1"); !errors.Is(err, probeErr) {
		t.Fatalf("probe error = %v, want %v", err, probeErr)
	}

	dispatchErr := errors.New("dispatch failed")
	trigger = NewImmediateTrigger(
		&fakeStreamFetcher{streams: []twitch.Stream{{ID: "s-1", UserID: "b-1"}}},
		&fakeTargetedDispatcher{err: dispatchErr},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err := trigger.TriggerScheduleIfLive(context.Background(), 42, "b-1"); !errors.Is(err, dispatchErr) {
		t.Fatalf("dispatch error = %v, want %v", err, dispatchErr)
	}
}

func TestImmediateTrigger_IgnoresInvalidInputOrMissingDependencies(t *testing.T) {
	fetcher := &fakeStreamFetcher{streams: []twitch.Stream{{ID: "s-1", UserID: "b-1"}}}
	dispatcher := &fakeTargetedDispatcher{}

	trigger := NewImmediateTrigger(fetcher, dispatcher, nil)
	if err := trigger.TriggerScheduleIfLive(context.Background(), 0, "b-1"); err != nil {
		t.Fatalf("zero schedule id error = %v, want nil", err)
	}
	if err := trigger.TriggerScheduleIfLive(context.Background(), 42, ""); err != nil {
		t.Fatalf("empty broadcaster id error = %v, want nil", err)
	}
	if fetcher.calls != 0 || dispatcher.calls != 0 {
		t.Fatalf("invalid input calls fetcher=%d dispatcher=%d, want 0/0", fetcher.calls, dispatcher.calls)
	}

	missingDispatcher := NewImmediateTrigger(fetcher, nil, nil)
	if err := missingDispatcher.TriggerScheduleIfLive(context.Background(), 42, "b-1"); err != nil {
		t.Fatalf("missing dispatcher error = %v, want nil", err)
	}
	if fetcher.calls != 0 {
		t.Fatalf("missing dispatcher fetcher calls = %d, want 0", fetcher.calls)
	}
}

func assertStreamProbeParams(t *testing.T, fetcher *fakeStreamFetcher, broadcasterID string) {
	t.Helper()
	if fetcher.lastParams == nil {
		t.Fatal("GetStreams params = nil")
	}
	if len(fetcher.lastParams.UserID) != 1 || fetcher.lastParams.UserID[0] != broadcasterID {
		t.Fatalf("GetStreams UserID = %v, want [%s]", fetcher.lastParams.UserID, broadcasterID)
	}
	if fetcher.lastParams.First != 1 {
		t.Fatalf("GetStreams First = %d, want 1", fetcher.lastParams.First)
	}
}
