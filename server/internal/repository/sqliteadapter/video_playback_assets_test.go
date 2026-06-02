package sqliteadapter

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// seedDonePlaybackVideo creates a finished recording with `parts` parts and
// returns its video ID. Part 2 existing is what makes it eligible for a
// playback artifact (>= 2 contiguous parts).
func seedDonePlaybackVideo(t *testing.T, ctx context.Context, a *SQLiteAdapter, jobID, filename, broadcasterID string, parts int) int64 {
	t.Helper()
	v, err := a.CreateVideo(ctx, &repository.VideoInput{
		JobID: jobID, Filename: filename, DisplayName: broadcasterID,
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		BroadcasterID: broadcasterID, RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("CreateVideo: %v", err)
	}
	for i := 1; i <= parts; i++ {
		if _, err := a.CreateVideoPart(ctx, &repository.VideoPartInput{
			VideoID: v.ID, PartIndex: int32(i), Filename: fmt.Sprintf("%s-part%02d.mp4", filename, i),
			Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4,
		}); err != nil {
			t.Fatalf("CreateVideoPart: %v", err)
		}
	}
	if err := a.MarkVideoDone(ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("MarkVideoDone: %v", err)
	}
	return v.ID
}

func upsertReadyAsset(t *testing.T, ctx context.Context, a *SQLiteAdapter, videoID int64, name string, lastAccessed time.Time) {
	t.Helper()
	mime := "video/mp4"
	dur, size := 60.0, int64(2048)
	if _, err := a.UpsertVideoPlaybackAsset(ctx, &repository.VideoPlaybackAssetInput{
		VideoID: videoID, Status: repository.PlaybackAssetStatusReady,
		Filename: &name, MimeType: &mime, DurationSeconds: &dur, SizeBytes: &size,
		GeneratedAt: &lastAccessed, LastAccessedAt: &lastAccessed,
	}); err != nil {
		t.Fatalf("upsert ready asset: %v", err)
	}
}

// TestVideoPlaybackAssetReadyToFailedTransition pins that a ready->failed upsert
// NULLs filename/mime/last_accessed_at. The schema CHECK rejects a 'failed' row
// that still carries those fields, so a bug that forgot to clear them would
// surface only here (the fakeRepo can't enforce the constraint).
func TestVideoPlaybackAssetReadyToFailedTransition(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	seedUserChannel(t, ctx, a, "u-pb", "b-pb")
	vid := seedDonePlaybackVideo(t, ctx, a, "job-pb", "rec-pb", "b-pb", 2)

	upsertReadyAsset(t, ctx, a, vid, "rec-pb-playback.mp4", time.Now().UTC())

	msg := "ffmpeg concat failed"
	got, err := a.UpsertVideoPlaybackAsset(ctx, &repository.VideoPlaybackAssetInput{
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

// TestListReadyVideoPlaybackAssetsLRUOrder pins the ORDER BY last_accessed_at
// ASC that the eviction logic depends on. The fakeRepo returns rows verbatim, so
// a wrong ORDER BY (evicting the most-recently-watched first) only shows here.
func TestListReadyVideoPlaybackAssetsLRUOrder(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	seedUserChannel(t, ctx, a, "u-lru", "b-lru")
	now := time.Now().UTC()

	v1 := seedDonePlaybackVideo(t, ctx, a, "job-lru-1", "rec-lru-1", "b-lru", 2)
	v2 := seedDonePlaybackVideo(t, ctx, a, "job-lru-2", "rec-lru-2", "b-lru", 2)
	v3 := seedDonePlaybackVideo(t, ctx, a, "job-lru-3", "rec-lru-3", "b-lru", 2)
	upsertReadyAsset(t, ctx, a, v1, "rec-lru-1-playback.mp4", now.Add(-1*time.Hour))
	upsertReadyAsset(t, ctx, a, v2, "rec-lru-2-playback.mp4", now.Add(-3*time.Hour)) // oldest
	upsertReadyAsset(t, ctx, a, v3, "rec-lru-3-playback.mp4", now.Add(-2*time.Hour))

	rows, err := a.ListReadyVideoPlaybackAssets(ctx)
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

func TestTouchVideoPlaybackAssetMovesReadyAssetToBackOfLRU(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	seedUserChannel(t, ctx, a, "u-touch", "b-touch")
	base := time.Now().UTC().Add(-2 * time.Hour)

	v1 := seedDonePlaybackVideo(t, ctx, a, "job-touch-1", "rec-touch-1", "b-touch", 2)
	v2 := seedDonePlaybackVideo(t, ctx, a, "job-touch-2", "rec-touch-2", "b-touch", 2)
	upsertReadyAsset(t, ctx, a, v1, "rec-touch-1-playback.mp4", base)
	upsertReadyAsset(t, ctx, a, v2, "rec-touch-2-playback.mp4", base.Add(time.Hour))

	if err := a.TouchVideoPlaybackAsset(ctx, v1); err != nil {
		t.Fatalf("TouchVideoPlaybackAsset: %v", err)
	}

	rows, err := a.ListReadyVideoPlaybackAssets(ctx)
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
