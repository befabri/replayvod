package streammeta

import (
	"context"
	"database/sql"
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

type staticMediaOffset struct {
	seconds float64
	ok      bool
}

func (s staticMediaOffset) MediaOffsetSeconds() (float64, bool) {
	return s.seconds, s.ok
}

type recordingMediaOffsetResolver struct {
	seconds       float64
	ok            bool
	broadcasterID string
	videoID       int64
	calls         int
}

func (r *recordingMediaOffsetResolver) ResolveMediaOffsetSeconds(_ context.Context, broadcasterID string, videoID int64) (float64, bool) {
	r.calls++
	r.broadcasterID = broadcasterID
	r.videoID = videoID
	return r.seconds, r.ok
}

type fakeStreamFetcher struct {
	calls  int
	seen   []twitch.GetStreamsParams
	pages  [][]twitch.Stream
	errs   []error
	called chan int
}

func (f *fakeStreamFetcher) GetStreams(_ context.Context, params *twitch.GetStreamsParams) ([]twitch.Stream, twitch.Pagination, error) {
	f.calls++
	call := f.calls
	if params != nil {
		f.seen = append(f.seen, *params)
	}
	if f.called != nil {
		select {
		case f.called <- call:
		default:
		}
	}
	if call <= len(f.errs) && f.errs[call-1] != nil {
		return nil, twitch.Pagination{}, f.errs[call-1]
	}
	if call <= len(f.pages) {
		return f.pages[call-1], twitch.Pagination{}, nil
	}
	return nil, twitch.Pagination{}, nil
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

func newFetchRetryHydrator(fetcher streamFetcher, retries int, delay time.Duration) *Hydrator {
	return &Hydrator{
		twitch:  fetcher,
		log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		retries: retries,
		delay:   delay,
		now:     time.Now,
	}
}

func TestNewFetchRetryHydratorSetsClock(t *testing.T) {
	h := newFetchRetryHydrator(nil, 1, time.Millisecond)
	if h.now == nil {
		t.Fatal("newFetchRetryHydrator left now nil")
	}
	if got := h.now(); got.IsZero() {
		t.Fatal("newFetchRetryHydrator now returned zero time")
	}
}

func TestFetchWithRetry_RetriesEmptyStreamListThenReturnsStream(t *testing.T) {
	want := synthStream("s-1", "b-1", "game-42", "Game 42")
	fetcher := &fakeStreamFetcher{
		pages: [][]twitch.Stream{
			nil,
			{*want},
		},
	}
	h := newFetchRetryHydrator(fetcher, 3, time.Millisecond)

	got, err := h.fetchWithRetry(context.Background(), "b-1")
	if err != nil {
		t.Fatalf("fetchWithRetry() error = %v, want nil", err)
	}
	if got == nil || got.ID != "s-1" {
		t.Fatalf("fetchWithRetry() stream = %+v, want s-1", got)
	}
	if fetcher.calls != 2 {
		t.Fatalf("GetStreams calls = %d, want 2 (empty first page must retry)", fetcher.calls)
	}
	if len(fetcher.seen) != 2 || len(fetcher.seen[0].UserID) != 1 || fetcher.seen[0].UserID[0] != "b-1" || fetcher.seen[0].First != 1 {
		t.Fatalf("GetStreams params = %+v, want UserID [b-1] and First 1", fetcher.seen)
	}
}

func TestFetchWithRetry_ExhaustsRetriesReturningLastError(t *testing.T) {
	firstErr := errors.New("helix 500")
	lastErr := errors.New("helix 503")
	fetcher := &fakeStreamFetcher{errs: []error{firstErr, lastErr}}
	h := newFetchRetryHydrator(fetcher, 2, time.Millisecond)

	got, err := h.fetchWithRetry(context.Background(), "b-1")
	if got != nil {
		t.Fatalf("fetchWithRetry() stream = %+v, want nil", got)
	}
	if !errors.Is(err, lastErr) {
		t.Fatalf("fetchWithRetry() error = %v, want last error %v", err, lastErr)
	}
	if fetcher.calls != 2 {
		t.Fatalf("GetStreams calls = %d, want retries exhausted after 2 attempts", fetcher.calls)
	}
}

func TestFetchWithRetry_ExhaustsEmptyStreamRetries(t *testing.T) {
	fetcher := &fakeStreamFetcher{pages: [][]twitch.Stream{nil, nil}}
	h := newFetchRetryHydrator(fetcher, 2, time.Millisecond)

	got, err := h.fetchWithRetry(context.Background(), "b-1")
	if got != nil {
		t.Fatalf("fetchWithRetry() stream = %+v, want nil", got)
	}
	if err == nil {
		t.Fatal("fetchWithRetry() error = nil, want retries-exhausted error")
	}
	if fetcher.calls != 2 {
		t.Fatalf("GetStreams calls = %d, want 2 empty-stream attempts", fetcher.calls)
	}
}

func TestFetchWithRetry_ContextCancelDuringRetryDelay(t *testing.T) {
	fetcher := &fakeStreamFetcher{
		pages:  [][]twitch.Stream{nil, {*synthStream("s-1", "b-1", "game-42", "Game 42")}},
		called: make(chan int, 1),
	}
	h := newFetchRetryHydrator(fetcher, 2, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		_, err := h.fetchWithRetry(ctx, "b-1")
		errCh <- err
	}()

	select {
	case <-fetcher.called:
	case <-time.After(2 * time.Second):
		t.Fatal("GetStreams was not called")
	}
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("fetchWithRetry() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fetchWithRetry() did not return after context cancellation during retry delay")
	}
	if fetcher.calls != 1 {
		t.Fatalf("GetStreams calls = %d, want no second attempt after cancellation", fetcher.calls)
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

// TestHydrator_CallsEnrichWhenIGDBIDMissing pins the newer description path:
// a category that already has art still needs /helix/games if igdb_id is empty.
func TestHydrator_CallsEnrichWhenIGDBIDMissing(t *testing.T) {
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

	if len(art.calls) != 1 || art.calls[0] != "game-42" {
		t.Errorf("Enrich calls = %v, want [game-42]", art.calls)
	}
}

func TestHydrator_SkipsEnrichWhenGameMetadataRecentlyChecked(t *testing.T) {
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
	if err := repo.MarkCategoryGameMetadataChecked(ctx, "game-42"); err != nil {
		t.Fatalf("mark game metadata checked: %v", err)
	}

	h.persist(ctx, "b-1", synthStream("s-1", "b-1", "game-42", "Game 42"))

	if len(art.calls) != 0 {
		t.Errorf("Enrich should be skipped after recent game metadata check, got %v", art.calls)
	}
}

// TestHydrator_SkipsEnrichWhenGameMetadataAlreadyPresent documents the
// freshness-vs-quota tradeoff: if the category already has both box art and
// igdb_id from a prior sync, the Hydrator does NOT re-fetch. Re-firing on every
// stream.online for a popular category would burn Helix quota with zero signal.
func TestHydrator_SkipsEnrichWhenGameMetadataAlreadyPresent(t *testing.T) {
	ctx := context.Background()
	art := &recordingEnricher{}
	h, repo := newTestHydrator(t, art)

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "b-1", BroadcasterLogin: "b", BroadcasterName: "B",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	existing := "https://cdn.example.com/g-existing-{width}x{height}.jpg"
	igdbID := "4242"
	if _, err := repo.UpsertCategory(ctx, &repository.Category{
		ID: "game-42", Name: "Game 42", BoxArtURL: &existing, IGDBID: &igdbID,
	}); err != nil {
		t.Fatalf("seed category: %v", err)
	}

	h.persist(ctx, "b-1", synthStream("s-1", "b-1", "game-42", "Game 42"))

	if len(art.calls) != 0 {
		t.Errorf("Enrich should be skipped when game metadata is already present, got %v", art.calls)
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

// TestHydrator_HydrateFromStream_NoHelixFetch pins the poll-mode fix: enrichment
// runs off the already-polled stream with no GetStreams call. newTestHydrator
// wires a nil Twitch client, so any re-fetch would panic — reaching here proves
// HydrateFromStream uses the supplied stream. The streams row + category persist
// and the snapshot carries the signals the schedule matcher needs.
func TestHydrator_HydrateFromStream_NoHelixFetch(t *testing.T) {
	ctx := context.Background()
	h, repo := newTestHydrator(t, nil)

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "b-1", BroadcasterLogin: "b", BroadcasterName: "B",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	snap := h.HydrateFromStream(ctx, synthStream("s-1", "b-1", "game-42", "Game 42"))
	if snap == nil || snap.StreamID != "s-1" {
		t.Fatalf("HydrateFromStream snapshot = %+v, want StreamID s-1", snap)
	}
	if snap.GameID != "game-42" || snap.ViewerCount != 1 || snap.Language != "en" {
		t.Fatalf("snapshot signals = %+v, want game-42/viewers 1/lang en", snap)
	}
	if _, err := repo.GetStream(ctx, "s-1"); err != nil {
		t.Fatalf("streams row not persisted: %v", err)
	}

	if got := h.HydrateFromStream(ctx, nil); got != nil {
		t.Fatalf("HydrateFromStream(nil) = %+v, want nil", got)
	}
	// The other half of the guard: a stream with no UserID can't be keyed to a
	// broadcaster, so it must short-circuit rather than persist under "".
	if got := h.HydrateFromStream(ctx, &twitch.Stream{ID: "s-x", UserID: ""}); got != nil {
		t.Fatalf("HydrateFromStream(empty UserID) = %+v, want nil", got)
	}
}

// TestHydrator_HydrateFromStream_PersistsTags pins the tag path through the
// prefetched-stream entry: each non-empty Helix tag name is upserted, collected
// into snap.TagIDs, and linked in stream_tags. The empty-name entry is skipped.
func TestHydrator_HydrateFromStream_PersistsTags(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	h := NewHydrator(repo, nil, Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "b-1", BroadcasterLogin: "b", BroadcasterName: "B",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	stream := synthStream("s-tags", "b-1", "game-42", "Game 42")
	stream.Tags = []string{"English", "", "Speedrun"}

	snap := h.HydrateFromStream(ctx, stream)
	if snap == nil {
		t.Fatal("HydrateFromStream returned nil")
	}
	if len(snap.TagIDs) != 2 {
		t.Fatalf("snap.TagIDs = %v, want 2 ids (empty tag name skipped)", snap.TagIDs)
	}

	// The links must actually land in stream_tags for the recording's tag history.
	var linked int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM stream_tags WHERE stream_id = ?", "s-tags").Scan(&linked); err != nil {
		t.Fatalf("count stream_tags: %v", err)
	}
	if linked != 2 {
		t.Fatalf("stream_tags rows = %d, want 2", linked)
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

// newTestHydratorDB is newTestHydrator plus the raw *sql.DB handle, for
// tests that assert the M2M link rows (stream_categories /
// stream_titles) directly the way the existing tag test reads
// stream_tags.
func newTestHydratorDB(t *testing.T, art CategoryArtEnricher) (*Hydrator, repository.Repository, *sql.DB) {
	t.Helper()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	h := NewHydrator(repo, nil, Config{CategoryArt: art}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return h, repo, db
}

// seedChannel inserts the broadcaster row that streams.broadcaster_id
// FK-references, so a subsequent UpsertStream lands instead of failing
// the FK check.
func seedChannel(t *testing.T, ctx context.Context, repo repository.Repository) {
	t.Helper()
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "b-1", BroadcasterLogin: "b", BroadcasterName: "B",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
}

func countRows(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("count (%s): %v", query, err)
	}
	return n
}

// TestHydrator_Persist_MapsAllFieldsAndWritesLinks feeds a fully
// populated Helix stream and pins the field-by-field mapping persist
// performs: every Snapshot field comes off the stream, the streams row
// carries the optional columns (type, thumbnail, is_mature), and the
// category + title M2M edges land. This is the happy-path complement to
// the existing tag-link test, covering the title write path that no
// other test exercises.
func TestHydrator_Persist_MapsAllFieldsAndWritesLinks(t *testing.T) {
	ctx := context.Background()
	h, repo, db := newTestHydratorDB(t, nil)
	seedChannel(t, ctx, repo)

	startedAt := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	thumb := "https://thumb.example/{width}x{height}.jpg"
	stream := &twitch.Stream{
		ID:           "s-full",
		UserID:       "b-1",
		Type:         "live",
		Language:     "fr",
		Title:        "Full send",
		ViewerCount:  4321,
		GameID:       "game-7",
		GameName:     "Game 7",
		StartedAt:    startedAt,
		ThumbnailURL: thumb,
		IsMature:     true,
		Tags:         []string{"English", "Speedrun"},
	}

	snap := h.persist(ctx, "b-1", stream)
	if snap == nil {
		t.Fatal("persist returned nil")
	}

	// Snapshot mirrors the live response one field at a time.
	if snap.StreamID != "s-full" {
		t.Errorf("StreamID = %q, want s-full", snap.StreamID)
	}
	if snap.Title != "Full send" || snap.Language != "fr" || snap.ViewerCount != 4321 {
		t.Errorf("snapshot scalars = %+v, want title/lang/viewers Full send/fr/4321", snap)
	}
	if snap.GameID != "game-7" || snap.GameName != "Game 7" {
		t.Errorf("snapshot game = %q/%q, want game-7/Game 7", snap.GameID, snap.GameName)
	}
	if !snap.StartedAt.Equal(startedAt) {
		t.Errorf("snapshot StartedAt = %v, want %v", snap.StartedAt, startedAt)
	}
	if len(snap.CategoryIDs) != 1 || snap.CategoryIDs[0] != "game-7" {
		t.Errorf("snapshot CategoryIDs = %v, want [game-7]", snap.CategoryIDs)
	}
	if len(snap.TagIDs) != 2 {
		t.Errorf("snapshot TagIDs = %v, want 2", snap.TagIDs)
	}

	// The streams row carries the optional columns persist maps through.
	row, err := repo.GetStream(ctx, "s-full")
	if err != nil {
		t.Fatalf("get stream: %v", err)
	}
	if row.Type != "live" || row.Language != "fr" || row.ViewerCount != 4321 {
		t.Errorf("stream row scalars = %+v, want live/fr/4321", row)
	}
	if row.ThumbnailURL == nil || *row.ThumbnailURL != thumb {
		t.Errorf("stream row ThumbnailURL = %v, want %q", row.ThumbnailURL, thumb)
	}
	if row.IsMature == nil || !*row.IsMature {
		t.Errorf("stream row IsMature = %v, want true", row.IsMature)
	}
	if !row.StartedAt.Equal(startedAt) {
		t.Errorf("stream row StartedAt = %v, want %v", row.StartedAt, startedAt)
	}

	// Category and title edges both land in their junction tables.
	if got := countRows(t, ctx, db, "SELECT COUNT(*) FROM stream_categories WHERE stream_id = ?", "s-full"); got != 1 {
		t.Errorf("stream_categories rows = %d, want 1", got)
	}
	if got := countRows(t, ctx, db, "SELECT COUNT(*) FROM stream_titles WHERE stream_id = ?", "s-full"); got != 1 {
		t.Errorf("stream_titles rows = %d, want 1", got)
	}
}

// TestHydrator_Persist_OmitsEmptyOptionalFields pins the null-handling
// side: a live response with no game, no title and no thumbnail still
// upserts the streams row, but skips the category and title writes
// entirely and persists the thumbnail as NULL rather than an empty
// string. This is the branch the dashboard relies on to tell "Twitch
// had no title" apart from "title was blank".
func TestHydrator_Persist_OmitsEmptyOptionalFields(t *testing.T) {
	ctx := context.Background()
	h, repo, db := newTestHydratorDB(t, nil)
	seedChannel(t, ctx, repo)

	stream := &twitch.Stream{
		ID:          "s-bare",
		UserID:      "b-1",
		Type:        "live",
		Language:    "en",
		ViewerCount: 0,
		StartedAt:   time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC),
		// GameID, Title, ThumbnailURL all empty; no Tags.
	}

	snap := h.persist(ctx, "b-1", stream)
	if snap == nil || snap.StreamID != "s-bare" {
		t.Fatalf("persist snapshot = %+v, want StreamID s-bare", snap)
	}
	if len(snap.CategoryIDs) != 0 {
		t.Errorf("CategoryIDs = %v, want empty (no game id)", snap.CategoryIDs)
	}
	if len(snap.TagIDs) != 0 {
		t.Errorf("TagIDs = %v, want empty (no tags)", snap.TagIDs)
	}
	if snap.Title != "" {
		t.Errorf("Title = %q, want empty", snap.Title)
	}

	row, err := repo.GetStream(ctx, "s-bare")
	if err != nil {
		t.Fatalf("get stream: %v", err)
	}
	if row.ThumbnailURL != nil {
		t.Errorf("empty thumbnail must persist as NULL, got %q", *row.ThumbnailURL)
	}

	if got := countRows(t, ctx, db, "SELECT COUNT(*) FROM stream_categories WHERE stream_id = ?", "s-bare"); got != 0 {
		t.Errorf("stream_categories rows = %d, want 0", got)
	}
	if got := countRows(t, ctx, db, "SELECT COUNT(*) FROM stream_titles WHERE stream_id = ?", "s-bare"); got != 0 {
		t.Errorf("stream_titles rows = %d, want 0", got)
	}
}

// TestHydrator_Persist_StreamUpsertFailureStillReportsLiveSignals pins
// the partial-snapshot guarantee in the package doc: when the streams
// row can't be written (here: an unseeded broadcaster trips the FK),
// persist still returns the live signals the schedule matcher needs.
// CategoryIDs is reported off the Helix response even though no
// stream_categories edge can be created without a stream row.
func TestHydrator_Persist_StreamUpsertFailureStillReportsLiveSignals(t *testing.T) {
	ctx := context.Background()
	// No seedChannel: the broadcaster row is absent, so UpsertStream
	// fails the streams.broadcaster_id foreign key.
	h, repo, db := newTestHydratorDB(t, nil)

	startedAt := time.Date(2024, 9, 9, 9, 9, 9, 0, time.UTC)
	stream := &twitch.Stream{
		ID:          "s-orphan",
		UserID:      "b-missing",
		Type:        "live",
		Language:    "de",
		Title:       "still live",
		ViewerCount: 12,
		GameID:      "game-9",
		GameName:    "Game 9",
		StartedAt:   startedAt,
		Tags:        []string{"English"},
	}

	snap := h.persist(ctx, "b-missing", stream)
	if snap == nil {
		t.Fatal("persist must return a partial snapshot, got nil")
	}
	if snap.StreamID != "" {
		t.Errorf("StreamID = %q, want empty (stream row not persisted)", snap.StreamID)
	}
	// Live signals survive the upsert failure.
	if snap.Title != "still live" || snap.Language != "de" || snap.ViewerCount != 12 {
		t.Errorf("live signals lost: %+v", snap)
	}
	if snap.GameID != "game-9" || snap.GameName != "Game 9" || !snap.StartedAt.Equal(startedAt) {
		t.Errorf("live game/time signals lost: %+v", snap)
	}
	// The matcher reads CategoryIDs off the response even without a row.
	if len(snap.CategoryIDs) != 1 || snap.CategoryIDs[0] != "game-9" {
		t.Errorf("CategoryIDs = %v, want [game-9] reported from live data", snap.CategoryIDs)
	}
	if len(snap.TagIDs) != 1 {
		t.Errorf("TagIDs = %v, want 1 (tags upsert independently of the stream row)", snap.TagIDs)
	}

	// No stream row, and therefore no link rows.
	if _, err := repo.GetStream(ctx, "s-orphan"); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("GetStream err = %v, want ErrNotFound", err)
	}
	if got := countRows(t, ctx, db, "SELECT COUNT(*) FROM stream_categories WHERE stream_id = ?", "s-orphan"); got != 0 {
		t.Errorf("stream_categories rows = %d, want 0 (no stream to link)", got)
	}
	if got := countRows(t, ctx, db, "SELECT COUNT(*) FROM stream_tags WHERE stream_id = ?", "s-orphan"); got != 0 {
		t.Errorf("stream_tags rows = %d, want 0 (no stream to link)", got)
	}
	// The dedup rows themselves still exist for the next, FK-clean write.
	if _, err := repo.GetCategory(ctx, "game-9"); err != nil {
		t.Errorf("category row should still be upserted: %v", err)
	}
}

// faultRepo wraps a real repository and forces exactly one child-row
// method to return an error, leaving every other call to delegate to
// the backing store. It exercises the best-effort "log and continue"
// branches in persist without a hand-rolled stub: the surrounding
// writes still land, so assertions can read back the rest.
type faultRepo struct {
	repository.Repository
	failOn string
	err    error
}

func (f *faultRepo) UpsertCategory(ctx context.Context, c *repository.Category) (*repository.Category, error) {
	if f.failOn == "UpsertCategory" {
		return nil, f.err
	}
	return f.Repository.UpsertCategory(ctx, c)
}

func (f *faultRepo) LinkStreamCategory(ctx context.Context, streamID, categoryID string) error {
	if f.failOn == "LinkStreamCategory" {
		return f.err
	}
	return f.Repository.LinkStreamCategory(ctx, streamID, categoryID)
}

func (f *faultRepo) UpsertTag(ctx context.Context, name string) (*repository.Tag, error) {
	if f.failOn == "UpsertTag" {
		return nil, f.err
	}
	return f.Repository.UpsertTag(ctx, name)
}

func (f *faultRepo) LinkStreamTag(ctx context.Context, streamID string, tagID int64) error {
	if f.failOn == "LinkStreamTag" {
		return f.err
	}
	return f.Repository.LinkStreamTag(ctx, streamID, tagID)
}

func (f *faultRepo) UpsertTitle(ctx context.Context, name string) (*repository.Title, error) {
	if f.failOn == "UpsertTitle" {
		return nil, f.err
	}
	return f.Repository.UpsertTitle(ctx, name)
}

func (f *faultRepo) LinkStreamTitle(ctx context.Context, streamID string, titleID int64) error {
	if f.failOn == "LinkStreamTitle" {
		return f.err
	}
	return f.Repository.LinkStreamTitle(ctx, streamID, titleID)
}

// TestHydrator_Persist_ChildWriteFailuresAreBestEffort walks every
// child-row write persist makes and forces it to fail in turn. The
// contract the package doc promises is that none of these can nil the
// snapshot or lose the (successful) stream row: the failure is logged
// and the remaining writes proceed. Two cases also pin the subtle
// difference between an upsert failing (the id never reaches the
// snapshot) and a link failing (the id is still reported).
func TestHydrator_Persist_ChildWriteFailuresAreBestEffort(t *testing.T) {
	injected := errors.New("injected repo failure")
	cases := []struct {
		name   string
		failOn string
	}{
		{"category upsert", "UpsertCategory"},
		{"category link", "LinkStreamCategory"},
		{"tag upsert", "UpsertTag"},
		{"tag link", "LinkStreamTag"},
		{"title upsert", "UpsertTitle"},
		{"title link", "LinkStreamTitle"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db := testdb.NewSQLiteDB(t)
			real := sqliteadapter.New(db)
			repo := &faultRepo{Repository: real, failOn: tc.failOn, err: injected}
			h := NewHydrator(repo, nil, Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			seedChannel(t, ctx, real)

			stream := synthStream("s-1", "b-1", "game-42", "Game 42")
			stream.Title = "a title"
			stream.Tags = []string{"English"}

			snap := h.persist(ctx, "b-1", stream)
			if snap == nil {
				t.Fatal("child-row failure must not nil the snapshot")
			}
			// The stream row write itself was untouched, so it must
			// still be reported regardless of which child failed.
			if snap.StreamID != "s-1" {
				t.Fatalf("StreamID = %q, want s-1 (child failure must not lose the stream row)", snap.StreamID)
			}

			switch tc.failOn {
			case "UpsertCategory":
				// The id is appended only after a successful upsert.
				if len(snap.CategoryIDs) != 0 {
					t.Errorf("CategoryIDs = %v, want empty when the upsert failed", snap.CategoryIDs)
				}
			case "LinkStreamCategory":
				// The link failed but the id is still reported to the matcher.
				if len(snap.CategoryIDs) != 1 {
					t.Errorf("CategoryIDs = %v, want [game-42] even when the link failed", snap.CategoryIDs)
				}
			case "UpsertTag":
				if len(snap.TagIDs) != 0 {
					t.Errorf("TagIDs = %v, want empty when the upsert failed", snap.TagIDs)
				}
			case "LinkStreamTag":
				if len(snap.TagIDs) != 1 {
					t.Errorf("TagIDs = %v, want 1 even when the link failed", snap.TagIDs)
				}
			}
		})
	}
}

// TestNewMetadataWatcher_DefaultsInterval pins the zero-value-safe
// default: an unset interval falls back to DefaultMetadataWatchInterval,
// while an explicit value is honored verbatim.
func TestNewMetadataWatcher_DefaultsInterval(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h, _ := newTestHydrator(t, nil)

	def := NewMetadataWatcher(h, MetadataWatchConfig{}, log)
	if def.interval != DefaultMetadataWatchInterval {
		t.Errorf("default interval = %v, want %v", def.interval, DefaultMetadataWatchInterval)
	}
	custom := NewMetadataWatcher(h, MetadataWatchConfig{Interval: 5 * time.Second}, log)
	if custom.interval != 5*time.Second {
		t.Errorf("custom interval = %v, want 5s", custom.interval)
	}
}

// TestMetadataWatcher_WatchStopsOnContextCancel pins that Watch honors
// its context: an already-canceled ctx makes the poll loop return
// promptly rather than block on the ticker. The interval is set far
// past the test timeout so a return can only come from the ctx.Done
// branch, not a tick.
func TestMetadataWatcher_WatchStopsOnContextCancel(t *testing.T) {
	h, _ := newTestHydrator(t, nil)
	w := NewMetadataWatcher(h, MetadataWatchConfig{Interval: time.Hour}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		w.Watch(ctx, "b-1", 1, WatchInitial{})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not return after context cancel")
	}
}

func TestMetadataWatcher_WatchRecordsChangedMetadataOnTick(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewHydrator(repo, nil, Config{Retries: 1, RetryDelay: time.Millisecond}, log)
	videoID := seedRecording(t, ctx, repo)
	if _, err := repo.CreateJob(ctx, &repository.JobInput{
		ID:            "job-1",
		VideoID:       videoID,
		BroadcasterID: "b-1",
	}); err != nil {
		t.Fatalf("seed active job: %v", err)
	}
	live := synthStream("s-watch", "b-1", "game-new", "Game New")
	live.Title = "New Title"
	h.twitch = &fakeStreamFetcher{pages: [][]twitch.Stream{{*live}}}
	w := NewMetadataWatcher(h, MetadataWatchConfig{Interval: 10 * time.Millisecond}, log)

	watchCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Watch(watchCtx, "b-1", videoID, WatchInitial{
			Title:       "Old Title",
			CategoryID:  "game-old",
			MediaOffset: staticMediaOffset{seconds: 42.25, ok: true},
		})
		close(done)
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Watch did not stop after cancellation")
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		changes, err := repo.ListVideoMetadataChanges(ctx, videoID)
		if err != nil {
			t.Fatalf("ListVideoMetadataChanges: %v", err)
		}
		if len(changes) == 1 {
			change := changes[0]
			if change.Title == nil || change.Title.Name != "New Title" {
				t.Fatalf("metadata title = %+v, want New Title", change.Title)
			}
			if change.Category == nil || change.Category.ID != "game-new" || change.Category.Name != "Game New" {
				t.Fatalf("metadata category = %+v, want game-new/Game New", change.Category)
			}
			if change.MediaOffsetSeconds == nil || *change.MediaOffsetSeconds != 42.25 {
				t.Fatalf("media_offset_seconds = %v, want 42.25", change.MediaOffsetSeconds)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Watch did not record changed metadata on a tick")
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
	if ev.MediaOffsetSeconds != nil {
		t.Fatalf("media_offset_seconds = %v, want nil when no media offset was provided", *ev.MediaOffsetSeconds)
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

func TestHydrator_LinkInitialVideoMetadata_StoresMediaOffset(t *testing.T) {
	ctx := context.Background()
	h, repo := newTestHydrator(t, nil)
	videoID := seedRecording(t, ctx, repo)
	offset := 12.5

	if err := h.LinkInitialVideoMetadata(ctx, videoID, ChannelUpdateMeta{
		Title:              "Opening",
		MediaOffsetSeconds: &offset,
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
	if events[0].MediaOffsetSeconds == nil || *events[0].MediaOffsetSeconds != 12.5 {
		t.Fatalf("media_offset_seconds = %v, want 12.5", events[0].MediaOffsetSeconds)
	}
}

func TestHydrator_RecordChannelUpdate_StoresResolvedMediaOffset(t *testing.T) {
	ctx := context.Background()
	h, repo := newTestHydrator(t, nil)
	videoID := seedRecording(t, ctx, repo)
	if _, err := repo.CreateJob(ctx, &repository.JobInput{
		ID:            "job-1",
		VideoID:       videoID,
		BroadcasterID: "b-1",
	}); err != nil {
		t.Fatalf("seed active job: %v", err)
	}
	resolver := &recordingMediaOffsetResolver{seconds: 66.5, ok: true}
	h.SetMediaOffsetResolver(resolver)

	if err := h.RecordChannelUpdate(ctx, "b-1", ChannelUpdateMeta{
		Title: "Webhook title",
	}); err != nil {
		t.Fatalf("record channel update: %v", err)
	}

	if resolver.calls != 1 || resolver.broadcasterID != "b-1" || resolver.videoID != videoID {
		t.Fatalf("resolver call = %d/%q/%d, want 1/b-1/%d",
			resolver.calls, resolver.broadcasterID, resolver.videoID, videoID)
	}
	events, err := repo.ListVideoMetadataChanges(ctx, videoID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly one event row, got %d", len(events))
	}
	if events[0].MediaOffsetSeconds == nil || *events[0].MediaOffsetSeconds != 66.5 {
		t.Fatalf("media_offset_seconds = %v, want 66.5", events[0].MediaOffsetSeconds)
	}
}

func TestHydrator_RecordChannelUpdate_LeavesMediaOffsetNullWhenResolverUnavailable(t *testing.T) {
	ctx := context.Background()
	h, repo := newTestHydrator(t, nil)
	videoID := seedRecording(t, ctx, repo)
	if _, err := repo.CreateJob(ctx, &repository.JobInput{
		ID:            "job-1",
		VideoID:       videoID,
		BroadcasterID: "b-1",
	}); err != nil {
		t.Fatalf("seed active job: %v", err)
	}
	h.SetMediaOffsetResolver(&recordingMediaOffsetResolver{ok: false})

	if err := h.RecordChannelUpdate(ctx, "b-1", ChannelUpdateMeta{
		Title: "Webhook title",
	}); err != nil {
		t.Fatalf("record channel update: %v", err)
	}

	events, err := repo.ListVideoMetadataChanges(ctx, videoID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly one event row, got %d", len(events))
	}
	if events[0].MediaOffsetSeconds != nil {
		t.Fatalf("media_offset_seconds = %v, want nil", *events[0].MediaOffsetSeconds)
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

	// SQLite stores text times at second resolution, so advance the test clock.
	base := time.Date(2026, 4, 12, 15, 0, 0, 0, time.UTC)
	calls := 0
	h.now = func() time.Time {
		calls++
		return base.Add(time.Duration(calls) * time.Second)
	}

	if err := h.LinkInitialVideoMetadata(ctx, videoID, ChannelUpdateMeta{
		Title:        "first",
		CategoryID:   "game-1",
		CategoryName: "First Game",
	}); err != nil {
		t.Fatalf("first link: %v", err)
	}
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
