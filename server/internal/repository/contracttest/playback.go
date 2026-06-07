package contracttest

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// testPlaybackAssetReadyToFailedTransition pins that a ready->failed upsert
// clears filename/mime/last_accessed_at. The schema CHECK rejects a 'failed'
// row that still carries those fields, so a bug that forgot to clear them would
// surface here.
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

// testPlaybackAssetListReadyLRUOrder pins the ORDER BY last_accessed_at ASC
// that the eviction logic depends on (oldest-accessed first).
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

	rows, err := repo.ListReadyVideoPlaybackAssets(ctx)
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

// testPlaybackAssetTouchMovesToBackOfLRU pins that TouchVideoPlaybackAsset
// updates last_accessed_at so the touched asset becomes most-recently-used.
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

	rows, err := repo.ListReadyVideoPlaybackAssets(ctx)
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
