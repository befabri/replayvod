package contracttest

import (
	"context"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testCreateVideoNormalizesRecordingSettings(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-audio", BroadcasterLogin: "audio", BroadcasterName: "Audio",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	got, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID:         "job-audio",
		Filename:      "audio",
		DisplayName:   "Audio",
		Status:        repository.VideoStatusPending,
		BroadcasterID: "bc-audio",
		Language:      "en",
		RecordingType: repository.RecordingTypeAudio,
		ForceH264:     true,
	})
	if err != nil {
		t.Fatalf("CreateVideo: %v", err)
	}
	if got.RecordingType != repository.RecordingTypeAudio {
		t.Fatalf("recording_type = %q, want audio", got.RecordingType)
	}
	if got.Quality != repository.QualityHigh {
		t.Fatalf("quality = %q, want HIGH default", got.Quality)
	}
	if got.ForceH264 {
		t.Fatalf("force_h264 = true for audio row, want false")
	}
}

func testListVideosByJobIDs(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-1", BroadcasterLogin: "bc", BroadcasterName: "BC",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	for _, jobID := range []string{"job-a", "job-b", "job-c"} {
		if _, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID:         jobID,
			Filename:      jobID,
			DisplayName:   jobID,
			Status:        repository.VideoStatusDone,
			Quality:       repository.QualityHigh,
			BroadcasterID: "bc-1",
			Language:      "en",
			RecordingType: repository.RecordingTypeVideo,
		}); err != nil {
			t.Fatalf("seed %s: %v", jobID, err)
		}
	}

	t.Run("matched + missing job ids", func(t *testing.T) {
		got, err := repo.ListVideosByJobIDs(ctx, []string{"job-a", "job-c", "job-missing"})
		if err != nil {
			t.Fatalf("by job ids: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2 matched rows, got %d", len(got))
		}
		gotJobs := map[string]bool{}
		for _, v := range got {
			gotJobs[v.JobID] = true
		}
		if !gotJobs["job-a"] || !gotJobs["job-c"] {
			t.Errorf("expected job-a and job-c, got %v", gotJobs)
		}
	})

	t.Run("nil job ids returns empty, no error", func(t *testing.T) {
		empty, err := repo.ListVideosByJobIDs(ctx, nil)
		if err != nil {
			t.Fatalf("by nil ids: %v", err)
		}
		if len(empty) != 0 {
			t.Errorf("nil ids should return 0 rows, got %d", len(empty))
		}
	})

	t.Run("duplicate job ids collapse", func(t *testing.T) {
		got, err := repo.ListVideosByJobIDs(ctx, []string{"job-a", "job-a", "job-b"})
		if err != nil {
			t.Fatalf("by dup ids: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("duplicates should collapse, got %d rows", len(got))
		}
	})
}

func testListVideosPageCursorPagination(t *testing.T, h Harness) {
	ctx := context.Background()
	seedVideoListPageFixture(t, ctx, h)
	repo := h.Repo()
	durationMin := 250.0
	sizeMin := int64(2500)

	cases := []struct {
		name string
		opts repository.ListVideosOpts
		want []string
	}{
		{"created desc", repository.ListVideosOpts{Sort: "created_at", Order: "desc", Limit: 2}, []string{"job-d2", "job-d1", "job-c", "job-b", "job-a"}},
		{"created asc", repository.ListVideosOpts{Sort: "created_at", Order: "asc", Limit: 2}, []string{"job-a", "job-b", "job-c", "job-d1", "job-d2"}},
		{"channel asc", repository.ListVideosOpts{Sort: "channel", Order: "asc", Limit: 2}, []string{"job-a", "job-b", "job-c", "job-d1", "job-d2"}},
		{"channel desc", repository.ListVideosOpts{Sort: "channel", Order: "desc", Limit: 2}, []string{"job-d2", "job-d1", "job-c", "job-b", "job-a"}},
		{"duration desc", repository.ListVideosOpts{Sort: "duration", Order: "desc", Limit: 2}, []string{"job-b", "job-c", "job-d2", "job-d1", "job-a"}},
		{"duration asc", repository.ListVideosOpts{Sort: "duration", Order: "asc", Limit: 2}, []string{"job-a", "job-d1", "job-d2", "job-c", "job-b"}},
		{"size desc", repository.ListVideosOpts{Sort: "size", Order: "desc", Limit: 2}, []string{"job-b", "job-c", "job-d2", "job-d1", "job-a"}},
		{"size asc", repository.ListVideosOpts{Sort: "size", Order: "asc", Limit: 2}, []string{"job-a", "job-d1", "job-d2", "job-c", "job-b"}},
		{"duration min filter", repository.ListVideosOpts{Sort: "created_at", Order: "desc", Limit: 2, DurationMinSeconds: &durationMin}, []string{"job-c", "job-b"}},
		{"size min filter", repository.ListVideosOpts{Sort: "created_at", Order: "desc", Limit: 2, SizeMinBytes: &sizeMin}, []string{"job-d2", "job-c", "job-b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collectVideoListPageJobIDs(t, ctx, repo, tc.opts)
			assertStringSlice(t, got, tc.want)
		})
	}
}

