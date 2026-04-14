package schedule

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// TestProcess_StreamOffline_EndsLastActiveStream confirms the Phase 5
// follow-up from the spec: on stream.offline, find the broadcaster's
// most recent active stream row and stamp ended_at. Idempotent on
// retries — the second call finds the already-ended row and returns
// without error.
func TestProcess_StreamOffline_EndsLastActiveStream(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := NewEventProcessor(repo, nil, nil, nil, nil, log)

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "b-off", BroadcasterLogin: "b", BroadcasterName: "b",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	stream, err := repo.UpsertStream(ctx, &repository.StreamInput{
		ID: "s-1", BroadcasterID: "b-off", Type: "live", Language: "en",
		ViewerCount: 100, StartedAt: time.Now().UTC().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("seed stream: %v", err)
	}

	n := &twitch.EventSubNotification{
		MessageType: twitch.MsgTypeNotification,
		Event: twitch.StreamOfflineEvent{
			BroadcasterUserID:    "b-off",
			BroadcasterUserLogin: "b",
			BroadcasterUserName:  "b",
		},
	}
	if err := p.Process(ctx, n); err != nil {
		t.Fatalf("process: %v", err)
	}

	got, err := repo.GetStream(ctx, stream.ID)
	if err != nil {
		t.Fatalf("get stream: %v", err)
	}
	if got.EndedAt == nil {
		t.Fatal("EndedAt must be set after stream.offline")
	}

	// Idempotency: re-processing the same offline event must not error
	// and must not re-stamp ended_at backwards.
	firstEnd := *got.EndedAt
	time.Sleep(10 * time.Millisecond)
	if err := p.Process(ctx, n); err != nil {
		t.Fatalf("re-process: %v", err)
	}
	got2, _ := repo.GetStream(ctx, stream.ID)
	if got2.EndedAt == nil || !got2.EndedAt.Equal(firstEnd) {
		t.Errorf("ended_at must be stable across retries: first %v second %v", firstEnd, got2.EndedAt)
	}
}

// TestHighestQuality_PicksHighRankAndBreaksTiesByID pins the winner-
// selection rule from .docs/spec/eventsub.md: on a stream.online match
// the processor must trigger ONE download at the highest-quality matching
// schedule's quality. Without this, two matching schedules race on the
// downloader's busy-check — whichever Start() call wins decides the
// quality, and if the lower-quality one wins the VOD gets recorded at
// the wrong setting.
//
// The ID-based tie-break keeps the choice deterministic across retries
// of the same event.
func TestHighestQuality_PicksHighRankAndBreaksTiesByID(t *testing.T) {
	cases := []struct {
		name   string
		input  []*repository.DownloadSchedule
		wantID int64
	}{
		{
			name: "single",
			input: []*repository.DownloadSchedule{
				{ID: 7, Quality: repository.QualityMedium},
			},
			wantID: 7,
		},
		{
			name: "HIGH beats MEDIUM beats LOW regardless of order",
			input: []*repository.DownloadSchedule{
				{ID: 3, Quality: repository.QualityLow},
				{ID: 1, Quality: repository.QualityHigh},
				{ID: 2, Quality: repository.QualityMedium},
			},
			wantID: 1,
		},
		{
			name: "ties on HIGH break to lower ID",
			input: []*repository.DownloadSchedule{
				{ID: 42, Quality: repository.QualityHigh},
				{ID: 7, Quality: repository.QualityHigh},
				{ID: 100, Quality: repository.QualityHigh},
			},
			wantID: 7,
		},
		{
			name: "unknown quality sorts below known values",
			input: []*repository.DownloadSchedule{
				{ID: 5, Quality: "BOGUS"},
				{ID: 9, Quality: repository.QualityLow},
			},
			wantID: 9,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := highestQuality(tc.input)
			if got.ID != tc.wantID {
				t.Errorf("got ID=%d, want %d (quality=%q)", got.ID, tc.wantID, got.Quality)
			}
		})
	}
}

// TestProcess_StreamStatus_PublishedOnOfflineTransition pins the
// delta-feed contract: every stream.offline webhook fires a
// StreamStatusEvent so SSE subscribers can drop the broadcaster from
// their live-set. Covers the active-stream path (ended_at stamped +
// event published) in one flow.
func TestProcess_StreamStatus_PublishedOnOfflineTransition(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	bus := eventbus.New()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := NewEventProcessor(repo, nil, nil, nil, bus, log)

	// Subscribe before the event fires so we don't miss it.
	sub := bus.StreamStatus.Subscribe(ctx)

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "b-status", BroadcasterLogin: "bs", BroadcasterName: "BS",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	if _, err := repo.UpsertStream(ctx, &repository.StreamInput{
		ID: "s-status", BroadcasterID: "b-status", Type: "live", Language: "en",
		ViewerCount: 5, StartedAt: time.Now().UTC().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("seed stream: %v", err)
	}

	n := &twitch.EventSubNotification{
		MessageType: twitch.MsgTypeNotification,
		Event: twitch.StreamOfflineEvent{
			BroadcasterUserID:    "b-status",
			BroadcasterUserLogin: "bs",
			BroadcasterUserName:  "BS",
		},
	}
	if err := p.Process(ctx, n); err != nil {
		t.Fatalf("process: %v", err)
	}

	select {
	case ev := <-sub:
		if ev.Kind != eventbus.StreamStatusOffline {
			t.Errorf("kind: got %q want %q", ev.Kind, eventbus.StreamStatusOffline)
		}
		if ev.BroadcasterID != "b-status" {
			t.Errorf("broadcaster_id: got %q", ev.BroadcasterID)
		}
		if ev.StreamID != "s-status" {
			t.Errorf("stream_id: got %q want s-status", ev.StreamID)
		}
		if ev.At.IsZero() {
			t.Error("At timestamp should be populated")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected StreamStatus event on bus; got none")
	}
}

// TestProcess_StreamStatus_PublishedOnOnlineTransition ensures the
// online delta fires even when no schedule matches — the channels-list
// live-indicator must reflect every online event, not only the ones
// that triggered a download.
func TestProcess_StreamStatus_PublishedOnOnlineTransition(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	bus := eventbus.New()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	// No twitch client, no downloader — we only care about the bus
	// publication, which happens before any schedule match or hydrate.
	p := NewEventProcessor(repo, nil, nil, nil, bus, log)

	sub := bus.StreamStatus.Subscribe(ctx)

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "b-on", BroadcasterLogin: "bo", BroadcasterName: "BO",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	n := &twitch.EventSubNotification{
		MessageType: twitch.MsgTypeNotification,
		Event: twitch.StreamOnlineEvent{
			ID:                   "evt-1",
			BroadcasterUserID:    "b-on",
			BroadcasterUserLogin: "bo",
			BroadcasterUserName:  "BO",
		},
	}
	if err := p.Process(ctx, n); err != nil {
		t.Fatalf("process: %v", err)
	}

	select {
	case ev := <-sub:
		if ev.Kind != eventbus.StreamStatusOnline {
			t.Errorf("kind: got %q want %q", ev.Kind, eventbus.StreamStatusOnline)
		}
		if ev.BroadcasterID != "b-on" {
			t.Errorf("broadcaster_id: got %q", ev.BroadcasterID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected StreamStatus online event on bus")
	}
}
