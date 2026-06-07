package contracttest

import (
	"context"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testCategoryDescriptionMethods(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	igdb := "123"
	existing := "Existing"
	for _, category := range []repository.Category{
		{ID: "needs-description", Name: "A", IGDBID: &igdb},
		{ID: "no-igdb", Name: "B"},
		{ID: "has-description", Name: "C", IGDBID: &igdb, Description: &existing},
		{ID: "recently-checked", Name: "D", IGDBID: &igdb},
	} {
		c := category
		if _, err := repo.UpsertCategory(ctx, &c); err != nil {
			t.Fatalf("seed %s: %v", c.ID, err)
		}
	}
	if err := repo.MarkCategoryDescriptionChecked(ctx, "recently-checked"); err != nil {
		t.Fatalf("MarkCategoryDescriptionChecked: %v", err)
	}

	missing, err := repo.ListCategoriesMissingDescription(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListCategoriesMissingDescription: %v", err)
	}
	if len(missing) != 1 || missing[0].ID != "needs-description" {
		t.Fatalf("missing description = %+v, want only needs-description", missing)
	}
	retry, err := repo.ListCategoriesMissingDescription(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("ListCategoriesMissingDescription retry: %v", err)
	}
	if len(retry) != 2 || retry[0].ID != "needs-description" || retry[1].ID != "recently-checked" {
		t.Fatalf("retryable missing description = %+v, want needs-description and recently-checked", retry)
	}
	if err := repo.UpdateCategoryDescription(ctx, "needs-description", "New description"); err != nil {
		t.Fatalf("UpdateCategoryDescription: %v", err)
	}
	got, err := repo.GetCategory(ctx, "needs-description")
	if err != nil {
		t.Fatalf("GetCategory: %v", err)
	}
	if got.Description == nil || *got.Description != "New description" {
		t.Fatalf("description = %v, want New description", got.Description)
	}
	if got.DescriptionCheckedAt == nil {
		t.Fatal("description_checked_at must be set when description is updated")
	}
}

func testGetCategoryDetail(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	description := "Local detail metadata."
	if _, err := repo.UpsertCategory(ctx, &repository.Category{
		ID:          "cat-detail",
		Name:        "Detail Game",
		Description: &description,
	}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-category-detail", BroadcasterLogin: "categorydetail", BroadcasterName: "Category Detail",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	seeds := []struct {
		jobID   string
		status  string
		size    int64
		deleted bool
	}{
		{jobID: "category-detail-sized", status: repository.VideoStatusDone, size: 2_048},
		{jobID: "category-detail-running", status: repository.VideoStatusRunning},
		{jobID: "category-detail-deleted", status: repository.VideoStatusDone, size: 8_192, deleted: true},
	}
	for _, seed := range seeds {
		video, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID:         seed.jobID,
			Filename:      seed.jobID,
			DisplayName:   "Category Detail",
			Status:        seed.status,
			Quality:       repository.QualityHigh,
			BroadcasterID: "bc-category-detail",
			Language:      "en",
			RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("create video %s: %v", seed.jobID, err)
		}
		if err := repo.LinkVideoCategory(ctx, video.ID, "cat-detail"); err != nil {
			t.Fatalf("link category %s: %v", seed.jobID, err)
		}
		if seed.size > 0 {
			if err := repo.MarkVideoDone(ctx, video.ID, 60, seed.size, nil, repository.CompletionKindComplete, false); err != nil {
				t.Fatalf("mark done %s: %v", seed.jobID, err)
			}
		}
		if seed.deleted {
			if err := repo.SoftDeleteVideo(ctx, video.ID, repository.DeletionKindManual); err != nil {
				t.Fatalf("soft delete %s: %v", seed.jobID, err)
			}
		}
	}

	got, err := repo.GetCategoryDetail(ctx, "cat-detail")
	if err != nil {
		t.Fatalf("GetCategoryDetail: %v", err)
	}
	if got.Category.Description == nil || *got.Category.Description != description {
		t.Fatalf("description = %v, want %q", got.Category.Description, description)
	}
	if got.VideoCount != 2 {
		t.Fatalf("VideoCount = %d, want 2", got.VideoCount)
	}
	if got.TotalSize != 2_048 {
		t.Fatalf("TotalSize = %d, want 2048", got.TotalSize)
	}
}

