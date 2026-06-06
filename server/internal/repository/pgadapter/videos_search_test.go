package pgadapter

import (
	"context"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func TestSearchVideos(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	for _, ch := range []repository.Channel{
		{BroadcasterID: "bc-title", BroadcasterLogin: "titlecaster", BroadcasterName: "Title Caster"},
		{BroadcasterID: "bc-channel", BroadcasterLogin: "neoncaster", BroadcasterName: "Neon Caster"},
		{BroadcasterID: "bc-category", BroadcasterLogin: "categorycaster", BroadcasterName: "Category Caster"},
	} {
		channel := ch
		if _, err := a.UpsertChannel(ctx, &channel); err != nil {
			t.Fatalf("seed channel %s: %v", ch.BroadcasterID, err)
		}
	}
	for _, cat := range []repository.Category{
		{ID: "cat-neon", Name: "Neon Game"},
		{ID: "cat-other", Name: "Other Game"},
	} {
		category := cat
		if _, err := a.UpsertCategory(ctx, &category); err != nil {
			t.Fatalf("seed category %s: %v", cat.ID, err)
		}
	}

	base := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	seeds := []struct {
		jobID         string
		title         string
		displayName   string
		broadcasterID string
		minute        int
		categoryID    string
		historyTitle  string
	}{
		{"job-title", "Neon Run", "Title Caster", "bc-title", 1, "cat-other", ""},
		{"job-title-history", "Opening Soon", "Title Caster", "bc-title", 2, "cat-other", "Neon Finale"},
		{"job-channel", "Different", "Neon Caster", "bc-channel", 3, "cat-other", ""},
		{"job-category", "Different", "Category Caster", "bc-category", 4, "cat-neon", ""},
		{"job-substring", "Late Neon Mix", "Title Caster", "bc-title", 5, "cat-other", ""},
	}
	for _, s := range seeds {
		v, err := a.CreateVideo(ctx, &repository.VideoInput{
			JobID:         s.jobID,
			Filename:      s.jobID,
			DisplayName:   s.displayName,
			Title:         s.title,
			Status:        repository.VideoStatusDone,
			Quality:       repository.QualityHigh,
			BroadcasterID: s.broadcasterID,
			Language:      "en",
			RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("create %s: %v", s.jobID, err)
		}
		if s.categoryID != "" {
			if err := a.LinkVideoCategory(ctx, v.ID, s.categoryID); err != nil {
				t.Fatalf("link category %s: %v", s.jobID, err)
			}
		}
		if s.historyTitle != "" {
			title, err := a.UpsertTitle(ctx, s.historyTitle)
			if err != nil {
				t.Fatalf("upsert history title %s: %v", s.jobID, err)
			}
			if err := a.LinkVideoTitle(ctx, v.ID, title.ID); err != nil {
				t.Fatalf("link history title %s: %v", s.jobID, err)
			}
		}
		startedAt := base.Add(time.Duration(s.minute) * time.Minute)
		if _, err := a.db.Exec(ctx, "UPDATE videos SET start_download_at = $1 WHERE id = $2", startedAt, v.ID); err != nil {
			t.Fatalf("override start_download_at %s: %v", s.jobID, err)
		}
	}

	t.Run("title and title-history rank before channel/category matches", func(t *testing.T) {
		got, err := a.SearchVideos(ctx, "neon", 10)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		jobs := pgVideoJobIDs(got)
		if len(jobs) < 5 {
			t.Fatalf("expected all seeded matches, got %v", jobs)
		}
		wantFirst := map[string]bool{"job-title": true, "job-title-history": true}
		if !wantFirst[jobs[0]] || !wantFirst[jobs[1]] {
			t.Fatalf("title matches should rank first, got %v", jobs)
		}
	})

	t.Run("query matches broadcaster metadata case-insensitively", func(t *testing.T) {
		got, err := a.SearchVideos(ctx, "NEONCASTER", 10)
		if err != nil {
			t.Fatalf("search channel: %v", err)
		}
		if len(got) == 0 || got[0].JobID != "job-channel" {
			t.Fatalf("expected job-channel first, got %v", pgVideoJobIDs(got))
		}
	})

	t.Run("query matches linked category", func(t *testing.T) {
		got, err := a.SearchVideos(ctx, "Neon Game", 10)
		if err != nil {
			t.Fatalf("search category: %v", err)
		}
		if len(got) == 0 || got[0].JobID != "job-category" {
			t.Fatalf("expected job-category first, got %v", pgVideoJobIDs(got))
		}
	})

	t.Run("limit caps result rows", func(t *testing.T) {
		got, err := a.SearchVideos(ctx, "neon", 2)
		if err != nil {
			t.Fatalf("search limit: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("limit=2 should return 2 rows, got %d", len(got))
		}
	})

	t.Run("soft-deleted videos are excluded", func(t *testing.T) {
		if err := a.SoftDeleteVideo(ctx, pgGotVideoID(t, ctx, a, "job-title"), repository.DeletionKindManual); err != nil {
			t.Fatalf("soft delete: %v", err)
		}
		got, err := a.SearchVideos(ctx, "Neon Run", 10)
		if err != nil {
			t.Fatalf("search deleted: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("soft-deleted title should not return, got %v", pgVideoJobIDs(got))
		}
	})
}

func pgVideoJobIDs(videos []repository.Video) []string {
	out := make([]string, len(videos))
	for i, v := range videos {
		out[i] = v.JobID
	}
	return out
}

func pgGotVideoID(t *testing.T, ctx context.Context, a *PGAdapter, jobID string) int64 {
	t.Helper()
	v, err := a.GetVideoByJobID(ctx, jobID)
	if err != nil {
		t.Fatalf("get video %s: %v", jobID, err)
	}
	return v.ID
}
