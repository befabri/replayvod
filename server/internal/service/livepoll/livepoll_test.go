package livepoll

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/service/schedule"
	"github.com/befabri/replayvod/server/internal/service/streammeta"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

type fakeRepo struct {
	channels         []repository.Channel
	activeStreams    []repository.Stream
	channelsErr      error
	activeStreamsErr error
	// beforeActive runs at the top of ListActiveStreams; a non-nil result is
	// returned as the read's error.
	beforeActive func(ctx context.Context) error
}

func (r *fakeRepo) ListChannels(context.Context) ([]repository.Channel, error) {
	if r.channelsErr != nil {
		return nil, r.channelsErr
	}
	return append([]repository.Channel(nil), r.channels...), nil
}

func (r *fakeRepo) ListActiveStreams(ctx context.Context) ([]repository.Stream, error) {
	if r.beforeActive != nil {
		if err := r.beforeActive(ctx); err != nil {
			return nil, err
		}
	}
	if r.activeStreamsErr != nil {
		return nil, r.activeStreamsErr
	}
	return append([]repository.Stream(nil), r.activeStreams...), nil
}

type fakeTwitch struct {
	mu       sync.Mutex
	streams  []twitch.Stream
	batches  [][]string
	calls    int
	pageSize int      // 0 = every match in a single page
	err      error    // returned from every GetStreams call when set
	ticked   chan int // optional: receives the call number on each GetStreams
	// echoCursor, when set, is returned as the cursor of every page: the
	// stalled-pagination shape. freshCursor returns a new cursor on every
	// page instead, so the drain never ends on its own.
	echoCursor  string
	freshCursor bool
	// beforeReply runs with the call number before any reply; a non-nil
	// result is returned as the call's error.
	beforeReply func(ctx context.Context, call int) error
}

func (f *fakeTwitch) GetStreams(ctx context.Context, params *twitch.GetStreamsParams) ([]twitch.Stream, twitch.Pagination, error) {
	f.mu.Lock()
	f.calls++
	call := f.calls
	if params.After == "" {
		// Record the batch once per 100-ID batch (the first page).
		f.batches = append(f.batches, append([]string(nil), params.UserID...))
	}
	f.mu.Unlock()
	if f.ticked != nil {
		select {
		case f.ticked <- call:
		default:
		}
	}
	if f.beforeReply != nil {
		if err := f.beforeReply(ctx, call); err != nil {
			return nil, twitch.Pagination{}, err
		}
	}
	if f.err != nil {
		return nil, twitch.Pagination{}, f.err
	}
	if f.echoCursor != "" {
		return nil, twitch.Pagination{Cursor: f.echoCursor}, nil
	}
	if f.freshCursor {
		return nil, twitch.Pagination{Cursor: strconv.Itoa(call)}, nil
	}

	allowed := make(map[string]bool, len(params.UserID))
	for _, id := range params.UserID {
		allowed[id] = true
	}
	var matching []twitch.Stream
	for _, stream := range f.streams {
		if allowed[stream.UserID] {
			matching = append(matching, stream)
		}
	}

	if f.pageSize <= 0 || len(matching) <= f.pageSize {
		return matching, twitch.Pagination{}, nil
	}
	// Paginate: the cursor encodes the offset into the matching slice.
	offset := 0
	if params.After != "" {
		offset, _ = strconv.Atoi(params.After)
	}
	end := offset + f.pageSize
	if end >= len(matching) {
		return matching[offset:], twitch.Pagination{}, nil
	}
	return matching[offset:end], twitch.Pagination{Cursor: strconv.Itoa(end)}, nil
}

type fakeProcessor struct {
	online     []twitch.Stream
	offline    []twitch.StreamOfflineEvent
	closeStale []string

	onlineErr     error
	offlineErr    error
	closeStaleErr error

	onlineErrByID     map[string]error
	offlineErrByID    map[string]error
	closeStaleErrByID map[string]error
}

func (p *fakeProcessor) DispatchStreamOnlineFromStream(_ context.Context, stream twitch.Stream) error {
	if err := p.onlineErrByID[stream.UserID]; err != nil {
		return err
	}
	if p.onlineErr != nil {
		return p.onlineErr
	}
	p.online = append(p.online, stream)
	return nil
}

func (p *fakeProcessor) DispatchStreamOffline(_ context.Context, event twitch.StreamOfflineEvent) error {
	if err := p.offlineErrByID[event.BroadcasterUserID]; err != nil {
		return err
	}
	if p.offlineErr != nil {
		return p.offlineErr
	}
	p.offline = append(p.offline, event)
	return nil
}

