package contracttest

import (
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func streamIDs(streams []repository.Stream) []string {
	out := make([]string, len(streams))
	for i, s := range streams {
		out[i] = s.ID
	}
	return out
}

func testStreamLifecycleAndListing(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "bc-a")
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "bc-b", BroadcasterLogin: "bc-b", BroadcasterName: "bc-b"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	mature := true
	for _, s := range []repository.StreamInput{
		{ID: "a1", BroadcasterID: "bc-a", Type: "live", Language: "en", ViewerCount: 5, StartedAt: now.Add(-2 * time.Hour)},
		{ID: "a2", BroadcasterID: "bc-a", Type: "live", Language: "en", ViewerCount: 5, StartedAt: now.Add(-time.Hour), IsMature: &mature},
		{ID: "b1", BroadcasterID: "bc-b", Type: "live", Language: "fr", ViewerCount: 1, StartedAt: now},
	} {
		got, err := repo.UpsertStream(ctx, &s)
		if err != nil || !got.StartedAt.Equal(s.StartedAt) || (s.IsMature == nil) != (got.IsMature == nil) || (s.IsMature != nil && *got.IsMature != *s.IsMature) {
			t.Fatalf("seed stream %s = %+v, %v", s.ID, got, err)
		}
	}
	active, err := repo.ListActiveStreams(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, streamIDs(active), []string{"b1", "a2", "a1"})
	ended := now.Add(-30 * time.Minute)
	if err := repo.EndStream(ctx, "a1", ended); err != nil {
		t.Fatal(err)
	}
	if err := repo.EndStream(ctx, "a1", now); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetStream(ctx, "a1"); err != nil || got.EndedAt == nil || !got.EndedAt.Equal(ended) {
		t.Fatalf("ended stream = %+v, %v; the first end time must stick", got, err)
	}
	active, err = repo.ListActiveStreams(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, streamIDs(active), []string{"b1", "a2"})
	page, err := repo.ListStreamsByBroadcaster(ctx, "bc-a", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, streamIDs(page), []string{"a2"})
	page, err = repo.ListStreamsByBroadcaster(ctx, "bc-a", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, streamIDs(page), []string{"a1"})
	page, err = repo.ListStreamsByBroadcaster(ctx, "bc-a", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, streamIDs(page), []string{"a2", "a1"})
	if last, err := repo.GetLastLiveStream(ctx, "bc-a"); err != nil || last.ID != "a2" {
		t.Fatalf("last stream of bc-a = %+v, %v", last, err)
	}
	if last, err := repo.GetLastLiveStream(ctx, "bc-b"); err != nil || last.ID != "b1" {
		t.Fatalf("last stream of bc-b = %+v, %v", last, err)
	}
	if _, err := repo.GetLastLiveStream(ctx, "nobody"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("last stream of an unknown broadcaster: %v", err)
	}
}

func testStreamMetadataLinks(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "bc-a")
	if _, err := repo.UpsertStream(ctx, &repository.StreamInput{ID: "s-meta", BroadcasterID: "bc-a", Type: "live", Language: "en", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	first, err := repo.UpsertTitle(ctx, "first title")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.UpsertTitle(ctx, "second title")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{first.ID, second.ID, first.ID} {
		if err := repo.LinkStreamTitle(ctx, "s-meta", id); err != nil {
			t.Fatalf("link title %d: %v", id, err)
		}
	}
	titles, err := repo.ListTitlesForStream(ctx, "s-meta")
	if err != nil || len(titles) != 2 || titles[0].ID != first.ID || titles[1].ID != second.ID {
		t.Fatalf("stream titles = %+v, %v", titles, err)
	}
	if titles, err := repo.ListTitlesForStream(ctx, "missing"); err != nil || len(titles) != 0 {
		t.Fatalf("titles of an unknown stream = %+v, %v", titles, err)
	}
	if _, err := repo.UpsertCategory(ctx, &repository.Category{ID: "cat-1", Name: "Chess"}); err != nil {
		t.Fatal(err)
	}
	tag, err := repo.UpsertTag(ctx, "tag-1")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := repo.LinkStreamCategory(ctx, "s-meta", "cat-1"); err != nil {
			t.Fatalf("link category: %v", err)
		}
		if err := repo.LinkStreamTag(ctx, "s-meta", tag.ID); err != nil {
			t.Fatalf("link tag: %v", err)
		}
	}
}

func testVideoTagsComeFromTheirBroadcast(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "bc-a")
	for _, id := range []string{"s-tagged", "s-untagged"} {
		if _, err := repo.UpsertStream(ctx, &repository.StreamInput{ID: id, BroadcasterID: "bc-a", Type: "live", Language: "en", StartedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	var tagIDs []int64
	for _, name := range []string{"beta", "élan", "Zulu", "alpha", "Élan", "Alpha"} {
		tag, err := repo.UpsertTag(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		tagIDs = append(tagIDs, tag.ID)
	}
	for _, id := range append(tagIDs, tagIDs[0]) {
		if err := repo.LinkStreamTag(ctx, "s-tagged", id); err != nil {
			t.Fatal(err)
		}
	}
	video := func(jobID string, streamID *string) int64 {
		t.Helper()
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: "bc-a", Status: repository.VideoStatusDone,
			Quality: repository.QualityHigh, BroadcasterID: "bc-a", StreamID: streamID, Language: "en",
			RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatal(err)
		}
		return v.ID
	}
	tagged, untagged := "s-tagged", "s-untagged"
	first, restarted := video("tags-first", &tagged), video("tags-restarted", &tagged)
	quiet, archive := video("tags-quiet", &untagged), video("tags-archive", nil)

	got, err := repo.ListTagsForVideos(ctx, []int64{first, restarted, quiet, archive, 99999})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("videos with tags = %v, want only the two recordings of the tagged broadcast", got)
	}
	for _, id := range []int64{first, restarted} {
		assertStringSlice(t, tagNames(got[id]), []string{"Alpha", "Zulu", "alpha", "beta", "Élan", "élan"})
	}
	if empty, err := repo.ListTagsForVideos(ctx, nil); err != nil || len(empty) != 0 {
		t.Fatalf("empty batch = %v, %v", empty, err)
	}
}
