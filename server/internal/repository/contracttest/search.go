package contracttest

import (
	"slices"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func loginsOf(channels []repository.Channel) []string {
	out := make([]string, len(channels))
	for i, c := range channels {
		out[i] = c.BroadcasterLogin
	}
	return out
}

func categoryNamesOf(categories []repository.Category) []string {
	out := make([]string, len(categories))
	for i, c := range categories {
		out[i] = c.Name
	}
	return out
}

func testSearchChannels(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	seed := []repository.Channel{
		{BroadcasterID: "1", BroadcasterLogin: "shrimpcaster", BroadcasterName: "ShrimpCaster"},
		{BroadcasterID: "2", BroadcasterLogin: "shoal", BroadcasterName: "Shoal"},
		{BroadcasterID: "3", BroadcasterLogin: "washtubcaster", BroadcasterName: "Washtub"},
		{BroadcasterID: "4", BroadcasterLogin: "unrelated", BroadcasterName: "Elsewhere"},
		{BroadcasterID: "5", BroadcasterLogin: "percent_tester", BroadcasterName: "100% tester"},
		{BroadcasterID: "6", BroadcasterLogin: "echecs_club", BroadcasterName: "Échecs Club"},
		{BroadcasterID: "7", BroadcasterLogin: "club_echecs", BroadcasterName: "Club Échecs"},
		{BroadcasterID: "8", BroadcasterLogin: "bubblecaster", BroadcasterName: "Shrimpcaster Fan"},
	}
	for _, c := range seed {
		ch := c
		if _, err := repo.UpsertChannel(ctx, &ch); err != nil {
			t.Fatalf("seed %s: %v", c.BroadcasterLogin, err)
		}
	}
	search := func(t *testing.T, query string, limit int) []string {
		t.Helper()
		got, err := repo.SearchChannels(ctx, query, limit)
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		return loginsOf(got)
	}
	t.Run("login or display name prefix beats substring, alphabetical within prefix", func(t *testing.T) {
		assertStringSlice(t, search(t, "sh", 10), []string{"bubblecaster", "shoal", "shrimpcaster", "washtubcaster"})
	})
	t.Run("exact login match ranks ahead of an earlier display name prefix", func(t *testing.T) {
		assertStringSlice(t, search(t, "shrimpcaster", 10), []string{"shrimpcaster", "bubblecaster"})
	})
	t.Run("empty query returns every channel", func(t *testing.T) {
		if got := search(t, "", 10); len(got) != len(seed) {
			t.Fatalf("want all %d seeded, got %v", len(seed), got)
		}
	})
	t.Run("limit caps result rows", func(t *testing.T) {
		if got := search(t, "", 2); len(got) != 2 {
			t.Fatalf("limit=2 should return 2 rows, got %v", got)
		}
	})
	t.Run("display name matches as well as login", func(t *testing.T) {
		assertStringSlice(t, search(t, "100", 10), []string{"percent_tester"})
	})
	t.Run("case folding covers accented display names", func(t *testing.T) {
		for _, query := range []string{"échecs", "ÉCHECS", "Échecs"} {
			assertStringSlice(t, search(t, query, 10), []string{"echecs_club", "club_echecs"})
		}
	})
}

func testSearchCategories(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	seed := []repository.Category{
		{ID: "1", Name: "Valorant"},
		{ID: "2", Name: "Valheim"},
		{ID: "3", Name: "The Legend of Valor"},
		{ID: "4", Name: "Celeste"},
		{ID: "5", Name: "50% Off"},
		{ID: "6", Name: "Échecs"},
		{ID: "7", Name: "Jeu d'échecs"},
	}
	for _, c := range seed {
		cat := c
		if _, err := repo.UpsertCategory(ctx, &cat); err != nil {
			t.Fatalf("seed %s: %v", c.Name, err)
		}
	}
	search := func(t *testing.T, query string, limit int) []string {
		t.Helper()
		got, err := repo.SearchCategories(ctx, query, limit)
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		return categoryNamesOf(got)
	}
	t.Run("prefix beats substring, alphabetical within prefix", func(t *testing.T) {
		assertStringSlice(t, search(t, "val", 10), []string{"Valheim", "Valorant", "The Legend of Valor"})
	})
	t.Run("exact name match ranks above prefix", func(t *testing.T) {
		if got := search(t, "Valorant", 10); len(got) == 0 || got[0] != "Valorant" {
			t.Fatalf("exact match should rank first, got %v", got)
		}
	})
	t.Run("empty query returns every category", func(t *testing.T) {
		if got := search(t, "", 10); len(got) != len(seed) {
			t.Fatalf("want all %d seeded, got %v", len(seed), got)
		}
	})
	t.Run("limit caps result rows", func(t *testing.T) {
		if got := search(t, "", 2); len(got) != 2 {
			t.Fatalf("limit=2 should return 2 rows, got %v", got)
		}
	})
	t.Run("accented prefix ranks before accented substring", func(t *testing.T) {
		assertStringSlice(t, search(t, "é", 10), []string{"Échecs", "Jeu d'échecs"})
	})
	t.Run("case folding covers accented names", func(t *testing.T) {
		for _, query := range []string{"échecs", "ÉCHECS", "Échecs"} {
			assertStringSlice(t, search(t, query, 10), []string{"Échecs", "Jeu d'échecs"})
		}
	})
}

func testSearchVideos(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	for _, ch := range []repository.Channel{
		{BroadcasterID: "bc-title", BroadcasterLogin: "titlecaster", BroadcasterName: "Title Caster"},
		{BroadcasterID: "bc-channel", BroadcasterLogin: "neoncaster", BroadcasterName: "Neon Caster"},
		{BroadcasterID: "bc-category", BroadcasterLogin: "categorycaster", BroadcasterName: "Category Caster"},
		{BroadcasterID: "bc-accent", BroadcasterLogin: "chesscaster", BroadcasterName: "Échecs Caster"},
	} {
		channel := ch
		if _, err := repo.UpsertChannel(ctx, &channel); err != nil {
			t.Fatalf("seed channel %s: %v", ch.BroadcasterID, err)
		}
	}
	for _, cat := range []repository.Category{
		{ID: "cat-neon", Name: "Neon Game"},
		{ID: "cat-other", Name: "Other Game"},
		{ID: "cat-accent", Name: "Échecs"},
	} {
		category := cat
		if _, err := repo.UpsertCategory(ctx, &category); err != nil {
			t.Fatalf("seed category %s: %v", cat.ID, err)
		}
	}
	base := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	videoIDs := map[string]int64{}
	for i, s := range []struct {
		jobID, title, displayName, broadcasterID, categoryID, historyTitle string
	}{
		{"job-title", "Neon Run", "Title Caster", "bc-title", "cat-other", ""},
		{"job-title-history", "Opening Soon", "Title Caster", "bc-title", "cat-other", "Neon Finale"},
		{"job-channel", "Different", "Neon Caster", "bc-channel", "cat-other", ""},
		{"job-category", "Different", "Category Caster", "bc-category", "cat-neon", ""},
		{"job-substring", "Late Neon Mix", "Title Caster", "bc-title", "cat-other", ""},
		{"job-accent-title", "Échecs en direct", "Title Caster", "bc-title", "cat-other", ""},
		{"job-accent-channel", "Partie du soir", "Échecs Caster", "bc-accent", "cat-other", ""},
		{"job-accent-category", "Partie du matin", "Title Caster", "bc-title", "cat-accent", ""},
	} {
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID: s.jobID, Filename: s.jobID, DisplayName: s.displayName, Title: s.title,
			Status: repository.VideoStatusDone, Quality: repository.QualityHigh,
			BroadcasterID: s.broadcasterID, Language: "en", RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("create %s: %v", s.jobID, err)
		}
		videoIDs[s.jobID] = v.ID
		if err := repo.LinkVideoCategory(ctx, v.ID, s.categoryID); err != nil {
			t.Fatalf("link category %s: %v", s.jobID, err)
		}
		if s.historyTitle != "" {
			title, err := repo.UpsertTitle(ctx, s.historyTitle)
			if err != nil {
				t.Fatalf("upsert history title %s: %v", s.jobID, err)
			}
			if err := repo.LinkVideoTitle(ctx, v.ID, title.ID); err != nil {
				t.Fatalf("link history title %s: %v", s.jobID, err)
			}
		}
		h.BackdateVideoStartDownload(t, v.ID, base.Add(time.Duration(i+1)*time.Minute))
	}
	search := func(t *testing.T, query string, limit int) []string {
		t.Helper()
		got, err := repo.SearchVideos(ctx, query, limit)
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		return videoJobIDs(got)
	}
	t.Run("title and title history rank before channel and category matches", func(t *testing.T) {
		got := search(t, "neon", 10)
		if len(got) != 5 {
			t.Fatalf("expected every neon row, got %v", got)
		}
		if !slices.Contains(got[:2], "job-title") || !slices.Contains(got[:2], "job-title-history") {
			t.Fatalf("title matches should rank first, got %v", got)
		}
	})
	t.Run("query matches broadcaster metadata", func(t *testing.T) {
		if got := search(t, "neoncaster", 10); len(got) == 0 || got[0] != "job-channel" {
			t.Fatalf("expected job-channel first, got %v", got)
		}
	})
	t.Run("query matches linked category", func(t *testing.T) {
		if got := search(t, "Neon Game", 10); len(got) == 0 || got[0] != "job-category" {
			t.Fatalf("expected job-category first, got %v", got)
		}
	})
	t.Run("limit caps result rows", func(t *testing.T) {
		if got := search(t, "neon", 2); len(got) != 2 {
			t.Fatalf("limit=2 should return 2 rows, got %v", got)
		}
	})
	t.Run("case folding covers accented titles, channels and categories", func(t *testing.T) {
		want := []string{"job-accent-title", "job-accent-channel", "job-accent-category"}
		for _, query := range []string{"échecs", "ÉCHECS", "Échecs"} {
			assertStringSlice(t, search(t, query, 10), want)
		}
	})
	t.Run("accented exact title outranks accented prefix", func(t *testing.T) {
		if got := search(t, "ÉCHECS EN DIRECT", 10); len(got) == 0 || got[0] != "job-accent-title" {
			t.Fatalf("expected exact title first, got %v", got)
		}
	})
	t.Run("soft-deleted videos are excluded", func(t *testing.T) {
		if err := repo.SoftDeleteVideo(ctx, videoIDs["job-title"], repository.DeletionKindManual); err != nil {
			t.Fatalf("soft delete: %v", err)
		}
		if got := search(t, "Neon Run", 10); len(got) != 0 {
			t.Fatalf("soft-deleted title should not return, got %v", got)
		}
	})
}
