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