func testListCategoriesByIDsReturnsInputOrder(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	for _, c := range []repository.Category{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B"},
		{ID: "c", Name: "C"},
	} {
		cat := c
		if _, err := repo.UpsertCategory(ctx, &cat); err != nil {
			t.Fatalf("seed %s: %v", c.ID, err)
		}
	}

	got, err := repo.ListCategoriesByIDs(ctx, []string{"c", "missing", "a", "c", "b"})
	if err != nil {
		t.Fatalf("ListCategoriesByIDs: %v", err)
	}
	want := []string{"c", "a", "b"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("row %d ID = %q, want %q: %+v", i, got[i].ID, id, got)
		}
	}
}

func testListCategoriesMissingGameMetadata(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	art := "art"
	igdb := "123"
	for _, category := range []repository.Category{
		{ID: "missing-both", Name: "A"},
		{ID: "missing-igdb", Name: "B", BoxArtURL: &art},
		{ID: "complete", Name: "C", BoxArtURL: &art, IGDBID: &igdb},
		{ID: "missing-art", Name: "D", IGDBID: &igdb},
		{ID: "recently-checked", Name: "E", BoxArtURL: &art},
	} {
		c := category
		if _, err := repo.UpsertCategory(ctx, &c); err != nil {
			t.Fatalf("seed %s: %v", c.ID, err)
		}
	}
	if err := repo.MarkCategoryGameMetadataChecked(ctx, "recently-checked"); err != nil {
		t.Fatalf("MarkCategoryGameMetadataChecked: %v", err)
	}

	got, err := repo.ListCategoriesMissingGameMetadata(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListCategoriesMissingGameMetadata: %v", err)
	}
	want := []string{"missing-both", "missing-igdb", "missing-art"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("row %d ID = %q, want %q: %+v", i, got[i].ID, id, got)
		}
	}
	retry, err := repo.ListCategoriesMissingGameMetadata(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("ListCategoriesMissingGameMetadata retry: %v", err)
	}
	wantRetry := []string{"missing-both", "missing-igdb", "missing-art", "recently-checked"}
	if len(retry) != len(wantRetry) {
		t.Fatalf("retry len = %d, want %d: %+v", len(retry), len(wantRetry), retry)
	}
	for i, id := range wantRetry {
		if retry[i].ID != id {
			t.Fatalf("retry row %d ID = %q, want %q: %+v", i, retry[i].ID, id, retry)
		}
	}
}

