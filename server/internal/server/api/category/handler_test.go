package category

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
	"github.com/befabri/trpcgo"
)

func requireTRPCCode(t *testing.T, err error, want trpcgo.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want tRPC code %v", want)
	}
	var te *trpcgo.Error
	if !errors.As(err, &te) {
		t.Fatalf("error = %T (%v), want *trpcgo.Error", err, err)
	}
	if te.Code != want {
		t.Fatalf("tRPC code = %v, want %v", te.Code, want)
	}
}

func newHandler(t *testing.T) *Handler {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	return NewHandler(New(repo, nil, log), log)
}

func TestGetByID_NotFound(t *testing.T) {
	h := newHandler(t)
	_, err := h.GetByID(context.Background(), GetByIDInput{ID: "does-not-exist"})
	requireTRPCCode(t, err, trpcgo.CodeNotFound)
}

func TestList_Empty(t *testing.T) {
	h := newHandler(t)
	got, err := h.List(context.Background())
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if got == nil {
		t.Fatal("result is nil, want non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestListWithVideos_FiltersCatalogOnlyAndDeletedVideoCategories(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))

	for _, c := range []repository.Category{
		{ID: "cat-visible", Name: "Visible Game"},
		{ID: "cat-searched", Name: "Searched Only"},
		{ID: "cat-deleted", Name: "Deleted Only"},
	} {
		c := c
		if err := seedCategory(repo, &c); err != nil {
			t.Fatalf("seed category %s: %v", c.ID, err)
		}
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID:    "bc-cats",
		BroadcasterLogin: "cats",
		BroadcasterName:  "Cats",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	visible, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID:         "job-visible-cat",
		Filename:      "visible-cat",
		DisplayName:   "Cats",
		Status:        repository.VideoStatusDone,
		Quality:       repository.QualityHigh,
		BroadcasterID: "bc-cats",
		Language:      "en",
	})
	if err != nil {
		t.Fatalf("create visible video: %v", err)
	}
	if err := repo.LinkVideoCategory(ctx, visible.ID, "cat-visible"); err != nil {
		t.Fatalf("link visible category: %v", err)
	}

	deleted, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID:         "job-deleted-cat",
		Filename:      "deleted-cat",
		DisplayName:   "Cats",
		Status:        repository.VideoStatusDone,
		Quality:       repository.QualityHigh,
		BroadcasterID: "bc-cats",
		Language:      "en",
	})
	if err != nil {
		t.Fatalf("create deleted video: %v", err)
	}
	if err := repo.LinkVideoCategory(ctx, deleted.ID, "cat-deleted"); err != nil {
		t.Fatalf("link deleted category: %v", err)
	}
	if err := repo.SoftDeleteVideo(ctx, deleted.ID, repository.DeletionKindManual); err != nil {
		t.Fatalf("soft delete video: %v", err)
	}

	fake := &fakeCategorySearcher{err: errors.New("searchWithVideos must not call Twitch")}
	h := NewHandler(New(repo, fake, log), log)
	all, err := h.List(ctx)
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("category.list should remain the full mirrored catalog, got %d rows", len(all))
	}

	got, err := h.ListWithVideos(ctx)
	if err != nil {
		t.Fatalf("ListWithVideos error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %+v", len(got), got)
	}
	if got[0].ID != "cat-visible" {
		t.Fatalf("ID = %q, want cat-visible", got[0].ID)
	}

	got, err = h.SearchWithVideos(ctx, SearchInput{Query: "Visible", Limit: 10})
	if err != nil {
		t.Fatalf("SearchWithVideos error = %v", err)
	}
	if len(got) != 1 || got[0].ID != "cat-visible" {
		t.Fatalf("SearchWithVideos visible results = %+v, want cat-visible only", got)
	}
	got, err = h.SearchWithVideos(ctx, SearchInput{Query: "Searched", Limit: 10})
	if err != nil {
		t.Fatalf("SearchWithVideos searched-only error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("SearchWithVideos catalog-only results = %+v, want none", got)
	}
	if fake.calls != 0 {
		t.Fatalf("SearchWithVideos Twitch calls = %d, want 0", fake.calls)
	}
}