func testListVideosPageFiltersAndNullCursor(t *testing.T, h Harness) {
	ctx := context.Background()
	seedVideoListFilterFixture(t, ctx, h)
	repo := h.Repo()
	qualityHigh := repository.QualityHigh

	cases := []struct {
		name string
		opts repository.ListVideosOpts
		want []string
	}{
		{
			"quality filter",
			repository.ListVideosOpts{Sort: "created_at", Order: "desc", Limit: 2, Quality: qualityHigh},
			[]string{"job-f-failed-b", "job-f-failed-a", "job-f-high-b", "job-f-high-a"},
		},
		{
			"broadcaster filter",
			repository.ListVideosOpts{Sort: "created_at", Order: "desc", Limit: 2, BroadcasterID: "bc-filter-a"},
			[]string{"job-f-failed-a", "job-f-low-a", "job-f-high-a"},
		},
		{
			"language filter",
			repository.ListVideosOpts{Sort: "created_at", Order: "desc", Limit: 2, Language: "fr"},
			[]string{"job-f-failed-b", "job-f-low-b", "job-f-high-b"},
		},
		{
			"duration desc crosses into NULL",
			repository.ListVideosOpts{Sort: "duration", Order: "desc", Limit: 2},
			[]string{"job-f-low-b", "job-f-high-b", "job-f-low-a", "job-f-high-a", "job-f-failed-b", "job-f-failed-a"},
		},
		{
			"size desc crosses into NULL",
			repository.ListVideosOpts{Sort: "size", Order: "desc", Limit: 2},
			[]string{"job-f-low-b", "job-f-high-b", "job-f-low-a", "job-f-high-a", "job-f-failed-b", "job-f-failed-a"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collectVideoListPageJobIDs(t, ctx, repo, tc.opts)
			assertStringSlice(t, got, tc.want)
		})
	}
}

func testListVideosByBroadcasterAndCategoryPage(t *testing.T, h Harness) {
	ctx := context.Background()
	seedVideoPageFixture(t, ctx, h)
	repo := h.Repo()
	limit := 2

	cases := []struct {
		name  string
		fetch func(*repository.VideoPageCursor) (*repository.VideoPage, error)
		want  []string
	}{
		{
			name: "broadcaster filters, pages, and skips deleted",
			fetch: func(cursor *repository.VideoPageCursor) (*repository.VideoPage, error) {
				return repo.ListVideosByBroadcaster(ctx, "bc-video-page-target", limit, cursor)
			},
			want: []string{"job-other-category", "job-new", "job-tie-high", "job-tie-low", "job-old"},
		},
		{
			name: "category filters, pages, and skips deleted",
			fetch: func(cursor *repository.VideoPageCursor) (*repository.VideoPage, error) {
				return repo.ListVideosByCategory(ctx, "cat-video-page-target", limit, cursor)
			},
			want: []string{"job-other-broadcaster", "job-new", "job-tie-high", "job-tie-low", "job-old"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collectVideoPageJobIDs(t, limit, tc.fetch)
			assertStringSlice(t, got, tc.want)
		})
	}
}