func (p *fakeProcessor) CloseStaleStream(_ context.Context, broadcasterID string) error {
	if err := p.closeStaleErrByID[broadcasterID]; err != nil {
		return err
	}
	if p.closeStaleErr != nil {
		return p.closeStaleErr
	}
	p.closeStale = append(p.closeStale, broadcasterID)
	return nil
}

func TestTickSeedsActiveStreamAndDoesNotRefireSameStream(t *testing.T) {
	ctx := context.Background()
	started := time.Now().UTC()
	repo := &fakeRepo{
		channels: []repository.Channel{channel("b-1")},
		activeStreams: []repository.Stream{{
			ID:            "s-1",
			BroadcasterID: "b-1",
		}},
	}
	tw := &fakeTwitch{streams: []twitch.Stream{stream("s-1", "b-1", started)}}
	proc := &fakeProcessor{}
	svc := New(repo, tw, proc, time.Minute, nil)

	if err := svc.tick(ctx); err != nil {
		t.Fatalf("tick = %v, want nil", err)
	}
	if len(proc.online) != 0 || len(proc.offline) != 0 {
		t.Fatalf("dispatches online=%v offline=%v, want none", proc.online, proc.offline)
	}
	if got := svc.lastLive["b-1"].streamID; got != "s-1" {
		t.Fatalf("lastLive[b-1].streamID = %q, want s-1", got)
	}
}

func TestTickDispatchesOnlineAndOfflineDiff(t *testing.T) {
	ctx := context.Background()
	started := time.Now().UTC()
	repo := &fakeRepo{
		channels: []repository.Channel{channel("b-1"), channel("b-2")},
		activeStreams: []repository.Stream{{
			ID:            "old-s-2",
			BroadcasterID: "b-2",
		}},
	}
	tw := &fakeTwitch{streams: []twitch.Stream{stream("new-s-1", "b-1", started)}}
	proc := &fakeProcessor{}
	svc := New(repo, tw, proc, time.Minute, nil)

	if err := svc.tick(ctx); err != nil {
		t.Fatalf("tick = %v, want nil", err)
	}
	if len(proc.online) != 1 || proc.online[0].UserID != "b-1" || proc.online[0].ID != "new-s-1" {
		t.Fatalf("online dispatches = %+v, want b-1/new-s-1", proc.online)
	}
	if len(proc.offline) != 1 || proc.offline[0].BroadcasterUserID != "b-2" {
		t.Fatalf("offline dispatches = %+v, want b-2", proc.offline)
	}
	// The offline event for b-2 must carry the channel mirror's login/name.
	if proc.offline[0].BroadcasterUserLogin != "b-2-login" || proc.offline[0].BroadcasterUserName != "b-2-name" {
		t.Fatalf("offline identity = %q/%q, want b-2-login/b-2-name", proc.offline[0].BroadcasterUserLogin, proc.offline[0].BroadcasterUserName)
	}
	if _, ok := svc.lastLive["b-2"]; ok {
		t.Fatal("lastLive still contains b-2 after offline dispatch")
	}
}

// TestTickStreamIDChangeClosesOldStreamBeforeOpeningNew pins the rerun /
// offline-blip case: a broadcaster stays live across a poll boundary but under a
// new stream ID. The old streams row must be retired via CloseStaleStream (which
// stamps ended_at WITHOUT an SSE offline, so the live-dot doesn't flicker)
// before the new stream opens; no stream.offline must be dispatched.
func TestTickStreamIDChangeClosesOldStreamBeforeOpeningNew(t *testing.T) {
	ctx := context.Background()
	started := time.Now().UTC()
	repo := &fakeRepo{
		channels: []repository.Channel{channel("b-1")},
		activeStreams: []repository.Stream{{
			ID:            "s-old",
			BroadcasterID: "b-1",
		}},
	}
	tw := &fakeTwitch{streams: []twitch.Stream{stream("s-new", "b-1", started)}}
	proc := &fakeProcessor{}
	svc := New(repo, tw, proc, time.Minute, nil)

	if err := svc.tick(ctx); err != nil {
		t.Fatalf("tick = %v, want nil", err)
	}
	if len(proc.closeStale) != 1 || proc.closeStale[0] != "b-1" {
		t.Fatalf("closeStale calls = %+v, want one for b-1", proc.closeStale)
	}
	if len(proc.offline) != 0 {
		t.Fatalf("offline dispatches = %+v, want none (no live-dot flap on rerun)", proc.offline)
	}
	if len(proc.online) != 1 || proc.online[0].ID != "s-new" {
		t.Fatalf("online dispatches = %+v, want one for s-new", proc.online)
	}
	if got := svc.lastLive["b-1"].streamID; got != "s-new" {
		t.Fatalf("lastLive[b-1].streamID = %q, want s-new", got)
	}
}