func TestGetDetail_ReturnsDescriptionAndVisibleVideoStats(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))

	description := "A tactical game about careful choices."
	if err := seedCategory(repo, &repository.Category{
		ID:          "cat-detail",
		Name:        "Detail Game",
		Description: &description,
	}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID:    "bc-detail",
		BroadcasterLogin: "detail",
		BroadcasterName:  "Detail",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	done, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID:         "job-detail-done",
		Filename:      "detail-done",
		DisplayName:   "Detail",
		Status:        repository.VideoStatusDone,
		Quality:       repository.QualityHigh,
		BroadcasterID: "bc-detail",
		Language:      "en",
	})
	if err != nil {
		t.Fatalf("create done video: %v", err)
	}
	if err := repo.LinkVideoCategory(ctx, done.ID, "cat-detail"); err != nil {
		t.Fatalf("link done category: %v", err)
	}
	if err := repo.MarkVideoDone(ctx, done.ID, 60, 1_500, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("mark done: %v", err)
	}

	running, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID:         "job-detail-running",
		Filename:      "detail-running",
		DisplayName:   "Detail",
		Status:        repository.VideoStatusRunning,
		Quality:       repository.QualityHigh,
		BroadcasterID: "bc-detail",
		Language:      "en",
	})
	if err != nil {
		t.Fatalf("create running video: %v", err)
	}
	if err := repo.LinkVideoCategory(ctx, running.ID, "cat-detail"); err != nil {
		t.Fatalf("link running category: %v", err)
	}

	deleted, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID:         "job-detail-deleted",
		Filename:      "detail-deleted",
		DisplayName:   "Detail",
		Status:        repository.VideoStatusDone,
		Quality:       repository.QualityHigh,
		BroadcasterID: "bc-detail",
		Language:      "en",
	})
	if err != nil {
		t.Fatalf("create deleted video: %v", err)
	}
	if err := repo.LinkVideoCategory(ctx, deleted.ID, "cat-detail"); err != nil {
		t.Fatalf("link deleted category: %v", err)
	}
	if err := repo.MarkVideoDone(ctx, deleted.ID, 60, 9_000, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("mark deleted done: %v", err)
	}
	if err := repo.SoftDeleteVideo(ctx, deleted.ID, repository.DeletionKindManual); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	h := NewHandler(New(repo, nil, log), log)
	got, err := h.GetDetail(ctx, GetByIDInput{ID: "cat-detail"})
	if err != nil {
		t.Fatalf("GetDetail error = %v", err)
	}
	if got.ID != "cat-detail" || got.Name != "Detail Game" {
		t.Fatalf("category identity = (%q, %q)", got.ID, got.Name)
	}
	if got.Description == nil || *got.Description != description {
		t.Fatalf("description = %v, want %q", got.Description, description)
	}
	if got.VideoCount != 2 {
		t.Fatalf("video_count = %d, want 2", got.VideoCount)
	}
	if got.TotalSize != 1_500 {
		t.Fatalf("total_size = %d, want 1500", got.TotalSize)
	}
}

func TestSearch_EmptyQueryReturnsAll(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))

	for _, c := range []repository.Category{
		{ID: "c1", Name: "Chess", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "c2", Name: "Zelda", CreatedAt: time.Now(), UpdatedAt: time.Now()},
	} {
		c := c
		if err := seedCategory(repo, &c); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	h := NewHandler(New(repo, nil, log), log)
	got, err := h.Search(context.Background(), SearchInput{Query: "", Limit: 10})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestSearch_LimitDefaultsTo50(t *testing.T) {
	h := newHandler(t)
	_, err := h.Search(context.Background(), SearchInput{Query: "x", Limit: 0})
	if err != nil {
		t.Fatalf("Search (limit=0) error = %v", err)
	}
}

func TestSearch_QueryFilters(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))

	for _, c := range []repository.Category{
		{ID: "g1", Name: "Chess", CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{ID: "g2", Name: "Minecraft", CreatedAt: time.Now(), UpdatedAt: time.Now()},
	} {
		c := c
		if err := seedCategory(repo, &c); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	h := NewHandler(New(repo, nil, log), log)
	got, err := h.Search(context.Background(), SearchInput{Query: "Chess", Limit: 10})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len = %d, want 1", len(got))
	}
	if len(got) > 0 && got[0].ID != "g1" {
		t.Errorf("ID = %q, want g1", got[0].ID)
	}
}

