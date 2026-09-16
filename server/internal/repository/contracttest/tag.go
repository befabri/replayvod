package contracttest

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func tagNames(tags []repository.Tag) []string {
	out := make([]string, len(tags))
	for i, tag := range tags {
		out[i] = tag.Name
	}
	return out
}

func testTagsAndVideoTags(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	beta, err := repo.UpsertTag(ctx, "beta")
	if err != nil {
		t.Fatal(err)
	}
	alpha, err := repo.UpsertTag(ctx, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetTag(ctx, alpha.ID); err != nil || got.Name != "alpha" || got.CreatedAt.IsZero() {
		t.Fatalf("tag = %+v, %v", got, err)
	}
	if _, err := repo.GetTag(ctx, alpha.ID+beta.ID+1); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing tag: %v", err)
	}
	if got, err := repo.GetTagByName(ctx, "beta"); err != nil || got.ID != beta.ID {
		t.Fatalf("tag by name = %+v, %v", got, err)
	}
	if _, err := repo.GetTagByName(ctx, "gamma"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing tag name: %v", err)
	}
	tags, err := repo.ListTags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, tagNames(tags), []string{"alpha", "beta"})
	SeedUserChannel(t, ctx, repo, "owner", "execution-channel")
	v, err := repo.CreateVideo(ctx, executionInput("tagged"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{beta.ID, alpha.ID, beta.ID} {
		if err := repo.LinkVideoTag(ctx, v.ID, id); err != nil {
			t.Fatalf("link tag %d: %v", id, err)
		}
	}
	tags, err = repo.ListTagsForVideo(ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, tagNames(tags), []string{"alpha", "beta"})
	if tags, err := repo.ListTagsForVideo(ctx, v.ID+1); err != nil || len(tags) != 0 {
		t.Fatalf("tags of an unknown video = %+v, %v", tags, err)
	}
}

func testCategoryLookupAndSearchCache(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	for _, c := range []repository.Category{{ID: "z", Name: "Zelda"}, {ID: "a", Name: "Apex"}} {
		if _, err := repo.UpsertCategory(ctx, &c); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := repo.GetCategoryByName(ctx, "Apex"); err != nil || got.ID != "a" {
		t.Fatalf("category by name = %+v, %v", got, err)
	}
	if _, err := repo.GetCategoryByName(ctx, "Tetris"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing category name: %v", err)
	}
	categories, err := repo.ListCategories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, categoryNamesOf(categories), []string{"Apex", "Zelda"})
	now := time.Now().UTC().Truncate(time.Second)
	for _, in := range []repository.CategorySearchCacheInput{
		{NormalizedQuery: "fresh", CategoryIDs: []string{"a"}, ExpiresAt: now.Add(time.Hour), LastAccessedAt: now.Add(-time.Hour)},
		{NormalizedQuery: "stale", CategoryIDs: []string{"z"}, ExpiresAt: now.Add(-time.Hour), LastAccessedAt: now.Add(-2 * time.Hour)},
	} {
		if _, err := repo.UpsertCategorySearchCache(ctx, in); err != nil {
			t.Fatalf("cache %s: %v", in.NormalizedQuery, err)
		}
	}
	if err := repo.TouchCategorySearchCache(ctx, "fresh", now); err != nil {
		t.Fatal(err)
	}
	if err := repo.TouchCategorySearchCache(ctx, "missing", now); err != nil {
		t.Fatalf("touching an absent query: %v", err)
	}
	if got, err := repo.GetCategorySearchCache(ctx, "fresh"); err != nil || !got.LastAccessedAt.Equal(now) {
		t.Fatalf("touched cache = %+v, %v", got, err)
	}
	if err := repo.DeleteExpiredCategorySearchCache(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetCategorySearchCache(ctx, "stale"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expired cache survived: %v", err)
	}
	if got, err := repo.GetCategorySearchCache(ctx, "fresh"); err != nil || len(got.CategoryIDs) != 1 || got.CategoryIDs[0] != "a" {
		t.Fatalf("live cache lost: %+v, %v", got, err)
	}
}

func testPruneCategorySearchCache(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	now := time.Now().UTC().Truncate(time.Second)
	// Insertion order is interleaved with access recency so that neither the
	// oldest nor the newest rows are the ones expected to survive.
	access := map[string]time.Duration{"hot": -time.Hour, "stale": -4 * time.Hour, "recent": -2 * time.Hour, "old": -3 * time.Hour}
	cache := func(query string) {
		t.Helper()
		if _, err := repo.UpsertCategorySearchCache(ctx, repository.CategorySearchCacheInput{
			NormalizedQuery: query, CategoryIDs: []string{query}, ExpiresAt: now.Add(time.Hour), LastAccessedAt: now.Add(access[query]),
		}); err != nil {
			t.Fatalf("cache %s: %v", query, err)
		}
	}
	for _, query := range []string{"hot", "stale", "recent", "old"} {
		cache(query)
	}
	prune := func(maxRows int, want ...string) {
		t.Helper()
		if err := repo.PruneCategorySearchCache(ctx, maxRows); err != nil {
			t.Fatalf("prune to %d: %v", maxRows, err)
		}
		for _, query := range []string{"hot", "stale", "recent", "old"} {
			_, err := repo.GetCategorySearchCache(ctx, query)
			switch {
			case err != nil && !errors.Is(err, repository.ErrNotFound):
				t.Fatal(err)
			case (err == nil) != slices.Contains(want, query):
				t.Fatalf("after pruning to %d, %s cached = %v, want survivors %v", maxRows, query, err == nil, want)
			}
		}
	}
	prune(10, "hot", "stale", "recent", "old")
	prune(4, "hot", "stale", "recent", "old")
	prune(2, "hot", "recent")
	if err := repo.TouchCategorySearchCache(ctx, "hot", now.Add(-6*time.Hour)); err != nil {
		t.Fatal(err)
	}
	prune(1, "recent")
	prune(0)
	cache("hot")
	prune(-1)
	prune(0)
}

func testCategoryColumnsRoundTrip(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	art, igdb := "https://cdn.example.com/g-rt-{width}x{height}.jpg", "igdb-rt"
	in := &repository.Category{ID: "cat-rt", Name: "Roundtrip", BoxArtURL: &art, IGDBID: &igdb}
	if _, err := repo.UpsertCategory(ctx, in); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.SearchCategories(ctx, "Roundtrip", 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("search = %+v, %v", rows, err)
	}
	got := rows[0]
	if got.ID != in.ID || got.Name != in.Name || derefString(got.BoxArtURL) != art || derefString(got.IGDBID) != igdb || got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("category = %+v", got)
	}
}