func TestTickBatchesGetStreamsAtHundredBroadcasters(t *testing.T) {
	ctx := context.Background()
	var channels []repository.Channel
	for i := 0; i < 101; i++ {
		channels = append(channels, channel(fmt.Sprintf("b-%03d", i)))
	}
	tw := &fakeTwitch{}
	svc := New(&fakeRepo{channels: channels}, tw, &fakeProcessor{}, time.Minute, nil)

	if err := svc.tick(ctx); err != nil {
		t.Fatalf("tick = %v, want nil", err)
	}
	if len(tw.batches) != 2 {
		t.Fatalf("GetStreams batches = %d, want 2", len(tw.batches))
	}
	if len(tw.batches[0]) != 100 || len(tw.batches[1]) != 1 {
		t.Fatalf("batch sizes = %d,%d; want 100,1", len(tw.batches[0]), len(tw.batches[1]))
	}
}

// TestCollectBroadcastersDedupesAndSkipsBlankIDs pins the channel-set reduction
// that feeds the Helix query: rows without a broadcaster ID are dropped, a
// repeated ID keeps the first channel seen and is queried only once, and the ID
// list preserves first-seen order.
func TestCollectBroadcastersDedupesAndSkipsBlankIDs(t *testing.T) {
	channels := []repository.Channel{
		{BroadcasterID: "b-1", BroadcasterLogin: "first"},
		{BroadcasterID: ""}, // no ID: must be skipped, not queried as ""
		{BroadcasterID: "b-2", BroadcasterLogin: "two"},
		{BroadcasterID: "b-1", BroadcasterLogin: "dup"}, // duplicate: first wins, not re-listed
	}

	byID, ids := collectBroadcasters(channels)

	want := []string{"b-1", "b-2"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i, id := range want {
		if ids[i] != id {
			t.Fatalf("ids = %v, want %v (first-seen order)", ids, want)
		}
	}
	if _, ok := byID[""]; ok {
		t.Fatal("blank broadcaster ID leaked into the lookup")
	}
	if byID["b-1"].BroadcasterLogin != "first" {
		t.Fatalf("byID[b-1].login = %q, want first (first channel wins on dup)", byID["b-1"].BroadcasterLogin)
	}
}

// TestFetchLiveDrainsPaginationAcrossPages pins that a single 100-ID batch with
// more simultaneously-live broadcasters than fit on one Helix page is fully
// drained via the cursor. Without First=100 + cursor draining only the first
// page would be seen, and the unseen-but-live tail would later be reported
// offline.
func TestFetchLiveDrainsPaginationAcrossPages(t *testing.T) {
	ctx := context.Background()
	started := time.Now().UTC()
	const liveCount = 25
	var channels []repository.Channel
	var streams []twitch.Stream
	for i := 0; i < liveCount; i++ {
		id := fmt.Sprintf("b-%02d", i)
		channels = append(channels, channel(id))
		streams = append(streams, stream("s-"+id, id, started))
	}
	tw := &fakeTwitch{streams: streams, pageSize: 10}
	proc := &fakeProcessor{}
	svc := New(&fakeRepo{channels: channels}, tw, proc, time.Minute, nil)

	if err := svc.tick(ctx); err != nil {
		t.Fatalf("tick = %v, want nil", err)
	}
	if len(proc.online) != liveCount {
		t.Fatalf("online dispatches = %d, want %d (pagination tail must not be dropped)", len(proc.online), liveCount)
	}
	if tw.calls != 3 {
		t.Fatalf("GetStreams calls = %d, want 3 (10+10+5 paged)", tw.calls)
	}
	if len(svc.lastLive) != liveCount {
		t.Fatalf("lastLive size = %d, want %d", len(svc.lastLive), liveCount)
	}
}

// TestTickGetStreamsErrorAbortsTickWithoutDispatch pins that a Helix failure
// fails the tick cleanly: nothing is dispatched and lastLive is untouched so a
// transient error can't be read as everyone going offline.
func TestTickGetStreamsErrorAbortsTickWithoutDispatch(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{channels: []repository.Channel{channel("b-1")}}
	tw := &fakeTwitch{err: errors.New("helix 503")}
	proc := &fakeProcessor{}
	svc := New(repo, tw, proc, time.Minute, nil)
	svc.lastLive["b-1"] = liveStream{streamID: "s-1"}
	svc.seeded = true

	if err := svc.tick(ctx); err == nil {
		t.Fatal("tick = nil, want GetStreams error")
	}
	if len(proc.online) != 0 || len(proc.offline) != 0 {
		t.Fatalf("dispatches online=%v offline=%v, want none on Helix error", proc.online, proc.offline)
	}
	if svc.lastLive["b-1"].streamID != "s-1" {
		t.Fatal("lastLive mutated on a failed tick")
	}
}

// TestTickCloseStaleStreamFailureKeepsLastLive pins the dispatch-error
// accumulation contract: when CloseStaleStream fails for a rerun, the old
// lastLive entry must survive (so the retry can close it next tick) and the new
// stream must NOT be opened. The tick returns the error.
func TestTickCloseStaleStreamFailureKeepsLastLive(t *testing.T) {
	ctx := context.Background()
	started := time.Now().UTC()
	repo := &fakeRepo{
		channels:      []repository.Channel{channel("b-1")},
		activeStreams: []repository.Stream{{ID: "s-old", BroadcasterID: "b-1"}},
	}
	tw := &fakeTwitch{streams: []twitch.Stream{stream("s-new", "b-1", started)}}
	proc := &fakeProcessor{closeStaleErr: errors.New("end stream failed")}
	svc := New(repo, tw, proc, time.Minute, nil)

	if err := svc.tick(ctx); err == nil {
		t.Fatal("tick = nil, want joined CloseStaleStream error")
	}
	if len(proc.online) != 0 {
		t.Fatalf("online dispatches = %+v, want none (stale close must precede open)", proc.online)
	}
	if got := svc.lastLive["b-1"].streamID; got != "s-old" {
		t.Fatalf("lastLive[b-1].streamID = %q, want s-old preserved for retry", got)
	}
}

// TestTickOnlineDispatchErrorDoesNotSkipOtherBroadcasters pins the
// per-broadcaster isolation contract: one failed stream.online dispatch is
// returned from tick, but other newly-live broadcasters still dispatch and
// advance lastLive.
func TestTickOnlineDispatchErrorDoesNotSkipOtherBroadcasters(t *testing.T) {
	ctx := context.Background()
	started := time.Now().UTC()
	repo := &fakeRepo{channels: []repository.Channel{channel("b-1"), channel("b-2")}}
	tw := &fakeTwitch{streams: []twitch.Stream{
		stream("s-1", "b-1", started),
		stream("s-2", "b-2", started),
	}}
	boom := errors.New("online failed")
	proc := &fakeProcessor{onlineErrByID: map[string]error{"b-1": boom}}
	svc := New(repo, tw, proc, time.Minute, nil)

	err := svc.tick(ctx)
	if !errors.Is(err, boom) {
		t.Fatalf("tick = %v, want joined online error", err)
	}
	if len(proc.online) != 1 || proc.online[0].UserID != "b-2" {
		t.Fatalf("online dispatches = %+v, want only b-2 success", proc.online)
	}
	if got := svc.lastLive["b-1"]; got.streamID != "s-1" || !got.pendingDispatch {
		t.Fatalf("failed online dispatch was not retained for retry: %+v", got)
	}
	if got := svc.lastLive["b-2"].streamID; got != "s-2" {
		t.Fatalf("lastLive[b-2].streamID = %q, want s-2", got)
	}
	proc.onlineErrByID = nil
	if err := svc.tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(proc.online) != 2 || proc.online[1].UserID != "b-1" || len(proc.closeStale) != 0 {
		t.Fatalf("retry did not dispatch only the pending stream: %+v, closed=%v", proc.online, proc.closeStale)
	}
}

// TestTickOfflineDispatchErrorDoesNotSkipOtherBroadcasters is the offline
// mirror: a failed offline dispatch leaves that broadcaster in lastLive for a
// retry, while other offline broadcasters are removed after a successful
// dispatch.
func TestTickOfflineDispatchErrorDoesNotSkipOtherBroadcasters(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{
		channels: []repository.Channel{channel("b-1"), channel("b-2")},
		activeStreams: []repository.Stream{
			{ID: "s-1", BroadcasterID: "b-1"},
			{ID: "s-2", BroadcasterID: "b-2"},
		},
	}
	boom := errors.New("offline failed")
	proc := &fakeProcessor{offlineErrByID: map[string]error{"b-1": boom}}
	svc := New(repo, &fakeTwitch{}, proc, time.Minute, nil)

	err := svc.tick(ctx)
	if !errors.Is(err, boom) {
		t.Fatalf("tick = %v, want joined offline error", err)
	}
	if len(proc.offline) != 1 || proc.offline[0].BroadcasterUserID != "b-2" {
		t.Fatalf("offline dispatches = %+v, want only b-2 success", proc.offline)
	}
	if got := svc.lastLive["b-1"].streamID; got != "s-1" {
		t.Fatalf("lastLive[b-1].streamID = %q, want s-1 retained for retry", got)
	}
	if _, ok := svc.lastLive["b-2"]; ok {
		t.Fatal("lastLive still contains b-2 after successful offline dispatch")
	}
}

// TestTickOfflineFallsBackToCachedIdentity pins the fix for blank offline
// identity: a broadcaster observed live carries its login/name forward, so if
// the channel mirror row disappears before it goes offline the SSE delta still
// names the channel instead of emitting an ID-only event.
func TestTickOfflineFallsBackToCachedIdentity(t *testing.T) {
	ctx := context.Background()
	started := time.Now().UTC()
	repo := &fakeRepo{
		channels: []repository.Channel{channel("b-1")},
	}
	tw := &fakeTwitch{streams: []twitch.Stream{stream("s-1", "b-1", started)}}
	proc := &fakeProcessor{}
	svc := New(repo, tw, proc, time.Minute, nil)

	// Tick 1: b-1 goes live; identity is cached from the live stream.
	if err := svc.tick(ctx); err != nil {
		t.Fatalf("tick 1 = %v, want nil", err)
	}
	if len(proc.online) != 1 {
		t.Fatalf("tick 1 online = %+v, want one", proc.online)
	}

	// Tick 2: the channel mirror row is gone and b-1 is no longer live.
	repo.channels = nil
	tw.streams = nil
	if err := svc.tick(ctx); err != nil {
		t.Fatalf("tick 2 = %v, want nil", err)
	}
	if len(proc.offline) != 1 {
		t.Fatalf("tick 2 offline = %+v, want one", proc.offline)
	}
	off := proc.offline[0]
	if off.BroadcasterUserID != "b-1" {
		t.Fatalf("offline broadcaster = %q, want b-1", off.BroadcasterUserID)
	}
	if off.BroadcasterUserLogin != "b-1-login" || off.BroadcasterUserName != "b-1-name" {
		t.Fatalf("offline identity = %q/%q, want cached b-1-login/b-1-name", off.BroadcasterUserLogin, off.BroadcasterUserName)
	}
}

// TestTickSeedFailureReturnsErrorThenRecovers pins that a seed failure fails the
// tick (leaving detection un-seeded so no spurious transitions fire) and that a
// later successful seed clears the failure state.
func TestTickSeedFailureReturnsErrorThenRecovers(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{
		channels:         []repository.Channel{channel("b-1")},
		activeStreams:    []repository.Stream{{ID: "s-1", BroadcasterID: "b-1"}},
		activeStreamsErr: errors.New("db unavailable"),
	}
	tw := &fakeTwitch{}
	proc := &fakeProcessor{}
	svc := New(repo, tw, proc, time.Minute, nil)

	if err := svc.tick(ctx); err == nil {
		t.Fatal("tick = nil, want seed error")
	}
	if svc.seeded {
		t.Fatal("seeded = true after a failed seed; want false so seeding retries")
	}
	if tw.calls != 0 {
		t.Fatal("GetStreams called despite a failed seed; want the tick to abort before fetch")
	}

	repo.activeStreamsErr = nil
	if err := svc.tick(ctx); err != nil {
		t.Fatalf("recovery tick = %v, want nil", err)
	}
	if !svc.seeded {
		t.Fatal("seeded = false after a successful seed")
	}
	if svc.seedFailures != 0 {
		t.Fatalf("seedFailures = %d after recovery, want 0", svc.seedFailures)
	}
}

// TestSeedFailureEscalatesToErrorLog pins that repeated seed failures stop being
// silent: after seedFailureEscalation consecutive misses the poller logs at
// error level so a persistently unreadable store is visible.
func TestSeedFailureEscalatesToErrorLog(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	repo := &fakeRepo{activeStreamsErr: errors.New("db unavailable")}
	svc := New(repo, &fakeTwitch{}, &fakeProcessor{}, time.Minute, log)

	for i := 0; i < seedFailureEscalation; i++ {
		if err := svc.tick(ctx); err == nil {
			t.Fatalf("tick %d = nil, want seed error", i+1)
		}
	}
	if !bytes.Contains(buf.Bytes(), []byte("level=ERROR")) {
		t.Fatalf("no error-level log after %d seed failures; got:\n%s", seedFailureEscalation, buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("stalled")) {
		t.Fatalf("escalated log missing the stall message; got:\n%s", buf.String())
	}
}

// TestRunTicksImmediatelyAndStopsOnCancel pins the poller lifecycle: Run fires
// one tick immediately (not after a full interval), keeps ticking on the
// interval, and returns promptly when its context is cancelled.
func TestRunTicksImmediatelyAndStopsOnCancel(t *testing.T) {
	started := time.Now().UTC()
	repo := &fakeRepo{channels: []repository.Channel{channel("b-1")}}
	tw := &fakeTwitch{streams: []twitch.Stream{stream("s-1", "b-1", started)}, ticked: make(chan int, 64)}
	svc := New(repo, tw, &fakeProcessor{}, 20*time.Millisecond, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.Run(ctx)
		close(done)
	}()

	// Immediate tick.
	select {
	case <-tw.ticked:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not tick immediately")
	}
	// Interval re-tick.
	select {
	case <-tw.ticked:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not tick again on the interval")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

// TestRunDisabledWhenDependencyMissing pins that Run returns immediately (rather
// than blocking forever) when a required dependency is nil.
func TestRunDisabledWhenDependencyMissing(t *testing.T) {
	svc := New(&fakeRepo{}, &fakeTwitch{}, nil, time.Minute, nil)
	done := make(chan struct{})
	go func() {
		svc.Run(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run with a nil dependency did not return")
	}
}

func channel(id string) repository.Channel {
	return repository.Channel{
		BroadcasterID:    id,
		BroadcasterLogin: id + "-login",
		BroadcasterName:  id + "-name",
	}
}

func stream(id, broadcasterID string, startedAt time.Time) twitch.Stream {
	return twitch.Stream{
		ID:        id,
		UserID:    broadcasterID,
		UserLogin: broadcasterID + "-login",
		UserName:  broadcasterID + "-name",
		Type:      "live",
		StartedAt: startedAt,
	}
}

// assertQuietLog fails when the captured log carries anything at warn level or
// above.
func assertQuietLog(t *testing.T, buf *bytes.Buffer) {
	t.Helper()
	for _, level := range []string{"level=WARN", "level=ERROR"} {
		if bytes.Contains(buf.Bytes(), []byte(level)) {
			t.Fatalf("unexpected %s entry:\n%s", level, buf.String())
		}
	}
}

// seededService returns a poller whose baseline already holds b-1 live, so a
// tick that wrongly treats a partial or failed fetch as authoritative would
// dispatch a spurious stream.offline for it.
func seededService(tw *fakeTwitch, proc *fakeProcessor, log *slog.Logger) *Service {
	repo := &fakeRepo{
		channels:      []repository.Channel{channel("b-1")},
		activeStreams: []repository.Stream{{ID: "s-1", BroadcasterID: "b-1"}},
	}
	return New(repo, tw, proc, time.Minute, log)
}

// TestFetchLiveRepeatedCursorFailsTheTick pins the progress guard: a Helix
// reply that echoes the cursor it was given ends the tick with an error on
// its first reuse instead of draining forever, and nothing is dispatched
// from the partial page set.
func TestFetchLiveRepeatedCursorFailsTheTick(t *testing.T) {
	tw := &fakeTwitch{echoCursor: "same"}
	proc := &fakeProcessor{}
	svc := seededService(tw, proc, nil)

	err := svc.tick(context.Background())
	if !errors.Is(err, twitch.ErrPaginationStalled) {
		t.Fatalf("tick = %v, want ErrPaginationStalled", err)
	}
	if tw.calls != 2 {
		t.Fatalf("GetStreams calls = %d, want 2 (stall detected on the cursor's first reuse)", tw.calls)
	}
	if len(proc.online) != 0 || len(proc.offline) != 0 || len(proc.closeStale) != 0 {
		t.Fatalf("dispatched online=%d offline=%d closeStale=%d after a stalled drain; want none", len(proc.online), len(proc.offline), len(proc.closeStale))
	}
	if _, live := svc.lastLive["b-1"]; !live {
		t.Fatal("lastLive lost b-1 after a failed fetch")
	}
}

// TestFetchLivePageCapFailsTheTick pins the second half of the guard: cursors
// that keep changing but never end stop at maxGetStreamsPages.
func TestFetchLivePageCapFailsTheTick(t *testing.T) {
	tw := &fakeTwitch{freshCursor: true}
	proc := &fakeProcessor{}
	svc := seededService(tw, proc, nil)

	err := svc.tick(context.Background())
	if !errors.Is(err, twitch.ErrPaginationStalled) {
		t.Fatalf("tick = %v, want ErrPaginationStalled", err)
	}
	if tw.calls != maxGetStreamsPages {
		t.Fatalf("GetStreams calls = %d, want %d (page cap)", tw.calls, maxGetStreamsPages)
	}
	if len(proc.offline) != 0 {
		t.Fatalf("offline dispatches = %d after a capped drain; want none", len(proc.offline))
	}
}

// TestFetchLiveStopsOnCancellationBetweenPages pins the in-loop ctx check: a
// context cancelled while a page is in flight ends the drain before the next
// request, even when the client itself keeps answering.
func TestFetchLiveStopsOnCancellationBetweenPages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tw := &fakeTwitch{freshCursor: true, beforeReply: func(_ context.Context, call int) error {
		if call == 2 {
			cancel()
		}
		return nil
	}}
	svc := seededService(tw, &fakeProcessor{}, nil)

	err := svc.tick(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("tick = %v, want context.Canceled", err)
	}
	if tw.calls != 2 {
		t.Fatalf("GetStreams calls = %d, want 2 (no request after cancellation)", tw.calls)
	}
}

// TestRunOnceStaysQuietOnShutdownCancellation pins that a tick cut short by
// the poller's own shutdown is not reported as a poll failure, while the same
// error outside shutdown still is.
func TestRunOnceStaysQuietOnShutdownCancellation(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tw := &fakeTwitch{beforeReply: func(ctx context.Context, _ int) error {
		cancel()
		return ctx.Err()
	}}
	seededService(tw, &fakeProcessor{}, log).runOnce(ctx)
	if tw.calls != 1 {
		t.Fatalf("GetStreams calls = %d, want 1", tw.calls)
	}
	assertQuietLog(t, &buf)

	buf.Reset()
	seededService(&fakeTwitch{err: errors.New("helix down")}, &fakeProcessor{}, log).runOnce(context.Background())
	if !bytes.Contains(buf.Bytes(), []byte("level=WARN")) {
		t.Fatalf("a real tick failure was not logged at warn; got:\n%s", buf.String())
	}
}

// TestSeedCancellationIsNotCountedAsFailure pins that a baseline read
// interrupted by shutdown neither advances the seed-failure counter nor
// reaches the stall alarm, however many times it repeats.
func TestSeedCancellationIsNotCountedAsFailure(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := &fakeRepo{
		channels: []repository.Channel{channel("b-1")},
		beforeActive: func(ctx context.Context) error {
			cancel()
			return ctx.Err()
		},
	}
	tw := &fakeTwitch{}
	svc := New(repo, tw, &fakeProcessor{}, time.Minute, log)
	for range seedFailureEscalation {
		svc.runOnce(ctx)
	}
	if svc.seedFailures != 0 {
		t.Fatalf("seedFailures = %d after cancelled seeds, want 0", svc.seedFailures)
	}
	if svc.seeded {
		t.Fatal("seeded = true after a cancelled seed")
	}
	if tw.calls != 0 {
		t.Fatalf("GetStreams calls = %d after cancelled seeds, want 0", tw.calls)
	}
	assertQuietLog(t, &buf)
}

func TestSeededLiveStreamRefreshesIdentityForOfflineEvent(t *testing.T) {
	repo := &fakeRepo{channels: []repository.Channel{channel("b-1")}, activeStreams: []repository.Stream{{ID: "s-1", BroadcasterID: "b-1"}}}
	tw := &fakeTwitch{streams: []twitch.Stream{stream("s-1", "b-1", time.Now())}}
	proc := &fakeProcessor{}
	svc := New(repo, tw, proc, time.Minute, nil)
	if err := svc.tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(proc.online) != 0 {
		t.Fatal("same stream dispatched online again")
	}
	repo.channels = nil
	tw.streams = nil
	if err := svc.tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(proc.offline) != 1 || proc.offline[0].BroadcasterUserLogin != "b-1-login" {
		t.Fatalf("offline identity: %+v", proc.offline)
	}
}

func TestFailedReplacementKeepsEventualOfflineNotification(t *testing.T) {
	repo := &fakeRepo{channels: []repository.Channel{channel("b-1")}, activeStreams: []repository.Stream{{ID: "old", BroadcasterID: "b-1"}}}
	tw := &fakeTwitch{streams: []twitch.Stream{stream("new", "b-1", time.Now())}}
	proc := &fakeProcessor{onlineErr: errors.New("storage unavailable")}
	svc := New(repo, tw, proc, time.Minute, nil)
	if err := svc.tick(t.Context()); err == nil {
		t.Fatal("expected admission failure")
	}
	if len(proc.closeStale) != 1 {
		t.Fatal("old stream not closed")
	}
	proc.onlineErr = nil
	tw.streams = nil
	if err := svc.tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(proc.offline) != 1 {
		t.Fatalf("offline event lost: %+v", proc.offline)
	}
	if err := svc.tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(proc.offline) != 1 {
		t.Fatal("offline event duplicated")
	}
}

type retryDownloader struct {
	err   error
	calls int
}

func (d *retryDownloader) Start(context.Context, downloader.Params) (string, error) {
	d.calls++
	return "job", d.err
}

func TestReplacementRetryDoesNotEndCurrentLiveStream(t *testing.T) {
	ctx := t.Context()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	if _, err := repo.UpsertUser(ctx, &repository.User{ID: "owner", Login: "owner", DisplayName: "Owner", Role: "owner"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "b-1", BroadcasterLogin: "b1", BroadcasterName: "B1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateSchedule(ctx, &repository.ScheduleInput{BroadcasterID: "b-1", RequestedBy: "owner", Quality: "HIGH"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertStream(ctx, &repository.StreamInput{ID: "old", BroadcasterID: "b-1", Type: "live", StartedAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dl := &retryDownloader{err: downloader.ErrStorageUnavailable}
	hydrator := streammeta.NewHydrator(repo, nil, streammeta.Config{}, log)
	processor := schedule.NewEventProcessor(repo, dl, nil, hydrator, nil, log)
	tw := &fakeTwitch{streams: []twitch.Stream{stream("new", "b-1", time.Now())}}
	svc := New(repo, tw, processor, time.Minute, log)
	for attempt := 0; attempt < 2; attempt++ {
		if err := svc.tick(ctx); !errors.Is(err, downloader.ErrStorageUnavailable) {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		current, err := repo.GetStream(ctx, "new")
		if err != nil || current.EndedAt != nil {
			t.Fatalf("retry retired the current live stream: %+v, %v", current, err)
		}
	}
	old, err := repo.GetStream(ctx, "old")
	if err != nil || old.EndedAt == nil {
		t.Fatalf("superseded stream was not retired: %+v, %v", old, err)
	}
	// A second stream transition during the same outage must still retire
	// the previous attempt, while retries of that new ID leave it open.
	tw.streams = []twitch.Stream{stream("newest", "b-1", time.Now().Add(time.Minute))}
	if err := svc.tick(ctx); !errors.Is(err, downloader.ErrStorageUnavailable) {
		t.Fatal(err)
	}
	previous, err := repo.GetStream(ctx, "new")
	if err != nil || previous.EndedAt == nil {
		t.Fatalf("second transition failed to retire previous stream: %+v, %v", previous, err)
	}
	dl.err = nil
	if err := svc.tick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.tick(ctx); err != nil || dl.calls != 4 {
		t.Fatalf("successful dispatch repeated: calls=%d, %v", dl.calls, err)
	}
	current, err := repo.GetStream(ctx, "newest")
	if err != nil || current.EndedAt != nil {
		t.Fatalf("successful recovery retired current stream: %+v, %v", current, err)
	}
	tw.streams = nil
	if err := svc.tick(ctx); err != nil {
		t.Fatal(err)
	}
	current, err = repo.GetStream(ctx, "newest")
	if err != nil || current.EndedAt == nil {
		t.Fatalf("eventual offline did not close current stream: %+v, %v", current, err)
	}
}

func (*retryDownloader) ObserveStreamOnline(twitch.Stream) {}

func (*retryDownloader) ObserveStreamOffline(string) {}
