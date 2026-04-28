package streammeta

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// recordingEnricher is a test double for CategoryArtEnricher that
// captures every Enrich call. Used to verify the Hydrator's
// on-first-observation hook fires with the right broadcaster/game id.
type recordingEnricher struct {
	calls []string
	err   error
}

func (r *recordingEnricher) Enrich(_ context.Context, categoryID string) error {
	r.calls = append(r.calls, categoryID)
	return r.err
}

// newTestHydrator builds a Hydrator backed by a fresh SQLite repo and
// the supplied enricher. Skips the Twitch client (nil) — persist
// takes a synthetic *twitch.Stream directly so we don't exercise
// fetchWithRetry in these tests.
func newTestHydrator(t *testing.T, art CategoryArtEnricher) (*Hydrator, repository.Repository) {
	t.Helper()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	h := NewHydrator(repo, nil, Config{CategoryArt: art}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return h, repo
}

// synthStream builds a minimal twitch.Stream with just the fields
// persist actually consumes. Keeps each test self-documenting without
// boilerplating 15 zero-value fields.
func synthStream(streamID, broadcasterID, gameID, gameName string) *twitch.Stream {
	return &twitch.Stream{
		ID:          streamID,
		UserID:      broadcasterID,
		UserLogin:   "login-" + broadcasterID,
		UserName:    "Name " + broadcasterID,
		Type:        "live",
		Language:    "en",
		ViewerCount: 1,
		GameID:      gameID,
		GameName:    gameName,
		StartedAt:   time.Now().UTC().Truncate(time.Second),
	}
}

// TestHydrator_CallsEnrichForNewCategory pins the integration:
// persist observes a game the local mirror has no art for, Enrich
// fires with exactly that game_id. The categoryart package tests
// cover what happens inside Enrich; this test covers the wiring.
func TestHydrator_CallsEnrichForNewCategory(t *testing.T) {
	ctx := context.Background()
	art := &recordingEnricher{}
	h, repo := newTestHydrator(t, art)

	// Seed channel so the stream-row upsert succeeds (FK). Category
	// row is created by persist itself.
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "b-1", BroadcasterLogin: "b", BroadcasterName: "B",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	h.persist(ctx, "b-1", synthStream("s-1", "b-1", "game-42", "Game 42"))

	if len(art.calls) != 1 {
		t.Fatalf("expected exactly one Enrich call, got %d (%v)", len(art.calls), art.calls)
	}
	if art.calls[0] != "game-42" {
		t.Errorf("Enrich called with wrong id: got %q, want %q", art.calls[0], "game-42")
	}
}

// TestHydrator_SkipsEnrichWhenArtAlreadyPresent documents the
// freshness-vs-quota tradeoff: if the category already has box art
// from a prior sync, the Hydrator does NOT re-fetch. Re-firing on
// every stream.online for a popular category would triple Helix
// quota with zero signal (box art is near-immutable).
func TestHydrator_SkipsEnrichWhenArtAlreadyPresent(t *testing.T) {
	ctx := context.Background()
	art := &recordingEnricher{}
	h, repo := newTestHydrator(t, art)

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "b-1", BroadcasterLogin: "b", BroadcasterName: "B",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	existing := "https://cdn.example.com/g-existing-{width}x{height}.jpg"
	if _, err := repo.UpsertCategory(ctx, &repository.Category{
		ID: "game-42", Name: "Game 42", BoxArtURL: &existing,
	}); err != nil {
		t.Fatalf("seed category: %v", err)
	}

	h.persist(ctx, "b-1", synthStream("s-1", "b-1", "game-42", "Game 42"))

	if len(art.calls) != 0 {
		t.Errorf("Enrich should be skipped when art already present, got %v", art.calls)
	}
}

// TestHydrator_TypedNilEnricherIsNoOp pins the interface-foot-gun
// guard: a caller passing an explicit typed-nil concrete pointer as
// cfg.CategoryArt (common when wiring optional deps through a DI
// framework or conditional construction) must NOT trigger a nil-
// deref inside persist. NewHydrator normalizes typed-nil to
// interface-nil so the runtime check short-circuits cleanly.
func TestHydrator_TypedNilEnricherIsNoOp(t *testing.T) {
	ctx := context.Background()
	// Typed-nil: an interface whose dynamic type is a concrete nil
	// pointer. Without nilSafeEnricher in NewHydrator, the h.art != nil
	// guard would evaluate true and the subsequent Enrich call would
	// panic.
	var typedNil *recordingEnricher // nil concrete pointer
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	h := NewHydrator(repo, nil, Config{CategoryArt: typedNil}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "b-1", BroadcasterLogin: "b", BroadcasterName: "B",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	// Should not panic; Enrich branch is skipped.
	snap := h.persist(ctx, "b-1", synthStream("s-1", "b-1", "game-42", "Game 42"))
	if snap == nil {
		t.Fatal("snapshot should be non-nil even with typed-nil enricher")
	}
}

// TestHydrator_NilEnricherIsNoOp asserts the optional-dep semantics:
// a Hydrator configured without a CategoryArt enricher (Config{} or
// tests / degraded mode) still persists the stream + category
// cleanly. The scheduled backfill task is the only filler in that
// configuration.
func TestHydrator_NilEnricherIsNoOp(t *testing.T) {
	ctx := context.Background()
	h, repo := newTestHydrator(t, nil)

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "b-1", BroadcasterLogin: "b", BroadcasterName: "B",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	snap := h.persist(ctx, "b-1", synthStream("s-1", "b-1", "game-42", "Game 42"))
	if snap == nil || snap.StreamID == "" {
		t.Fatalf("persist returned empty snapshot: %+v", snap)
	}

	got, err := repo.GetCategory(ctx, "game-42")
	if err != nil {
		t.Fatalf("get category: %v", err)
	}
	if got.BoxArtURL != nil {
		t.Errorf("no enricher → box art should stay nil, got %q", *got.BoxArtURL)
	}
}

// TestHydrator_EnricherErrorLoggedNotReturned pins that a Helix
// failure in the enricher doesn't fail persist. The snapshot still
// flows through to the caller; the category row exists (without art)
// for the scheduled backfill to pick up next tick.
func TestHydrator_EnricherErrorLoggedNotReturned(t *testing.T) {
	ctx := context.Background()
	art := &recordingEnricher{err: errors.New("helix 503")}
	h, repo := newTestHydrator(t, art)

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "b-1", BroadcasterLogin: "b", BroadcasterName: "B",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	snap := h.persist(ctx, "b-1", synthStream("s-1", "b-1", "game-42", "Game 42"))
	if snap == nil {
		t.Fatal("snapshot should not be nil on enrich error")
	}
	if snap.GameID != "game-42" {
		t.Errorf("snapshot game id: got %q", snap.GameID)
	}
	// Enrich was still attempted exactly once.
	if len(art.calls) != 1 {
		t.Errorf("expected 1 enrich attempt, got %d", len(art.calls))
	}
}

// seedRecording creates the channel + video rows that
// LinkInitialVideoMetadata expects to find, and returns the new
// video id. Used by the metadata-change tests below.
func seedRecording(t *testing.T, ctx context.Context, repo repository.Repository) int64 {
	t.Helper()
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "b-1", BroadcasterLogin: "b", BroadcasterName: "B",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	video, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID:         "job-1",
		Filename:      "rec-1.mp4",
		DisplayName:   "B",
		Status:        repository.VideoStatusRunning,
		Quality:       repository.QualityHigh,
		BroadcasterID: "b-1",
		Language:      "en",
		RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("create video: %v", err)
	}
	return video.ID
}

// TestHydrator_LinkInitialVideoMetadata_BothDimensionsShareTimestamp
// pins the design that drove the video_metadata_changes table: one
// channel.update event delivering both title and category produces a
// single timeline row, and the title span / category span share that
// row's occurred_at exactly. Two time.Now() calls (the old shape)
// would split the dashboard timeline into two stacked entries.
func TestHydrator_LinkInitialVideoMetadata_BothDimensionsShareTimestamp(t *testing.T) {
	ctx := context.Background()
	h, repo := newTestHydrator(t, nil)
	videoID := seedRecording(t, ctx, repo)

	if err := h.LinkInitialVideoMetadata(ctx, videoID, ChannelUpdateMeta{
		Title:        "Just chatting",
		CategoryID:   "game-42",
		CategoryName: "Game 42",
	}); err != nil {
		t.Fatalf("link initial: %v", err)
	}

	events, err := repo.ListVideoMetadataChanges(ctx, videoID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly one event row, got %d", len(events))
	}
	ev := events[0]
	if ev.Title == nil {
		t.Fatalf("event title should be hydrated")
	}
	if ev.Title.Name != "Just chatting" {
		t.Errorf("event title: got %q, want %q", ev.Title.Name, "Just chatting")
	}
	if ev.Category == nil || ev.Category.ID != "game-42" {
		t.Fatalf("event category: got %+v", ev.Category)
	}

	// The title span and category span must share the event's
	// occurred_at — that's the structural guarantee the events
	// table replaced timestamp-coincidence grouping with.
	titles, err := repo.ListTitlesForVideo(ctx, videoID)
	if err != nil {
		t.Fatalf("list titles: %v", err)
	}
	if len(titles) != 1 {
		t.Fatalf("expected one title span, got %d", len(titles))
	}
	if !titles[0].StartedAt.Equal(ev.OccurredAt) {
		t.Errorf("title span started_at %v != event occurred_at %v", titles[0].StartedAt, ev.OccurredAt)
	}
	categories, err := repo.ListCategoriesForVideo(ctx, videoID)
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if len(categories) != 1 {
		t.Fatalf("expected one category span, got %d", len(categories))
	}
	if !categories[0].StartedAt.Equal(ev.OccurredAt) {
		t.Errorf("category span started_at %v != event occurred_at %v", categories[0].StartedAt, ev.OccurredAt)
	}
}

// TestHydrator_LinkInitialVideoMetadata_TitleOnly verifies a partial
// channel.update (title without category) produces an event row with
// title_id set and category_id NULL. Mirrors the wire shape of a
// real Helix payload that touches only one dimension.
func TestHydrator_LinkInitialVideoMetadata_TitleOnly(t *testing.T) {
	ctx := context.Background()
	h, repo := newTestHydrator(t, nil)
	videoID := seedRecording(t, ctx, repo)

	if err := h.LinkInitialVideoMetadata(ctx, videoID, ChannelUpdateMeta{
		Title: "title-only",
	}); err != nil {
		t.Fatalf("link initial: %v", err)
	}

	events, err := repo.ListVideoMetadataChanges(ctx, videoID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event row, got %d", len(events))
	}
	if events[0].Title == nil || events[0].Title.Name != "title-only" {
		t.Errorf("title not hydrated: %+v", events[0].Title)
	}
	if events[0].Category != nil {
		t.Errorf("category should be nil, got %+v", events[0].Category)
	}
}

// TestHydrator_LinkInitialVideoMetadata_CategoryOnly is the inverse:
// category alone produces an event row with title_id NULL.
func TestHydrator_LinkInitialVideoMetadata_CategoryOnly(t *testing.T) {
	ctx := context.Background()
	h, repo := newTestHydrator(t, nil)
	videoID := seedRecording(t, ctx, repo)

	if err := h.LinkInitialVideoMetadata(ctx, videoID, ChannelUpdateMeta{
		CategoryID:   "game-42",
		CategoryName: "Game 42",
	}); err != nil {
		t.Fatalf("link initial: %v", err)
	}

	events, err := repo.ListVideoMetadataChanges(ctx, videoID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event row, got %d", len(events))
	}
	if events[0].Title != nil {
		t.Errorf("title should be nil, got %+v", events[0].Title)
	}
	if events[0].Category == nil || events[0].Category.ID != "game-42" {
		t.Errorf("category not hydrated: %+v", events[0].Category)
	}
}

// TestHydrator_LinkInitialVideoMetadata_OrdersByOccurredAt walks two
// successive change events and asserts ListVideoMetadataChanges
// returns them in chronological order. The dialog's offset-from-first
// timeline labels rely on this ordering.
func TestHydrator_LinkInitialVideoMetadata_OrdersByOccurredAt(t *testing.T) {
	ctx := context.Background()
	h, repo := newTestHydrator(t, nil)
	videoID := seedRecording(t, ctx, repo)

	if err := h.LinkInitialVideoMetadata(ctx, videoID, ChannelUpdateMeta{
		Title:        "first",
		CategoryID:   "game-1",
		CategoryName: "First Game",
	}); err != nil {
		t.Fatalf("first link: %v", err)
	}
	// Sleep one second so SQLite's text-time format (second-
	// resolution) records distinct occurred_at values. The fix
	// upstream (RecordChannelUpdate hoisting `at` to a single
	// time.Now() per event) doesn't help across separate calls —
	// these are genuinely two events.
	time.Sleep(1100 * time.Millisecond)
	if err := h.LinkInitialVideoMetadata(ctx, videoID, ChannelUpdateMeta{
		Title:        "second",
		CategoryID:   "game-2",
		CategoryName: "Second Game",
	}); err != nil {
		t.Fatalf("second link: %v", err)
	}

	events, err := repo.ListVideoMetadataChanges(ctx, videoID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Title == nil || events[0].Title.Name != "first" {
		t.Errorf("first event title: got %+v", events[0].Title)
	}
	if events[1].Title == nil || events[1].Title.Name != "second" {
		t.Errorf("second event title: got %+v", events[1].Title)
	}
	if !events[1].OccurredAt.After(events[0].OccurredAt) {
		t.Errorf("event order: second %v not after first %v", events[1].OccurredAt, events[0].OccurredAt)
	}
}