func TestSearch_TwitchResultIsReturnedCachedAndReused(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	fake := &fakeCategorySearcher{
		results: []twitch.SearchCategory{
			{
				ID:        "509658",
				Name:      "Just Chatting",
				BoxArtURL: "https://static-cdn.jtvnw.net/ttv-boxart/509658-{width}x{height}.jpg",
			},
		},
	}

	svc := New(repo, fake, log, WithClock(func() time.Time { return now }))
	h := NewHandler(svc, log)
	got, err := h.Search(context.Background(), SearchInput{Query: "  Just  ", Limit: 10})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("twitch calls = %d, want 1", fake.calls)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].ID != "509658" || got[0].Name != "Just Chatting" {
		t.Fatalf("category = (%q, %q), want Just Chatting", got[0].ID, got[0].Name)
	}

	cached, err := repo.GetCategory(context.Background(), "509658")
	if err != nil {
		t.Fatalf("GetCategory cached error = %v", err)
	}
	if cached.BoxArtURL == nil || *cached.BoxArtURL == "" {
		t.Fatal("cached BoxArtURL is empty")
	}

	searchCache, err := repo.GetCategorySearchCache(context.Background(), "just")
	if err != nil {
		t.Fatalf("GetCategorySearchCache error = %v", err)
	}
	if len(searchCache.CategoryIDs) != 1 || searchCache.CategoryIDs[0] != "509658" {
		t.Fatalf("search cache IDs = %v, want [509658]", searchCache.CategoryIDs)
	}
	if !searchCache.ExpiresAt.Equal(now.Add(categorySearchCacheTTL)) {
		t.Fatalf("cache expires_at = %v, want %v", searchCache.ExpiresAt, now.Add(categorySearchCacheTTL))
	}

	now = now.Add(30 * time.Minute)
	fake.err = errors.New("twitch should not be called on fresh cache hit")
	got, err = h.Search(context.Background(), SearchInput{Query: "just", Limit: 10})
	if err != nil {
		t.Fatalf("cached Search error = %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("twitch calls after cache hit = %d, want 1", fake.calls)
	}
	if len(got) != 1 || got[0].ID != "509658" {
		t.Fatalf("cached results = %+v, want 509658", got)
	}

	searchCache, err = repo.GetCategorySearchCache(context.Background(), "just")
	if err != nil {
		t.Fatalf("GetCategorySearchCache after hit error = %v", err)
	}
	if !searchCache.LastAccessedAt.Equal(now) {
		t.Fatalf("cache last_accessed_at = %v, want %v", searchCache.LastAccessedAt, now)
	}
}

func TestSearch_EmptyQueryDoesNotCallTwitch(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	fake := &fakeCategorySearcher{}

	h := NewHandler(New(repo, fake, log), log)
	_, err := h.Search(context.Background(), SearchInput{Query: "", Limit: 10})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if fake.calls != 0 {
		t.Fatalf("twitch calls = %d, want 0", fake.calls)
	}
}

