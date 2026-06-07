package contracttest

import (
	"context"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testListChannelsPageCursorPagination(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	if _, err := repo.UpsertUser(ctx, &repository.User{
		ID: "user-channel-favorites", Login: "channel-favorites", DisplayName: "Channel Favorites", Role: "viewer",
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := repo.UpsertUser(ctx, &repository.User{
		ID: "user-channel-other", Login: "channel-other", DisplayName: "Channel Other", Role: "viewer",
	}); err != nil {
		t.Fatalf("seed other user: %v", err)
	}

	channels := []repository.Channel{
		{BroadcasterID: "1", BroadcasterLogin: "alpha", BroadcasterName: "Alpha"},
		{BroadcasterID: "2", BroadcasterLogin: "bravo", BroadcasterName: "Bravo"},
		{BroadcasterID: "3", BroadcasterLogin: "bravo-alt", BroadcasterName: "Bravo"},
		{BroadcasterID: "4", BroadcasterLogin: "charlie", BroadcasterName: "Charlie"},
	}
	for _, c := range channels {
		ch := c
		if _, err := repo.UpsertChannel(ctx, &ch); err != nil {
			t.Fatalf("seed channel %s: %v", c.BroadcasterLogin, err)
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	for _, liveID := range []string{"1", "3"} {
		if _, err := repo.UpsertStream(ctx, &repository.StreamInput{
			ID: liveID + "-live", BroadcasterID: liveID, Type: "live", Language: "en",
			ViewerCount: 1, StartedAt: now,
		}); err != nil {
			t.Fatalf("seed live stream %s: %v", liveID, err)
		}
	}

	seedVideo := func(jobID, broadcasterID string, failed, deleted bool) {
		t.Helper()
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: jobID,
			Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
			BroadcasterID: broadcasterID, Language: "en",
			RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("seed video %s: %v", jobID, err)
		}
		if failed {
			if err := repo.MarkVideoFailed(ctx, v.ID, "seed failed", repository.CompletionKindPartial, true); err != nil {
				t.Fatalf("mark failed %s: %v", jobID, err)
			}
			return
		}
		if err := repo.MarkVideoDone(ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
			t.Fatalf("mark done %s: %v", jobID, err)
		}
		if deleted {
			if err := repo.SoftDeleteVideo(ctx, v.ID, repository.DeletionKindManual); err != nil {
				t.Fatalf("soft delete %s: %v", jobID, err)
			}
		}
	}
	seedVideo("job-bravo", "2", false, false)
	seedVideo("job-bravo-alt-failed", "3", true, false)
	seedVideo("job-charlie-deleted", "4", false, true)
	if _, err := repo.SetChannelFavorite(ctx, "user-channel-favorites", "1", true); err != nil {
		t.Fatalf("seed favorite alpha: %v", err)
	}
	if _, err := repo.SetChannelFavorite(ctx, "user-channel-favorites", "3", true); err != nil {
		t.Fatalf("seed favorite bravo-alt: %v", err)
	}
	if _, err := repo.SetChannelFavorite(ctx, "user-channel-other", "2", true); err != nil {
		t.Fatalf("seed other favorite bravo: %v", err)
	}

	cases := []struct {
		name   string
		sort   string
		filter string
		want   []string
	}{
		{"name asc", "name_asc", repository.ChannelFilterAll, []string{"alpha", "bravo", "bravo-alt", "charlie"}},
		{"name desc", "name_desc", repository.ChannelFilterAll, []string{"charlie", "bravo-alt", "bravo", "alpha"}},
		{"live only", "name_asc", repository.ChannelFilterLive, []string{"alpha", "bravo-alt"}},
		{"downloaded only", "name_asc", repository.ChannelFilterDownloaded, []string{"bravo"}},
		{"favorites only", "name_asc", repository.ChannelFilterFavorites, []string{"alpha", "bravo-alt"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collectChannelPageLogins(t, ctx, repo, 2, tc.sort, tc.filter, "user-channel-favorites")
			assertStringSlice(t, got, tc.want)
		})
	}
}

func testListLatestLivePerChannelOnePerBroadcaster(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	profile := "https://example.com/a.png"
	for _, c := range []repository.Channel{
		{BroadcasterID: "ch-a", BroadcasterLogin: "a", BroadcasterName: "A", ProfileImageURL: &profile},
		{BroadcasterID: "ch-b", BroadcasterLogin: "b", BroadcasterName: "B"},
	} {
		ch := c
		if _, err := repo.UpsertChannel(ctx, &ch); err != nil {
			t.Fatalf("seed channel %s: %v", c.BroadcasterID, err)
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	streams := []struct {
		id, bc string
		offset time.Duration
	}{
		{"s-a-old", "ch-a", -4 * time.Hour},
		{"s-a-new", "ch-a", -30 * time.Minute},
		{"s-b-1", "ch-b", -2 * time.Hour},
	}
	for _, s := range streams {
		if _, err := repo.UpsertStream(ctx, &repository.StreamInput{
			ID: s.id, BroadcasterID: s.bc, Type: "live", Language: "en",
			ViewerCount: 1, StartedAt: now.Add(s.offset),
		}); err != nil {
			t.Fatalf("seed stream %s: %v", s.id, err)
		}
	}

	got, err := repo.ListLatestLivePerChannel(ctx, 10)
	if err != nil {
		t.Fatalf("latest live: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rows (one per broadcaster), got %d", len(got))
	}
	if got[0].ID != "s-a-new" || got[0].BroadcasterID != "ch-a" {
		t.Errorf("row 0: want s-a-new/ch-a, got %s/%s", got[0].ID, got[0].BroadcasterID)
	}
	if got[0].BroadcasterLogin != "a" || got[0].BroadcasterName != "A" {
		t.Errorf("row 0 display info: got login=%q name=%q", got[0].BroadcasterLogin, got[0].BroadcasterName)
	}
	if got[0].ProfileImageURL == nil || *got[0].ProfileImageURL != profile {
		t.Errorf("row 0 profile image: got %v", got[0].ProfileImageURL)
	}
	if got[1].ID != "s-b-1" || got[1].BroadcasterID != "ch-b" {
		t.Errorf("row 1: want s-b-1/ch-b, got %s/%s", got[1].ID, got[1].BroadcasterID)
	}
}

func testListChannelsByIDs(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	for _, id := range []string{"1", "2", "3"} {
		if _, err := repo.UpsertChannel(ctx, &repository.Channel{
			BroadcasterID: id, BroadcasterLogin: "l-" + id, BroadcasterName: "n-" + id,
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	t.Run("matched + missing ids", func(t *testing.T) {
		got, err := repo.ListChannelsByIDs(ctx, []string{"1", "3", "missing"})
		if err != nil {
			t.Fatalf("by ids: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2 matched rows, got %d", len(got))
		}
		gotIDs := map[string]bool{}
		for _, c := range got {
			gotIDs[c.BroadcasterID] = true
		}
		if !gotIDs["1"] || !gotIDs["3"] {
			t.Errorf("expected ids 1 and 3, got %v", gotIDs)
		}
	})

	t.Run("nil ids returns empty, no error", func(t *testing.T) {
		empty, err := repo.ListChannelsByIDs(ctx, nil)
		if err != nil {
			t.Fatalf("by nil ids: %v", err)
		}
		if len(empty) != 0 {
			t.Errorf("nil ids should return 0 rows, got %d", len(empty))
		}
	})

	t.Run("duplicate ids deduped by set semantics", func(t *testing.T) {
		got, err := repo.ListChannelsByIDs(ctx, []string{"1", "1", "2"})
		if err != nil {
			t.Fatalf("by dup ids: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("duplicates should collapse, got %d rows", len(got))
		}
	})
}

func collectChannelPageLogins(t *testing.T, ctx context.Context, repo repository.Repository, limit int, sort string, filter string, userID string) []string {
	t.Helper()
	var cursor *repository.ChannelPageCursor
	out := []string{}
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("channel pagination did not terminate")
		}
		page, err := repo.ListChannelsPage(ctx, limit, sort, filter, userID, cursor)
		if err != nil {
			t.Fatalf("ListChannelsPage: %v", err)
		}
		if len(page.Items) > limit {
			t.Fatalf("page size: got %d, limit %d", len(page.Items), limit)
		}
		for _, item := range page.Items {
			out = append(out, item.BroadcasterLogin)
		}
		if page.NextCursor == nil {
			return out
		}
		if len(page.Items) == 0 {
			t.Fatal("empty channel page returned a next cursor")
		}
		cursor = page.NextCursor
	}
}
