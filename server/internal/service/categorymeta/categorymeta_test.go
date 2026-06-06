package categorymeta

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strconv"
	"testing"

	"github.com/befabri/replayvod/server/internal/igdb"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
)

type fakeIGDB struct {
	calls   [][]int64
	byID    map[int64]igdb.Game
	errOnce error
}

func (f *fakeIGDB) GetGames(_ context.Context, ids []int64) ([]igdb.Game, error) {
	call := append([]int64(nil), ids...)
	f.calls = append(f.calls, call)
	if f.errOnce != nil {
		err := f.errOnce
		f.errOnce = nil
		return nil, err
	}
	out := make([]igdb.Game, 0, len(ids))
	for _, id := range ids {
		if game, ok := f.byID[id]; ok {
			if game.ID == 0 {
				game.ID = id
			}
			out = append(out, game)
		}
	}
	return out, nil
}

func newCategoryMetaService(t *testing.T, fake *fakeIGDB) (*Service, repository.Repository) {
	t.Helper()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)
	svc := New(repo, fake, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.batchDelay = 0
	return svc, repo
}

func TestSyncMissing_FillsSummaryAndStorylineFallback(t *testing.T) {
	ctx := context.Background()
	fake := &fakeIGDB{byID: map[int64]igdb.Game{
		101: {Summary: "Summary description"},
		202: {Storyline: "Storyline fallback"},
	}}
	svc, repo := newCategoryMetaService(t, fake)

	seedCategory(t, repo, "game-a", "Alpha", "", nil)
	seedCategory(t, repo, "game-b", "Beta", "101", nil)
	seedCategory(t, repo, "game-c", "Gamma", "202", nil)
	existing := "Existing description"
	seedCategory(t, repo, "game-d", "Delta", "303", &existing)
	seedCategory(t, repo, "game-e", "Epsilon", "not-a-number", nil)

	synced, err := svc.SyncMissing(ctx)
	if err != nil {
		t.Fatalf("SyncMissing: %v", err)
	}
	if synced != 2 {
		t.Fatalf("synced = %d, want 2", synced)
	}
	if !reflect.DeepEqual(fake.calls, [][]int64{{101, 202}}) {
		t.Fatalf("IGDB calls = %v, want [[101 202]]", fake.calls)
	}
	assertDescription(t, repo, "game-b", "Summary description")
	assertDescription(t, repo, "game-c", "Storyline fallback")
	assertDescription(t, repo, "game-d", existing)
}

func TestSyncMissing_BatchesTo100(t *testing.T) {
	ctx := context.Background()
	fake := &fakeIGDB{byID: make(map[int64]igdb.Game, 250)}
	svc, repo := newCategoryMetaService(t, fake)
	for i := range 250 {
		igdbID := int64(10000 + i)
		fake.byID[igdbID] = igdb.Game{Summary: "description " + strconv.FormatInt(igdbID, 10)}
		seedCategory(t, repo, "game-"+pad4(i), "Game "+pad4(i), strconv.FormatInt(igdbID, 10), nil)
	}

	synced, err := svc.SyncMissing(ctx)
	if err != nil {
		t.Fatalf("SyncMissing: %v", err)
	}
	if synced != 250 {
		t.Fatalf("synced = %d, want 250", synced)
	}
	if len(fake.calls) != 3 {
		t.Fatalf("calls = %d, want 3 batches", len(fake.calls))
	}
	wantSizes := []int{100, 100, 50}
	for i, size := range wantSizes {
		if len(fake.calls[i]) != size {
			t.Fatalf("batch %d size = %d, want %d", i, len(fake.calls[i]), size)
		}
	}
}

func TestSyncMissing_IGDBErrorAborts(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("igdb failed")
	fake := &fakeIGDB{byID: map[int64]igdb.Game{}, errOnce: sentinel}
	svc, repo := newCategoryMetaService(t, fake)
	seedCategory(t, repo, "game-a", "Alpha", "101", nil)

	synced, err := svc.SyncMissing(ctx)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want sentinel", err)
	}
	if synced != 0 {
		t.Fatalf("synced = %d, want 0", synced)
	}
}

func TestSyncMissing_CachesNoDescriptionAndMissingGames(t *testing.T) {
	ctx := context.Background()
	fake := &fakeIGDB{byID: map[int64]igdb.Game{
		101: {},
	}}
	svc, repo := newCategoryMetaService(t, fake)
	seedCategory(t, repo, "game-no-description", "No Description", "101", nil)
	seedCategory(t, repo, "game-not-returned", "Not Returned", "202", nil)

	synced, err := svc.SyncMissing(ctx)
	if err != nil {
		t.Fatalf("first SyncMissing: %v", err)
	}
	if synced != 0 {
		t.Fatalf("synced = %d, want 0", synced)
	}
	if !reflect.DeepEqual(fake.calls, [][]int64{{101, 202}}) {
		t.Fatalf("IGDB calls = %v, want [[101 202]]", fake.calls)
	}
	assertDescriptionChecked(t, repo, "game-no-description")
	assertDescriptionChecked(t, repo, "game-not-returned")

	synced, err = svc.SyncMissing(ctx)
	if err != nil {
		t.Fatalf("second SyncMissing: %v", err)
	}
	if synced != 0 {
		t.Fatalf("synced second = %d, want 0", synced)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("IGDB calls after cached misses = %v, want no second call", fake.calls)
	}
}

func seedCategory(t *testing.T, repo repository.Repository, id, name, igdbID string, description *string) {
	t.Helper()
	var igdbPtr *string
	if igdbID != "" {
		igdbPtr = &igdbID
	}
	if _, err := repo.UpsertCategory(context.Background(), &repository.Category{
		ID:          id,
		Name:        name,
		IGDBID:      igdbPtr,
		Description: description,
	}); err != nil {
		t.Fatalf("seed category %s: %v", id, err)
	}
}

func assertDescriptionChecked(t *testing.T, repo repository.Repository, id string) {
	t.Helper()
	got, err := repo.GetCategory(context.Background(), id)
	if err != nil {
		t.Fatalf("GetCategory(%s): %v", id, err)
	}
	if got.Description != nil {
		t.Fatalf("%s description = %v, want nil", id, got.Description)
	}
	if got.DescriptionCheckedAt == nil {
		t.Fatalf("%s description_checked_at = nil, want timestamp", id)
	}
}

func assertDescription(t *testing.T, repo repository.Repository, id, want string) {
	t.Helper()
	got, err := repo.GetCategory(context.Background(), id)
	if err != nil {
		t.Fatalf("GetCategory(%s): %v", id, err)
	}
	if got.Description == nil || *got.Description != want {
		t.Fatalf("%s description = %v, want %q", id, got.Description, want)
	}
}

func pad4(i int) string {
	s := []byte("0000")
	for p := 3; p >= 0 && i > 0; p-- {
		s[p] = byte('0' + i%10)
		i /= 10
	}
	return string(s)
}
