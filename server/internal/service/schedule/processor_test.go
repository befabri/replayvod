package schedule

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/eventbus"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// fakeDownloader lets the dispatch tests force dl.Start outcomes (notably
// downloader.ErrBusy) without standing up a real *downloader.Service.
type fakeDownloader struct {
	startErr   error
	calls      int
	lastParams downloader.Params
}

func (f *fakeDownloader) Start(_ context.Context, p downloader.Params) (string, error) {
	f.calls++
	f.lastParams = p
	return "job-fake", f.startErr
}

type categoryFilterFailRepo struct {
	repository.Repository
	scheduleID int64
	err        error
}

func (r *categoryFilterFailRepo) ListScheduleCategories(ctx context.Context, scheduleID int64) ([]repository.Category, error) {
	if scheduleID == r.scheduleID {
		return nil, r.err
	}
	return r.Repository.ListScheduleCategories(ctx, scheduleID)
}

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

func TestProcess_DecodedPointerEventsDispatch(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	hydrator := streammeta.NewHydrator(repo, nil, streammeta.Config{}, log)
	p := NewEventProcessor(repo, nil, nil, hydrator, nil, log)

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "b-pointer-off", BroadcasterLogin: "off", BroadcasterName: "Off",
	}); err != nil {
		t.Fatalf("seed offline channel: %v", err)
	}
	stream, err := repo.UpsertStream(ctx, &repository.StreamInput{
		ID: "s-pointer-off", BroadcasterID: "b-pointer-off", Type: "live", Language: "en",
		ViewerCount: 42, StartedAt: time.Now().UTC().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("seed offline stream: %v", err)
	}

	if err := p.Process(ctx, &twitch.EventSubNotification{
		MessageType: twitch.MsgTypeNotification,
		Event: &twitch.StreamOfflineEvent{
			BroadcasterUserID:    "b-pointer-off",
			BroadcasterUserLogin: "off",
			BroadcasterUserName:  "Off",
		},
	}); err != nil {
		t.Fatalf("process pointer stream.offline: %v", err)
	}
	gotStream, err := repo.GetStream(ctx, stream.ID)
	if err != nil {
		t.Fatalf("get offline stream: %v", err)
	}
	if gotStream.EndedAt == nil {
		t.Fatal("pointer stream.offline did not set EndedAt")
	}

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "b-pointer-update", BroadcasterLogin: "upd", BroadcasterName: "Update",
	}); err != nil {
		t.Fatalf("seed update channel: %v", err)
	}
	video, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID:         "job-pointer-update",
		Filename:      "pointer-update.mp4",
		DisplayName:   "Update",
		Status:        repository.VideoStatusRunning,
		Quality:       repository.QualityHigh,
		BroadcasterID: "b-pointer-update",
		Language:      "en",
		RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("seed update video: %v", err)
	}
	if _, err := repo.CreateJob(ctx, &repository.JobInput{
		ID:            "job-pointer-update",
		VideoID:       video.ID,
		BroadcasterID: "b-pointer-update",
	}); err != nil {
		t.Fatalf("seed active update job: %v", err)
	}

	if err := p.Process(ctx, &twitch.EventSubNotification{
		MessageType: twitch.MsgTypeNotification,
		Event: &twitch.ChannelUpdateEvent{
			BroadcasterUserID:    "b-pointer-update",
			BroadcasterUserLogin: "upd",
			BroadcasterUserName:  "Update",
			Title:                "Pointer Title",
			CategoryID:           "game-pointer",
			CategoryName:         "Pointer Game",
		},
	}); err != nil {
		t.Fatalf("process pointer channel.update: %v", err)
	}

	changes, err := repo.ListVideoMetadataChanges(ctx, video.ID)
	if err != nil {
		t.Fatalf("list metadata changes: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("metadata changes = %d, want 1", len(changes))
	}
	if changes[0].Title == nil || changes[0].Title.Name != "Pointer Title" {
		t.Fatalf("metadata title = %+v, want Pointer Title", changes[0].Title)
	}
	if changes[0].Category == nil || changes[0].Category.ID != "game-pointer" || changes[0].Category.Name != "Pointer Game" {
		t.Fatalf("metadata category = %+v, want game-pointer/Pointer Game", changes[0].Category)
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

// TestCloseStaleStream_EndsRowWithoutPublishingOffline pins the live-poller
// rerun path: CloseStaleStream stamps ended_at on the open stream row but must
// NOT publish a StreamStatus offline (the broadcaster is still live under a new
// stream ID, so the live-dot must not flicker). Also covers idempotency on an
// already-ended row and the no-op for a broadcaster with no stream.
func TestCloseStaleStream_EndsRowWithoutPublishingOffline(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	bus := eventbus.New()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := NewEventProcessor(repo, nil, nil, nil, bus, log)

	sub := bus.StreamStatus.Subscribe(ctx)
	assertNoStreamStatus := func(when string) {
		t.Helper()
		select {
		case ev := <-sub:
			t.Fatalf("CloseStaleStream published a StreamStatus event %s: %+v", when, ev)
		case <-time.After(150 * time.Millisecond):
		}
	}

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "b-stale", BroadcasterLogin: "bs", BroadcasterName: "BS",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	if _, err := repo.UpsertStream(ctx, &repository.StreamInput{
		ID: "s-stale", BroadcasterID: "b-stale", Type: "live", Language: "en",
		ViewerCount: 3, StartedAt: time.Now().UTC().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("seed stream: %v", err)
	}

	if err := p.CloseStaleStream(ctx, "b-stale"); err != nil {
		t.Fatalf("CloseStaleStream: %v", err)
	}
	ended, err := repo.GetLastLiveStream(ctx, "b-stale")
	if err != nil {
		t.Fatalf("GetLastLiveStream: %v", err)
	}
	if ended.EndedAt == nil {
		t.Fatal("ended_at = nil after CloseStaleStream, want stamped")
	}
	assertNoStreamStatus("on the close")

	// Idempotent: a second close on the already-ended row is a no-op and must
	// not move ended_at or publish.
	firstEnded := *ended.EndedAt
	if err := p.CloseStaleStream(ctx, "b-stale"); err != nil {
		t.Fatalf("second CloseStaleStream: %v", err)
	}
	again, err := repo.GetLastLiveStream(ctx, "b-stale")
	if err != nil {
		t.Fatalf("GetLastLiveStream after second close: %v", err)
	}
	if again.EndedAt == nil || !again.EndedAt.Equal(firstEnded) {
		t.Fatalf("ended_at moved on idempotent re-close: got %v, want %v", again.EndedAt, firstEnded)
	}
	assertNoStreamStatus("on the idempotent re-close")

	// No stream row for the broadcaster: clean no-op.
	if err := p.CloseStaleStream(ctx, "b-never-live"); err != nil {
		t.Fatalf("CloseStaleStream(missing broadcaster) = %v, want nil", err)
	}
	assertNoStreamStatus("for a broadcaster with no stream")
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

// TestDispatchStreamOnline_ErrBusyIsIdempotent pins that an already-recording
// broadcaster (dl.Start returns downloader.ErrBusy) is a satisfied no-op:
// DispatchStreamOnline returns nil, so the live poller advances its lastLive
// state instead of re-dispatching the same broadcaster every tick. A non-ErrBusy
// failure must still propagate.
func TestDispatchStreamOnline_ErrBusyIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	if _, err := repo.UpsertUser(ctx, &repository.User{ID: "u-1", Login: "u1", DisplayName: "U1", Role: "owner"}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "b-1", BroadcasterLogin: "b1", BroadcasterName: "B1"}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	// A bare schedule (no filters) matches any stream.online for the broadcaster,
	// so dispatch reaches dl.Start.
	if _, err := repo.CreateSchedule(ctx, &repository.ScheduleInput{BroadcasterID: "b-1", RequestedBy: "u-1", Quality: "HIGH"}); err != nil {
		t.Fatalf("seed schedule: %v", err)
	}

	event := twitch.StreamOnlineEvent{
		ID: "s-1", BroadcasterUserID: "b-1", BroadcasterUserLogin: "b1", BroadcasterUserName: "B1", Type: "live",
	}

	busy := &fakeDownloader{startErr: downloader.ErrBusy}
	p := NewEventProcessor(repo, busy, nil, nil, nil, log)
	if err := p.DispatchStreamOnline(ctx, event); err != nil {
		t.Fatalf("DispatchStreamOnline(ErrBusy) = %v, want nil", err)
	}
	if busy.calls != 1 {
		t.Fatalf("dl.Start calls = %d, want 1", busy.calls)
	}

	boom := errors.New("disk on fire")
	failing := &fakeDownloader{startErr: boom}
	p2 := NewEventProcessor(repo, failing, nil, nil, nil, log)
	if err := p2.DispatchStreamOnline(ctx, event); !errors.Is(err, boom) {
		t.Fatalf("DispatchStreamOnline(generic error) = %v, want %v", err, boom)
	}
}

// TestDispatchStreamOnlineFromStream_MatchesFilteredScheduleWithoutHelix pins
// the poll-mode fix end-to-end: a category-filtered schedule must match off the
// already-polled stream, with no second Helix GetStreams. The hydrator gets a
// nil Twitch client, so Hydrate (the re-fetch path) returns an empty snapshot —
// meaning the filtered schedule can ONLY match via the prefetched stream. The
// webhook entry (no prefetch) is the contrast: it can't match without a fetch,
// which is the exact gap the prefetch path closes.
func TestDispatchStreamOnlineFromStream_MatchesFilteredScheduleWithoutHelix(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	if _, err := repo.UpsertUser(ctx, &repository.User{ID: "u-1", Login: "u1", DisplayName: "U1", Role: "owner"}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "b-1", BroadcasterLogin: "b1", BroadcasterName: "B1"}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	if _, err := repo.UpsertCategory(ctx, &repository.Category{ID: "game-42", Name: "Game 42"}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	sched, err := repo.CreateSchedule(ctx, &repository.ScheduleInput{
		BroadcasterID: "b-1", RequestedBy: "u-1", Quality: "HIGH", HasCategories: true,
	})
	if err != nil {
		t.Fatalf("seed schedule: %v", err)
	}
	if err := repo.LinkScheduleCategory(ctx, sched.ID, "game-42"); err != nil {
		t.Fatalf("link schedule category: %v", err)
	}

	// nil Twitch client: the re-fetch path (Hydrate) returns an empty snapshot.
	hydrator := streammeta.NewHydrator(repo, nil, streammeta.Config{}, log)

	stream := twitch.Stream{
		ID: "s-1", UserID: "b-1", UserLogin: "b1", UserName: "B1",
		Type: "live", GameID: "game-42", GameName: "Game 42",
	}

	pollDL := &fakeDownloader{}
	pPoll := NewEventProcessor(repo, pollDL, nil, hydrator, nil, log)
	if err := pPoll.DispatchStreamOnlineFromStream(ctx, stream); err != nil {
		t.Fatalf("DispatchStreamOnlineFromStream: %v", err)
	}
	if pollDL.calls != 1 {
		t.Fatalf("poll dl.Start calls = %d, want 1 (filtered schedule must match off the prefetched stream, no Helix fetch)", pollDL.calls)
	}

	webhookDL := &fakeDownloader{}
	pWebhook := NewEventProcessor(repo, webhookDL, nil, hydrator, nil, log)
	if err := pWebhook.DispatchStreamOnline(ctx, twitch.StreamOnlineEvent{
		ID: "s-1", BroadcasterUserID: "b-1", BroadcasterUserLogin: "b1", BroadcasterUserName: "B1", Type: "live",
	}); err != nil {
		t.Fatalf("DispatchStreamOnline: %v", err)
	}
	if webhookDL.calls != 0 {
		t.Fatalf("webhook dl.Start calls = %d, want 0 (no prefetch + no Helix → filtered schedule cannot match)", webhookDL.calls)
	}
}

// TestDispatchStreamOnline_MultiMatchTriggersOnlyWinnerButCountsAll pins the
// spec's multi-match rule: when several unfiltered schedules match, exactly one
// download starts (the highest quality), yet every matching schedule's
// trigger_count is bumped so the dashboard shows each one fired, not just the
// winner.
func TestDispatchStreamOnline_MultiMatchTriggersOnlyWinnerButCountsAll(t *testing.T) {
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// download_schedules is UNIQUE on (broadcaster_id, requested_by), so the two
	// schedules for the same broadcaster must come from different requesters.
	for _, id := range []string{"u-low", "u-high"} {
		if _, err := repo.UpsertUser(ctx, &repository.User{ID: id, Login: id, DisplayName: id, Role: "owner"}); err != nil {
			t.Fatalf("seed user %s: %v", id, err)
		}
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "b-1", BroadcasterLogin: "b1", BroadcasterName: "B1"}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	// Two unfiltered schedules (match any online) of different quality. LOW is
	// created first so a "first caller wins" bug would pick it; the winner must
	// be HIGH regardless of order.
	low, err := repo.CreateSchedule(ctx, &repository.ScheduleInput{BroadcasterID: "b-1", RequestedBy: "u-low", Quality: repository.QualityLow})
	if err != nil {
		t.Fatalf("seed low schedule: %v", err)
	}
	high, err := repo.CreateSchedule(ctx, &repository.ScheduleInput{BroadcasterID: "b-1", RequestedBy: "u-high", Quality: repository.QualityHigh})
	if err != nil {
		t.Fatalf("seed high schedule: %v", err)
	}

	dl := &fakeDownloader{}
	p := NewEventProcessor(repo, dl, nil, nil, nil, log)
	if err := p.DispatchStreamOnline(ctx, twitch.StreamOnlineEvent{
		ID: "s-1", BroadcasterUserID: "b-1", BroadcasterUserLogin: "b1", BroadcasterUserName: "B1", Type: "live",
	}); err != nil {
		t.Fatalf("DispatchStreamOnline: %v", err)
	}

	if dl.calls != 1 {
		t.Fatalf("dl.Start calls = %d, want exactly 1 (one download for multiple matches)", dl.calls)
	}
	if dl.lastParams.Quality != repository.QualityHigh {
		t.Fatalf("winner quality reaching Start = %q, want HIGH", dl.lastParams.Quality)
	}

	for _, want := range []struct {
		name string
		id   int64
	}{{"low", low.ID}, {"high", high.ID}} {
		got, err := repo.GetSchedule(ctx, want.id)
		if err != nil {
			t.Fatalf("GetSchedule(%s): %v", want.name, err)
		}
		if got.TriggerCount != 1 {
			t.Fatalf("%s schedule trigger_count = %d, want 1 (every match counts, not just the winner)", want.name, got.TriggerCount)
		}
		if got.LastTriggeredAt == nil {
			t.Fatalf("%s schedule last_triggered_at = nil, want stamped", want.name)
		}
	}
}

// TestDispatchStreamOnline_FilterLoadErrorDoesNotPoisonSuccessfulDispatch pins
// the webhook/poller contract: once at least one schedule matches and the
// download starts successfully, an unrelated schedule's filter-load failure is
// only a logged best-effort error. Returning it would make the webhook record a
// false FAILED event and make the live poller re-dispatch the same stream every
// tick because lastLive never advances.
func TestDispatchStreamOnline_FilterLoadErrorDoesNotPoisonSuccessfulDispatch(t *testing.T) {
	ctx := context.Background()
	realRepo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	for _, id := range []string{"u-winner", "u-broken"} {
		if _, err := realRepo.UpsertUser(ctx, &repository.User{ID: id, Login: id, DisplayName: id, Role: "owner"}); err != nil {
			t.Fatalf("seed user %s: %v", id, err)
		}
	}
	if _, err := realRepo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "b-1", BroadcasterLogin: "b1", BroadcasterName: "B1"}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	winner, err := realRepo.CreateSchedule(ctx, &repository.ScheduleInput{
		BroadcasterID: "b-1",
		RequestedBy:   "u-winner",
		Quality:       repository.QualityHigh,
	})
	if err != nil {
		t.Fatalf("seed winner schedule: %v", err)
	}
	broken, err := realRepo.CreateSchedule(ctx, &repository.ScheduleInput{
		BroadcasterID: "b-1",
		RequestedBy:   "u-broken",
		Quality:       repository.QualityLow,
		HasCategories: true,
	})
	if err != nil {
		t.Fatalf("seed broken schedule: %v", err)
	}

	filterErr := errors.New("category lookup failed")
	repo := &categoryFilterFailRepo{Repository: realRepo, scheduleID: broken.ID, err: filterErr}
	dl := &fakeDownloader{}
	p := NewEventProcessor(repo, dl, nil, nil, nil, log)

	err = p.DispatchStreamOnline(ctx, twitch.StreamOnlineEvent{
		ID: "s-1", BroadcasterUserID: "b-1", BroadcasterUserLogin: "b1", BroadcasterUserName: "B1", Type: "live",
	})
	if err != nil {
		t.Fatalf("DispatchStreamOnline() error = %v, want nil after successful download start", err)
	}
	if dl.calls != 1 {
		t.Fatalf("dl.Start calls = %d, want 1", dl.calls)
	}
	gotWinner, err := realRepo.GetSchedule(ctx, winner.ID)
	if err != nil {
		t.Fatalf("GetSchedule(winner): %v", err)
	}
	if gotWinner.TriggerCount != 1 {
		t.Fatalf("winner trigger_count = %d, want 1", gotWinner.TriggerCount)
	}
	gotBroken, err := realRepo.GetSchedule(ctx, broken.ID)
	if err != nil {
		t.Fatalf("GetSchedule(broken): %v", err)
	}
	if gotBroken.TriggerCount != 0 {
		t.Fatalf("broken schedule trigger_count = %d, want 0 because its filters did not load", gotBroken.TriggerCount)
	}
}