func testVideoMetadataDurationsTracksHistoryAndPrimaryCategory(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-meta", BroadcasterLogin: "meta", BroadcasterName: "Meta",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	for _, c := range []repository.Category{{ID: "cat-a", Name: "Alpha"}, {ID: "cat-b", Name: "Bravo"}} {
		cat := c
		if _, err := repo.UpsertCategory(ctx, &cat); err != nil {
			t.Fatalf("seed category %s: %v", c.ID, err)
		}
	}
	video, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID:         "job-meta-1",
		Filename:      "meta-video",
		DisplayName:   "Meta",
		Title:         "Opening",
		Status:        repository.VideoStatusPending,
		Quality:       repository.QualityHigh,
		BroadcasterID: "bc-meta",
		Language:      "en",
	})
	if err != nil {
		t.Fatalf("create video: %v", err)
	}
	titleA, err := repo.UpsertTitle(ctx, "Opening")
	if err != nil {
		t.Fatalf("title A: %v", err)
	}
	titleB, err := repo.UpsertTitle(ctx, "Main Run")
	if err != nil {
		t.Fatalf("title B: %v", err)
	}
	if err := repo.LinkVideoTitle(ctx, video.ID, titleA.ID); err != nil {
		t.Fatalf("link title A: %v", err)
	}
	if err := repo.LinkVideoTitle(ctx, video.ID, titleB.ID); err != nil {
		t.Fatalf("link title B: %v", err)
	}
	if err := repo.LinkVideoCategory(ctx, video.ID, "cat-a"); err != nil {
		t.Fatalf("link category A: %v", err)
	}
	if err := repo.LinkVideoCategory(ctx, video.ID, "cat-b"); err != nil {
		t.Fatalf("link category B: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	at1 := now.Add(-5 * time.Minute)
	at2 := now.Add(-3 * time.Minute)
	at3 := now.Add(-1 * time.Minute)
	resumeAt := now.Add(30 * time.Second)
	endAt := resumeAt.Add(30 * time.Second)

	if err := repo.UpsertVideoTitleSpan(ctx, video.ID, titleA.ID, at1); err != nil {
		t.Fatalf("span title A1: %v", err)
	}
	if err := repo.UpsertVideoCategorySpan(ctx, video.ID, "cat-a", at1); err != nil {
		t.Fatalf("span category A1: %v", err)
	}
	if err := repo.UpsertVideoTitleSpan(ctx, video.ID, titleB.ID, at2); err != nil {
		t.Fatalf("span title B: %v", err)
	}
	if err := repo.UpsertVideoCategorySpan(ctx, video.ID, "cat-b", at2); err != nil {
		t.Fatalf("span category B: %v", err)
	}
	if err := repo.UpsertVideoTitleSpan(ctx, video.ID, titleA.ID, at3); err != nil {
		t.Fatalf("span title A2: %v", err)
	}
	if err := repo.UpsertVideoCategorySpan(ctx, video.ID, "cat-a", at3); err != nil {
		t.Fatalf("span category A2: %v", err)
	}
	if err := repo.CloseOpenVideoMetadataSpans(ctx, video.ID, now); err != nil {
		t.Fatalf("close spans at now: %v", err)
	}
	if err := repo.ResumeVideoMetadataSpans(ctx, video.ID, resumeAt); err != nil {
		t.Fatalf("resume spans: %v", err)
	}
	if err := repo.CloseOpenVideoMetadataSpans(ctx, video.ID, endAt); err != nil {
		t.Fatalf("close resumed spans: %v", err)
	}

	titles, err := repo.ListTitlesForVideo(ctx, video.ID)
	if err != nil {
		t.Fatalf("list titles: %v", err)
	}
	if len(titles) != 4 {
		t.Fatalf("want 4 title spans, got %d", len(titles))
	}
	if titles[0].Name != "Opening" || titles[1].Name != "Main Run" || titles[2].Name != "Opening" || titles[3].Name != "Opening" {
		t.Fatalf("unexpected title span order: %+v", titles)
	}
	if titles[0].DurationSeconds < 119 || titles[1].DurationSeconds < 119 || titles[2].DurationSeconds < 59 || titles[3].DurationSeconds < 29 {
		t.Fatalf("unexpected title durations: %+v", titles)
	}

	cats, err := repo.ListCategoriesForVideo(ctx, video.ID)
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if len(cats) != 4 {
		t.Fatalf("want 4 category spans, got %d", len(cats))
	}
	if cats[0].Name != "Alpha" || cats[1].Name != "Bravo" || cats[2].Name != "Alpha" || cats[3].Name != "Alpha" {
		t.Fatalf("unexpected category span order: %+v", cats)
	}
	if cats[0].DurationSeconds < 119 || cats[1].DurationSeconds < 119 || cats[2].DurationSeconds < 59 || cats[3].DurationSeconds < 29 {
		t.Fatalf("unexpected category durations: %+v", cats)
	}
	primary, err := repo.ListPrimaryCategoriesForVideos(ctx, []int64{video.ID})
	if err != nil {
		t.Fatalf("primary categories: %v", err)
	}
	if got, ok := primary[video.ID]; !ok || got.ID != "cat-a" {
		t.Fatalf("primary category = %+v, want cat-a", got)
	}
}

// --- fixtures + collectors ---

func seedVideoPageFixture(t *testing.T, ctx context.Context, h Harness) {
	t.Helper()
	repo := h.Repo()
	for _, ch := range []struct{ id, login, name string }{
		{"bc-video-page-target", "video-page-target", "Video Page Target"},
		{"bc-video-page-other", "video-page-other", "Video Page Other"},
	} {
		if _, err := repo.UpsertChannel(ctx, &repository.Channel{
			BroadcasterID: ch.id, BroadcasterLogin: ch.login, BroadcasterName: ch.name,
		}); err != nil {
			t.Fatalf("seed channel %s: %v", ch.id, err)
		}
	}
	for _, cat := range []repository.Category{
		{ID: "cat-video-page-target", Name: "Video Page Target"},
		{ID: "cat-video-page-other", Name: "Video Page Other"},
	} {
		category := cat
		if _, err := repo.UpsertCategory(ctx, &category); err != nil {
			t.Fatalf("seed category %s: %v", cat.ID, err)
		}
	}

	base := time.Date(2026, 4, 23, 16, 0, 0, 0, time.UTC)
	seeds := []struct {
		jobID         string
		broadcasterID string
		categoryID    string
		startedAt     time.Time
		deleted       bool
	}{
		{"job-old", "bc-video-page-target", "cat-video-page-target", base.Add(1 * time.Minute), false},
		{"job-tie-low", "bc-video-page-target", "cat-video-page-target", base.Add(2 * time.Minute), false},
		{"job-tie-high", "bc-video-page-target", "cat-video-page-target", base.Add(2 * time.Minute), false},
		{"job-new", "bc-video-page-target", "cat-video-page-target", base.Add(3 * time.Minute), false},
		{"job-other-broadcaster", "bc-video-page-other", "cat-video-page-target", base.Add(4 * time.Minute), false},
		{"job-other-category", "bc-video-page-target", "cat-video-page-other", base.Add(5 * time.Minute), false},
		{"job-deleted", "bc-video-page-target", "cat-video-page-target", base.Add(6 * time.Minute), true},
	}
	for _, s := range seeds {
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID:         s.jobID,
			Filename:      s.jobID,
			DisplayName:   s.jobID,
			Title:         s.jobID,
			Status:        repository.VideoStatusDone,
			Quality:       repository.QualityHigh,
			BroadcasterID: s.broadcasterID,
			Language:      "en",
			RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("create %s: %v", s.jobID, err)
		}
		if err := repo.LinkVideoCategory(ctx, v.ID, s.categoryID); err != nil {
			t.Fatalf("link category %s: %v", s.jobID, err)
		}
		h.BackdateVideoStartDownload(t, v.ID, s.startedAt)
		if s.deleted {
			if err := repo.SoftDeleteVideo(ctx, v.ID, repository.DeletionKindManual); err != nil {
				t.Fatalf("soft delete %s: %v", s.jobID, err)
			}
		}
	}
}