func TestSearch_OneRuneQueryDoesNotCallTwitch(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	fake := &fakeCategorySearcher{}

	h := NewHandler(New(repo, fake, log), log)
	got, err := h.Search(context.Background(), SearchInput{Query: "é", Limit: 10})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if fake.calls != 0 {
		t.Fatalf("twitch calls = %d, want 0", fake.calls)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}

func TestSearch_RemoteErrorWithoutFallbackReturnsError(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	fake := &fakeCategorySearcher{err: errors.New("twitch down")}

	svc := New(repo, fake, log)
	_, err := svc.Search(context.Background(), "valorant", 10)
	if err == nil {
		t.Fatal("Search error = nil, want remote error")
	}
	if fake.calls != 1 {
		t.Fatalf("twitch calls = %d, want 1", fake.calls)
	}
}

func TestSearch_RemoteErrorUsesStaleCacheFallback(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	if err := seedCategory(repo, &repository.Category{ID: "cached-1", Name: "Cached Only"}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	if _, err := repo.UpsertCategorySearchCache(context.Background(), repository.CategorySearchCacheInput{
		NormalizedQuery: "rpg",
		CategoryIDs:     []string{"cached-1"},
		ExpiresAt:       now.Add(-time.Minute),
		LastAccessedAt:  now.Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("seed search cache: %v", err)
	}
	fake := &fakeCategorySearcher{err: errors.New("twitch down")}

	svc := New(repo, fake, log, WithClock(func() time.Time { return now }))
	got, err := svc.Search(context.Background(), "rpg", 10)
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("twitch calls = %d, want 1", fake.calls)
	}
	if len(got) != 1 || got[0].ID != "cached-1" {
		t.Fatalf("results = %+v, want cached-1", got)
	}
}

func TestSearch_RemoteErrorDoesNotUseStaleNegativeCacheFallback(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	if _, err := repo.UpsertCategorySearchCache(context.Background(), repository.CategorySearchCacheInput{
		NormalizedQuery: "missing",
		CategoryIDs:     []string{},
		ExpiresAt:       now.Add(-time.Minute),
		LastAccessedAt:  now.Add(-2 * time.Hour),
	}); err != nil {
		t.Fatalf("seed search cache: %v", err)
	}
	fake := &fakeCategorySearcher{err: errors.New("twitch down")}

	svc := New(repo, fake, log, WithClock(func() time.Time { return now }))
	_, err := svc.Search(context.Background(), "missing", 10)
	if err == nil {
		t.Fatal("Search error = nil, want remote error when only fallback is stale negative cache")
	}
	if fake.calls != 1 {
		t.Fatalf("twitch calls = %d, want 1", fake.calls)
	}
}

func TestSearch_NegativeResultsAreCached(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	fake := &fakeCategorySearcher{}

	h := NewHandler(New(repo, fake, log, WithClock(func() time.Time { return now })), log)
	got, err := h.Search(context.Background(), SearchInput{Query: "zzzz-missing", Limit: 10})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
	if fake.calls != 1 {
		t.Fatalf("twitch calls = %d, want 1", fake.calls)
	}

	searchCache, err := repo.GetCategorySearchCache(context.Background(), "zzzz-missing")
	if err != nil {
		t.Fatalf("GetCategorySearchCache error = %v", err)
	}
	if len(searchCache.CategoryIDs) != 0 {
		t.Fatalf("search cache IDs = %v, want empty", searchCache.CategoryIDs)
	}
	if !searchCache.ExpiresAt.Equal(now.Add(categorySearchNegativeCacheTTL)) {
		t.Fatalf("cache expires_at = %v, want %v", searchCache.ExpiresAt, now.Add(categorySearchNegativeCacheTTL))
	}

	fake.err = errors.New("twitch should not be called on negative cache hit")
	got, err = h.Search(context.Background(), SearchInput{Query: "zzzz-missing", Limit: 10})
	if err != nil {
		t.Fatalf("cached Search error = %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("twitch calls after negative cache hit = %d, want 1", fake.calls)
	}
	if len(got) != 0 {
		t.Fatalf("cached len = %d, want 0", len(got))
	}
}

func TestSearch_RemoteResultsAreMergedBeforeLimit(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	for _, c := range []repository.Category{
		{ID: "local-1", Name: "Alpha Three"},
		{ID: "local-2", Name: "Alpha Two"},
	} {
		c := c
		if err := seedCategory(repo, &c); err != nil {
			t.Fatalf("seed category: %v", err)
		}
	}
	fake := &fakeCategorySearcher{
		results: []twitch.SearchCategory{{ID: "remote-1", Name: "Alpha"}},
	}

	h := NewHandler(New(repo, fake, log, WithClock(func() time.Time { return now })), log)
	got, err := h.Search(context.Background(), SearchInput{Query: "alpha", Limit: 2})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("twitch calls = %d, want 1", fake.calls)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ID != "remote-1" {
		t.Fatalf("first result ID = %q, want remote-1; results=%+v", got[0].ID, got)
	}
}

func TestSearch_RemoteTokenMatchDoesNotOutrankLocalSubstring(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	if err := seedCategory(repo, &repository.Category{ID: "local-1", Name: "The Alpha Two Show"}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	fake := &fakeCategorySearcher{
		results: []twitch.SearchCategory{{ID: "remote-1", Name: "Alpha One Two"}},
	}

	h := NewHandler(New(repo, fake, log, WithClock(func() time.Time { return now })), log)
	got, err := h.Search(context.Background(), SearchInput{Query: "alpha two", Limit: 2})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	if got[0].ID != "local-1" {
		t.Fatalf("first result ID = %q, want local-1; results=%+v", got[0].ID, got)
	}
}

func TestSearch_RemoteDuplicateRefreshesReturnedMetadata(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	if err := seedCategory(repo, &repository.Category{ID: "alpha-1", Name: "Alpha"}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	boxArtURL := "https://static-cdn.jtvnw.net/ttv-boxart/alpha-1-{width}x{height}.jpg"
	fake := &fakeCategorySearcher{
		results: []twitch.SearchCategory{{
			ID:        "alpha-1",
			Name:      "Alpha",
			BoxArtURL: boxArtURL,
		}},
	}

	h := NewHandler(New(repo, fake, log, WithClock(func() time.Time { return now })), log)
	got, err := h.Search(context.Background(), SearchInput{Query: "alpha", Limit: 10})
	if err != nil {
		t.Fatalf("Search error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].ID != "alpha-1" {
		t.Fatalf("ID = %q, want alpha-1", got[0].ID)
	}
	if got[0].BoxArtURL == nil || *got[0].BoxArtURL != boxArtURL {
		t.Fatalf("BoxArtURL = %v, want %q", got[0].BoxArtURL, boxArtURL)
	}
}

func TestWriteCategorySearchCacheIgnoresRequestCancellation(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	svc := New(repo, nil, log, WithClock(func() time.Time { return now }))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc.writeCategorySearchCache(ctx, "cancelled", []string{"cat-1"}, now)

	got, err := repo.GetCategorySearchCache(context.Background(), "cancelled")
	if err != nil {
		t.Fatalf("GetCategorySearchCache: %v", err)
	}
	if len(got.CategoryIDs) != 1 || got.CategoryIDs[0] != "cat-1" {
		t.Fatalf("category_ids = %v, want [cat-1]", got.CategoryIDs)
	}
}

func TestToResponse_FieldMapping(t *testing.T) {
	boxArt := "https://example.com/box.jpg"
	igdb := "42"
	description := "A category description."
	c := &repository.Category{
		ID:          "cat1",
		Name:        "My Game",
		BoxArtURL:   &boxArt,
		IGDBID:      &igdb,
		Description: &description,
	}
	r := toResponse(c)
	if r.ID != c.ID {
		t.Errorf("ID: %q != %q", r.ID, c.ID)
	}
	if r.Name != c.Name {
		t.Errorf("Name: %q != %q", r.Name, c.Name)
	}
	if r.BoxArtURL == nil || *r.BoxArtURL != boxArt {
		t.Errorf("BoxArtURL: %v, want %q", r.BoxArtURL, boxArt)
	}
	if r.IGDBID == nil || *r.IGDBID != igdb {
		t.Errorf("IGDBID: %v, want %q", r.IGDBID, igdb)
	}
	if r.Description == nil || *r.Description != description {
		t.Errorf("Description: %v, want %q", r.Description, description)
	}
}

func seedCategory(repo repository.Repository, c *repository.Category) error {
	type categoryUpserter interface {
		UpsertCategory(ctx context.Context, c *repository.Category) (*repository.Category, error)
	}
	u, ok := repo.(categoryUpserter)
	if !ok {
		return errors.New("repo does not expose UpsertCategory")
	}
	_, err := u.UpsertCategory(context.Background(), c)
	return err
}

type fakeCategorySearcher struct {
	calls   int
	results []twitch.SearchCategory
	err     error
	queries []string
	firsts  []int
}

func (f *fakeCategorySearcher) SearchCategories(_ context.Context, params *twitch.SearchCategoriesParams) ([]twitch.SearchCategory, twitch.Pagination, error) {
	f.calls++
	if params != nil {
		f.queries = append(f.queries, params.Query)
		f.firsts = append(f.firsts, params.First)
	}
	return f.results, twitch.Pagination{}, f.err
}
