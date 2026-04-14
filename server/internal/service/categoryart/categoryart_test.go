package categoryart

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// fakeGames records every GetGames call the Service makes and returns
// a fixed response keyed by the requested IDs. Kept minimal — tests
// that need a different response per call seed the map accordingly.
type fakeGames struct {
	calls   [][]string
	byID    map[string]string // id → box_art_url
	errOnce error             // returned the first call, then cleared
}

func newFakeGames(byID map[string]string) *fakeGames {
	return &fakeGames{byID: byID}
}

func (f *fakeGames) GetGames(_ context.Context, params *twitch.GetGamesParams) ([]twitch.Game, error) {
	ids := append([]string(nil), params.ID...)
	f.calls = append(f.calls, ids)
	if f.errOnce != nil {
		err := f.errOnce
		f.errOnce = nil
		return nil, err
	}
	out := make([]twitch.Game, 0, len(ids))
	for _, id := range ids {
		if url, ok := f.byID[id]; ok {
			out = append(out, twitch.Game{ID: id, Name: "n-" + id, BoxArtURL: url})
		}
	}
	return out, nil
}

func newServiceWithRepo(t *testing.T, fake *fakeGames) (*Service, repository.Repository) {
	t.Helper()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	return New(repo, fake, slog.New(slog.NewTextHandler(io.Discard, nil))), repo
}

// TestEnrich_UpdatesSingleCategory pins the eager path: one id in, one
// Helix call, one UPDATE landed. Verifies the end-to-end wire from the
// Hydrator's on-first-observation hook down to the box_art_url column.
func TestEnrich_UpdatesSingleCategory(t *testing.T) {
	ctx := context.Background()
	fake := newFakeGames(map[string]string{"g-1": "https://static-cdn.jtvnw.net/ttv-boxart/g-1-{width}x{height}.jpg"})
	svc, repo := newServiceWithRepo(t, fake)

	if _, err := repo.UpsertCategory(ctx, &repository.Category{ID: "g-1", Name: "Game One"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := svc.Enrich(ctx, "g-1"); err != nil {
		t.Fatalf("enrich: %v", err)
	}
	got, err := repo.GetCategory(ctx, "g-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.BoxArtURL == nil || *got.BoxArtURL != "https://static-cdn.jtvnw.net/ttv-boxart/g-1-{width}x{height}.jpg" {
		t.Errorf("box_art_url not written: got %v", got.BoxArtURL)
	}
	if len(fake.calls) != 1 || len(fake.calls[0]) != 1 || fake.calls[0][0] != "g-1" {
		t.Errorf("expected one GetGames call with [g-1], got %v", fake.calls)
	}
}

// TestEnrich_UnknownCategoryLeavesRowAlone documents the degraded path:
// Twitch returns no match for an ID (game merged/removed). The existing
// NULL box_art_url stays NULL — we don't write an empty string on top.
func TestEnrich_UnknownCategoryLeavesRowAlone(t *testing.T) {
	ctx := context.Background()
	fake := newFakeGames(map[string]string{}) // nothing matches
	svc, repo := newServiceWithRepo(t, fake)

	if _, err := repo.UpsertCategory(ctx, &repository.Category{ID: "g-ghost", Name: "Ghost"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := svc.Enrich(ctx, "g-ghost"); err != nil {
		t.Fatalf("enrich: %v", err)
	}
	got, err := repo.GetCategory(ctx, "g-ghost")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.BoxArtURL != nil {
		t.Errorf("expected BoxArtURL to remain nil, got %q", *got.BoxArtURL)
	}
}

// TestEnrich_HelixErrorPropagates pins the contract change: Helix
// failures bubble up so callers can decide whether to log, retry, or
// ignore. The Hydrator currently log-and-continues on the webhook
// path; the scheduled task will re-try on its next tick.
func TestEnrich_HelixErrorPropagates(t *testing.T) {
	ctx := context.Background()
	fake := newFakeGames(map[string]string{})
	fake.errOnce = errors.New("helix 503")
	svc, repo := newServiceWithRepo(t, fake)

	if _, err := repo.UpsertCategory(ctx, &repository.Category{ID: "g-err", Name: "Err"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := svc.Enrich(ctx, "g-err"); err == nil {
		t.Fatal("expected Helix error to propagate, got nil")
	}
}

// TestSyncMissing_BatchesTo100 pins the /helix/games batching: 250 ids
// must fan out to exactly three calls of [100, 100, 50]. Any off-by-
// one in the chunking would either miss rows or exceed Twitch's 100/id
// limit.
func TestSyncMissing_BatchesTo100(t *testing.T) {
	ctx := context.Background()
	byID := make(map[string]string, 250)
	for i := range 250 {
		id := fmtID(i)
		byID[id] = "art-" + id
	}
	fake := newFakeGames(byID)
	svc, repo := newServiceWithRepo(t, fake)

	for i := range 250 {
		id := fmtID(i)
		if _, err := repo.UpsertCategory(ctx, &repository.Category{ID: id, Name: "N" + id}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	synced, err := svc.SyncMissing(ctx)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if synced != 250 {
		t.Errorf("synced: want 250, got %d", synced)
	}
	if len(fake.calls) != 3 {
		t.Fatalf("helix calls: want 3 batches, got %d (%v)", len(fake.calls), fake.calls)
	}
	wantSizes := []int{100, 100, 50}
	for i, size := range wantSizes {
		if len(fake.calls[i]) != size {
			t.Errorf("batch %d: want %d ids, got %d", i, size, len(fake.calls[i]))
		}
	}
}

// TestSyncMissing_NoMissingIsNoOp asserts the empty path takes zero
// Helix calls. Without this the scheduled task would make a pointless
// /helix/games call every interval when everything is already synced.
func TestSyncMissing_NoMissingIsNoOp(t *testing.T) {
	ctx := context.Background()
	fake := newFakeGames(map[string]string{})
	svc, _ := newServiceWithRepo(t, fake)

	synced, err := svc.SyncMissing(ctx)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if synced != 0 {
		t.Errorf("synced: want 0, got %d", synced)
	}
	if len(fake.calls) != 0 {
		t.Errorf("expected no Helix calls, got %d", len(fake.calls))
	}
}

// TestSyncMissing_HelixErrorAborts differs from Enrich: the scheduled
// task wants to surface Helix failures so the scheduler marks the run
// as failed and operators can diagnose. Partial progress is preserved
// — count reflects rows written before the error.
func TestSyncMissing_HelixErrorAborts(t *testing.T) {
	ctx := context.Background()
	fake := newFakeGames(map[string]string{})
	fake.errOnce = errors.New("helix 500")
	svc, repo := newServiceWithRepo(t, fake)

	if _, err := repo.UpsertCategory(ctx, &repository.Category{ID: "g-1", Name: "N"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	synced, err := svc.SyncMissing(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if synced != 0 {
		t.Errorf("synced before error: want 0, got %d", synced)
	}
}

func fmtID(i int) string {
	// Zero-padded for stable ordering in the batches slice.
	return "g-" + pad4(i)
}

func pad4(i int) string {
	s := []byte("0000")
	for p := 3; p >= 0 && i > 0; p-- {
		s[p] = byte('0' + i%10)
		i /= 10
	}
	return string(s)
}