func seedVideoListPageFixture(t *testing.T, ctx context.Context, h Harness) {
	t.Helper()
	repo := h.Repo()
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-page", BroadcasterLogin: "page", BroadcasterName: "Page",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	base := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	seeds := []struct {
		jobID       string
		displayName string
		duration    float64
		size        int64
		startedAt   time.Time
	}{
		{"job-a", "Alpha", 100, 1000, base.Add(1 * time.Minute)},
		{"job-b", "Bravo", 400, 4000, base.Add(2 * time.Minute)},
		{"job-c", "Charlie", 300, 3000, base.Add(3 * time.Minute)},
		{"job-d1", "Delta", 200, 2000, base.Add(4 * time.Minute)},
		{"job-d2", "Delta", 200, 2500, base.Add(4 * time.Minute)},
	}
	for _, s := range seeds {
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID:         s.jobID,
			Filename:      s.jobID,
			DisplayName:   s.displayName,
			Status:        repository.VideoStatusDone,
			Quality:       repository.QualityHigh,
			BroadcasterID: "bc-page",
			Language:      "en",
			RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("create %s: %v", s.jobID, err)
		}
		if err := repo.MarkVideoDone(ctx, v.ID, s.duration, s.size, nil, repository.CompletionKindComplete, false); err != nil {
			t.Fatalf("mark done %s: %v", s.jobID, err)
		}
		h.BackdateVideoStartDownload(t, v.ID, s.startedAt)
	}
}

