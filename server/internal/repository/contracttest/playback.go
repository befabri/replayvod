package contracttest

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// The schema requires ready-only fields to clear when an asset becomes failed.
func testPlaybackAssetReadyToFailedTransition(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u-pb", "b-pb")
	vid := seedDonePlaybackVideo(t, ctx, repo, "job-pb", "rec-pb", "b-pb", 2)

	upsertReadyAsset(t, ctx, repo, vid, "rec-pb-playback.mp4", time.Now().UTC())

	msg := "ffmpeg concat failed"
	got, err := repo.UpsertVideoPlaybackAsset(ctx, &repository.VideoPlaybackAssetInput{
		VideoID: vid, Status: repository.PlaybackAssetStatusFailed, Error: &msg,
	})
	if err != nil {
		t.Fatalf("ready->failed transition violated the CHECK constraint: %v", err)
	}
	if got.Status != repository.PlaybackAssetStatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if got.Filename != nil || got.MimeType != nil || got.LastAccessedAt != nil {
		t.Fatalf("failed row still carries ready fields: %#v", got)
	}
}

func testPlaybackAssetPaginationWithTiedTimestamps(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "page-owner", "page-channel")
	at := time.Now().UTC().Truncate(time.Second)
	var want []int64
	for i := range 5 {
		name := fmt.Sprintf("lru-page-%d", i)
		id := seedDonePlaybackVideo(t, ctx, repo, name, name, "page-channel", 2)
		upsertReadyAsset(t, ctx, repo, id, name+"-playback.mp4", at)
		want = append(want, id)
	}
	var got []int64
	cursor := repository.PlaybackAssetCursor{}
	for page := 0; ; page++ {
		if page > 3 {
			t.Fatal("LRU cursor did not advance")
		}
		rows, err := repo.ListReadyVideoPlaybackAssets(ctx, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			got = append(got, row.VideoID)
			cursor = repository.PlaybackAssetCursor{AccessedAt: *row.LastAccessedAt, GeneratedAt: *row.GeneratedAt, VideoID: row.VideoID}
		}
		if len(rows) < 2 {
			break
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("LRU pagination skipped or duplicated tied rows: got=%v want=%v", got, want)
	}
}

func testPlaybackAssetListReadyLRUOrder(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u-lru", "b-lru")
	now := time.Now().UTC()

	v1 := seedDonePlaybackVideo(t, ctx, repo, "job-lru-1", "rec-lru-1", "b-lru", 2)
	v2 := seedDonePlaybackVideo(t, ctx, repo, "job-lru-2", "rec-lru-2", "b-lru", 2)
	v3 := seedDonePlaybackVideo(t, ctx, repo, "job-lru-3", "rec-lru-3", "b-lru", 2)
	upsertReadyAsset(t, ctx, repo, v1, "rec-lru-1-playback.mp4", now.Add(-1*time.Hour))
	upsertReadyAsset(t, ctx, repo, v2, "rec-lru-2-playback.mp4", now.Add(-3*time.Hour)) // oldest
	upsertReadyAsset(t, ctx, repo, v3, "rec-lru-3-playback.mp4", now.Add(-2*time.Hour))

	rows, err := repo.ListReadyVideoPlaybackAssets(ctx, repository.PlaybackAssetCursor{}, 100)
	if err != nil {
		t.Fatalf("ListReadyVideoPlaybackAssets: %v", err)
	}
	gotOrder := make([]int64, len(rows))
	for i, r := range rows {
		gotOrder[i] = r.VideoID
	}
	want := []int64{v2, v3, v1} // oldest last_accessed_at first
	if !slices.Equal(gotOrder, want) {
		t.Fatalf("LRU order = %v, want %v (oldest last_accessed_at first)", gotOrder, want)
	}
}

func testPlaybackAssetTouchMovesToBackOfLRU(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u-touch", "b-touch")
	base := time.Now().UTC().Add(-2 * time.Hour)

	v1 := seedDonePlaybackVideo(t, ctx, repo, "job-touch-1", "rec-touch-1", "b-touch", 2)
	v2 := seedDonePlaybackVideo(t, ctx, repo, "job-touch-2", "rec-touch-2", "b-touch", 2)
	upsertReadyAsset(t, ctx, repo, v1, "rec-touch-1-playback.mp4", base)
	upsertReadyAsset(t, ctx, repo, v2, "rec-touch-2-playback.mp4", base.Add(time.Hour))

	if err := repo.TouchVideoPlaybackAsset(ctx, v1); err != nil {
		t.Fatalf("TouchVideoPlaybackAsset: %v", err)
	}

	rows, err := repo.ListReadyVideoPlaybackAssets(ctx, repository.PlaybackAssetCursor{}, 100)
	if err != nil {
		t.Fatalf("ListReadyVideoPlaybackAssets: %v", err)
	}
	gotOrder := make([]int64, len(rows))
	for i, r := range rows {
		gotOrder[i] = r.VideoID
	}
	want := []int64{v2, v1}
	if !slices.Equal(gotOrder, want) {
		t.Fatalf("LRU order after touch = %v, want %v", gotOrder, want)
	}
	if rows[1].LastAccessedAt == nil || !rows[1].LastAccessedAt.After(base.Add(time.Hour)) {
		t.Fatalf("touched last_accessed_at = %v, want after untouched row", rows[1].LastAccessedAt)
	}
}