func testUpdateCategoryGameMetadata(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	existingArt := "https://cdn.example.com/existing.jpg"
	if _, err := repo.UpsertCategory(ctx, &repository.Category{
		ID:        "g-meta",
		Name:      "Meta",
		BoxArtURL: &existingArt,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.UpdateCategoryGameMetadata(ctx, "g-meta", "", "9876"); err != nil {
		t.Fatalf("update igdb only: %v", err)
	}
	got, err := repo.GetCategory(ctx, "g-meta")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.BoxArtURL == nil || *got.BoxArtURL != existingArt {
		t.Fatalf("box_art_url = %v, want preserved %q", got.BoxArtURL, existingArt)
	}
	if got.IGDBID == nil || *got.IGDBID != "9876" {
		t.Fatalf("igdb_id = %v, want 9876", got.IGDBID)
	}
	if got.GameMetadataCheckedAt == nil {
		t.Fatal("game_metadata_checked_at must be set after game metadata update")
	}

	newArt := "https://cdn.example.com/new.jpg"
	if err := repo.UpdateCategoryGameMetadata(ctx, "g-meta", newArt, ""); err != nil {
		t.Fatalf("update art only: %v", err)
	}
	got, err = repo.GetCategory(ctx, "g-meta")
	if err != nil {
		t.Fatalf("get updated: %v", err)
	}
	if got.BoxArtURL == nil || *got.BoxArtURL != newArt {
		t.Fatalf("box_art_url = %v, want %q", got.BoxArtURL, newArt)
	}
	if got.IGDBID == nil || *got.IGDBID != "9876" {
		t.Fatalf("igdb_id = %v, want preserved 9876", got.IGDBID)
	}
}

func testUpsertCategoriesPreservesBoxArtAndReturnsInputOrder(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	art := "https://cdn.example.com/art-{width}x{height}.jpg"
	igdb := "igdb-42"
	if _, err := repo.UpsertCategory(ctx, &repository.Category{
		ID:        "g-1",
		Name:      "Old Name",
		BoxArtURL: &art,
		IGDBID:    &igdb,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := repo.UpsertCategories(ctx, []repository.Category{
		{ID: "g-2", Name: "Second"},
		{ID: "g-1", Name: "New Name"},
		{ID: "g-2", Name: "Second Duplicate"},
	})
	if err != nil {
		t.Fatalf("batch upsert: %v", err)
	}
	if len(got) != 2 || got[0].ID != "g-2" || got[1].ID != "g-1" {
		t.Fatalf("batch order = %+v, want g-2 then g-1", got)
	}

	updated, err := repo.GetCategory(ctx, "g-1")
	if err != nil {
		t.Fatalf("get updated: %v", err)
	}
	if updated.Name != "New Name" {
		t.Fatalf("name = %q, want New Name", updated.Name)
	}
	if updated.BoxArtURL == nil || *updated.BoxArtURL != art {
		t.Fatalf("box_art_url = %v, want %q", updated.BoxArtURL, art)
	}
	if updated.IGDBID == nil || *updated.IGDBID != igdb {
		t.Fatalf("igdb_id = %v, want %q", updated.IGDBID, igdb)
	}
}

func testUpsertCategoryPreservesBoxArt(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	art := "https://cdn.example.com/art-{width}x{height}.jpg"
	igdb := "igdb-42"
	if _, err := repo.UpsertCategory(ctx, &repository.Category{
		ID:        "g-1",
		Name:      "Old Name",
		BoxArtURL: &art,
		IGDBID:    &igdb,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := repo.UpsertCategory(ctx, &repository.Category{
		ID: "g-1", Name: "New Name",
	}); err != nil {
		t.Fatalf("webhook-path upsert: %v", err)
	}

	got, err := repo.GetCategory(ctx, "g-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "New Name" {
		t.Errorf("name should update: got %q", got.Name)
	}
	if got.BoxArtURL == nil || *got.BoxArtURL != art {
		t.Errorf("box_art_url was wiped: got %v, want %q", got.BoxArtURL, art)
	}
	if got.IGDBID == nil || *got.IGDBID != igdb {
		t.Errorf("igdb_id was wiped: got %v, want %q", got.IGDBID, igdb)
	}
}

func testListCategoriesWithVideos(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	for _, c := range []repository.Category{
		{ID: "cat-visible", Name: "Visible Game"},
		{ID: "cat-searched", Name: "Searched Only"},
		{ID: "cat-deleted", Name: "Deleted Only"},
	} {
		cat := c
		if _, err := repo.UpsertCategory(ctx, &cat); err != nil {
			t.Fatalf("seed category %s: %v", cat.ID, err)
		}
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-cats", BroadcasterLogin: "cats", BroadcasterName: "Cats",
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

	got, err := repo.ListCategoriesWithVideos(ctx)
	if err != nil {
		t.Fatalf("list categories with videos: %v", err)
	}
	if len(got) != 1 || got[0].ID != "cat-visible" {
		t.Fatalf("ListCategoriesWithVideos = %+v, want only cat-visible", got)
	}

	got, err = repo.SearchCategoriesWithVideos(ctx, "Visible", 10)
	if err != nil {
		t.Fatalf("search visible categories with videos: %v", err)
	}
	if len(got) != 1 || got[0].ID != "cat-visible" {
		t.Fatalf("SearchCategoriesWithVideos visible = %+v, want only cat-visible", got)
	}
	got, err = repo.SearchCategoriesWithVideos(ctx, "Searched", 10)
	if err != nil {
		t.Fatalf("search catalog-only categories with videos: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("SearchCategoriesWithVideos catalog-only = %+v, want none", got)
	}
	got, err = repo.SearchCategoriesWithVideos(ctx, "Deleted", 10)
	if err != nil {
		t.Fatalf("search deleted categories with videos: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("SearchCategoriesWithVideos deleted-only = %+v, want none", got)
	}
}

func testListCategoriesWithVideosPageSortAndCursor(t *testing.T, h Harness) {
	ctx := context.Background()
	seedCategoryPageFixture(t, ctx, h)
	repo := h.Repo()

	cases := []struct {
		name string
		sort string
		want []string
	}{
		{"default", "name_asc", []string{"Alpha Game", "Bravo Game", "Charlie Game"}},
		{"latest video", "latest_video_desc", []string{"Charlie Game", "Bravo Game", "Alpha Game"}},
		{"video count", "video_count_desc", []string{"Bravo Game", "Charlie Game", "Alpha Game"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collectCategoryPageNames(t, ctx, repo, 1, tc.sort)
			assertStringSlice(t, got, tc.want)
		})
	}
}

// seedCategoryPageFixture seeds three visible categories (with 1/3/2 videos at
// increasing start times) plus one deleted-only category, so the page sort tests
// can assert name, latest-video, and video-count orderings.
func seedCategoryPageFixture(t *testing.T, ctx context.Context, h Harness) {
	t.Helper()
	repo := h.Repo()
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-category-page", BroadcasterLogin: "categorypage", BroadcasterName: "Category Page",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	for _, c := range []repository.Category{
		{ID: "cat-alpha-page", Name: "Alpha Game"},
		{ID: "cat-bravo-page", Name: "Bravo Game"},
		{ID: "cat-charlie-page", Name: "Charlie Game"},
		{ID: "cat-deleted-page", Name: "Deleted Game"},
	} {
		cat := c
		if _, err := repo.UpsertCategory(ctx, &cat); err != nil {
			t.Fatalf("seed category %s: %v", cat.ID, err)
		}
	}

	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	seeds := []struct {
		jobID      string
		categoryID string
		startedAt  time.Time
		deleted    bool
	}{
		{"cat-page-alpha-1", "cat-alpha-page", base.Add(1 * time.Minute), false},
		{"cat-page-bravo-1", "cat-bravo-page", base.Add(2 * time.Minute), false},
		{"cat-page-bravo-2", "cat-bravo-page", base.Add(3 * time.Minute), false},
		{"cat-page-bravo-3", "cat-bravo-page", base.Add(4 * time.Minute), false},
		{"cat-page-charlie-1", "cat-charlie-page", base.Add(5 * time.Minute), false},
		{"cat-page-charlie-2", "cat-charlie-page", base.Add(6 * time.Minute), false},
		{"cat-page-deleted-1", "cat-deleted-page", base.Add(7 * time.Minute), true},
	}
	for _, seed := range seeds {
		video, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID:         seed.jobID,
			Filename:      seed.jobID,
			DisplayName:   "Category Page",
			Title:         seed.jobID,
			Status:        repository.VideoStatusDone,
			Quality:       repository.QualityHigh,
			BroadcasterID: "bc-category-page",
			Language:      "en",
			RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("create video %s: %v", seed.jobID, err)
		}
		if err := repo.LinkVideoCategory(ctx, video.ID, seed.categoryID); err != nil {
			t.Fatalf("link category %s: %v", seed.jobID, err)
		}
		h.BackdateVideoStartDownload(t, video.ID, seed.startedAt)
		if seed.deleted {
			if err := repo.SoftDeleteVideo(ctx, video.ID, repository.DeletionKindManual); err != nil {
				t.Fatalf("soft delete %s: %v", seed.jobID, err)
			}
		}
	}
}

func collectCategoryPageNames(t *testing.T, ctx context.Context, repo repository.Repository, limit int, sort string) []string {
	t.Helper()
	var cursor *repository.CategoryPageCursor
	out := []string{}
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("category pagination did not terminate")
		}
		page, err := repo.ListCategoriesWithVideosPage(ctx, limit, sort, cursor)
		if err != nil {
			t.Fatalf("ListCategoriesWithVideosPage: %v", err)
		}
		if len(page.Items) > limit {
			t.Fatalf("page size: got %d, limit %d", len(page.Items), limit)
		}
		for _, item := range page.Items {
			out = append(out, item.Name)
		}
		if page.NextCursor == nil {
			return out
		}
		if len(page.Items) == 0 {
			t.Fatal("empty category page returned a next cursor")
		}
		cursor = page.NextCursor
	}
}