func seedVideoListFilterFixture(t *testing.T, ctx context.Context, h Harness) {
	t.Helper()
	repo := h.Repo()
	for _, ch := range []struct{ id, login, name string }{
		{"bc-filter-a", "filter-a", "Filter A"},
		{"bc-filter-b", "filter-b", "Filter B"},
	} {
		if _, err := repo.UpsertChannel(ctx, &repository.Channel{
			BroadcasterID: ch.id, BroadcasterLogin: ch.login, BroadcasterName: ch.name,
		}); err != nil {
			t.Fatalf("seed channel %s: %v", ch.id, err)
		}
	}

	base := time.Date(2026, 4, 23, 14, 0, 0, 0, time.UTC)
	type seed struct {
		jobID         string
		broadcasterID string
		quality       string
		language      string
		duration      float64
		size          int64
		minute        int
		failed        bool
	}
	seeds := []seed{
		{"job-f-high-a", "bc-filter-a", repository.QualityHigh, "en", 100, 1000, 1, false},
		{"job-f-high-b", "bc-filter-b", repository.QualityHigh, "fr", 400, 4000, 2, false},
		{"job-f-low-a", "bc-filter-a", repository.QualityLow, "en", 200, 2000, 3, false},
		{"job-f-low-b", "bc-filter-b", repository.QualityLow, "fr", 500, 5000, 4, false},
		{"job-f-failed-a", "bc-filter-a", repository.QualityHigh, "en", 0, 0, 5, true},
		{"job-f-failed-b", "bc-filter-b", repository.QualityHigh, "fr", 0, 0, 6, true},
	}
	for _, s := range seeds {
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID:         s.jobID,
			Filename:      s.jobID,
			DisplayName:   s.jobID,
			Status:        repository.VideoStatusDone,
			Quality:       s.quality,
			BroadcasterID: s.broadcasterID,
			Language:      s.language,
			RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("create %s: %v", s.jobID, err)
		}
		if s.failed {
			if err := repo.MarkVideoFailed(ctx, v.ID, "seed-failed", repository.CompletionKindComplete, true); err != nil {
				t.Fatalf("mark failed %s: %v", s.jobID, err)
			}
		} else {
			if err := repo.MarkVideoDone(ctx, v.ID, s.duration, s.size, nil, repository.CompletionKindComplete, false); err != nil {
				t.Fatalf("mark done %s: %v", s.jobID, err)
			}
		}
		h.BackdateVideoStartDownload(t, v.ID, base.Add(time.Duration(s.minute)*time.Minute))
	}
}

func collectVideoPageJobIDs(t *testing.T, limit int, fetch func(*repository.VideoPageCursor) (*repository.VideoPage, error)) []string {
	t.Helper()
	var cursor *repository.VideoPageCursor
	out := []string{}
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("video page pagination did not terminate")
		}
		page, err := fetch(cursor)
		if err != nil {
			t.Fatalf("fetch video page: %v", err)
		}
		if len(page.Items) > limit {
			t.Fatalf("page size: got %d, limit %d", len(page.Items), limit)
		}
		for _, item := range page.Items {
			out = append(out, item.JobID)
		}
		if page.NextCursor == nil {
			return out
		}
		if len(page.Items) == 0 {
			t.Fatal("empty video page returned a next cursor")
		}
		cursor = page.NextCursor
	}
}

func collectVideoListPageJobIDs(t *testing.T, ctx context.Context, repo repository.Repository, opts repository.ListVideosOpts) []string {
	t.Helper()
	var cursor *repository.VideoListPageCursor
	out := []string{}
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("video pagination did not terminate")
		}
		page, err := repo.ListVideosPage(ctx, opts, cursor)
		if err != nil {
			t.Fatalf("ListVideosPage: %v", err)
		}
		if len(page.Items) > opts.Limit {
			t.Fatalf("page size: got %d, limit %d", len(page.Items), opts.Limit)
		}
		for _, item := range page.Items {
			out = append(out, item.JobID)
		}
		if page.NextCursor == nil {
			return out
		}
		if len(page.Items) == 0 {
			t.Fatal("empty video page returned a next cursor")
		}
		cursor = page.NextCursor
	}
}
