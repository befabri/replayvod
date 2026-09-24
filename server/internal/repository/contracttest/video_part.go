package contracttest

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testVideoPartsLifecycle(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "bc-1")
	v := seedLiveJob(t, ctx, repo, "parts-job", "bc-1")
	part := func(index int32, startSeq int64) *repository.VideoPart {
		t.Helper()
		p, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{
			VideoID: v.ID, PartIndex: index, Filename: fmt.Sprintf("parts-job-%d.mp4", index),
			Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4, StartMediaSeq: startSeq,
		})
		if err != nil {
			t.Fatalf("create part %d: %v", index, err)
		}
		return p
	}
	p1, p2 := part(1, 0), part(2, 100)
	if got, err := repo.GetVideoPartByIndex(ctx, v.ID, 1); err != nil || got.ID != p1.ID || got.Filename != "parts-job-1.mp4" || got.StartMediaSeq != 0 || got.EndMediaSeq != nil {
		t.Fatalf("part = %+v, %v", got, err)
	}
	if got, err := repo.GetVideoPartByIndex(ctx, v.ID, 2); err != nil || got.ID != p2.ID || got.StartMediaSeq != 100 {
		t.Fatalf("part by index = %+v, %v", got, err)
	}
	if _, err := repo.GetVideoPartByIndex(ctx, v.ID, 3); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing part index: %v", err)
	}
	if parts, err := repo.ListVideoParts(ctx, v.ID); err != nil || len(parts) != 2 {
		t.Fatalf("parts = %+v, %v", parts, err)
	}
	if ok, err := repo.HasFinalizedVideoParts(ctx, v.ID); err != nil || ok {
		t.Fatalf("unfinalized parts reported as output: %v, %v", ok, err)
	}
	if err := repo.FinalizeVideoPart(ctx, &repository.VideoPartFinalize{ID: p1.ID, DurationSeconds: 30, SizeBytes: 4096, EndMediaSeq: 99}); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.HasFinalizedVideoParts(ctx, v.ID); err != nil || !ok {
		t.Fatalf("finalized part not reported: %v, %v", ok, err)
	}
}

func testVideoMarkDone(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "execution-channel")
	v, err := repo.CreateVideo(ctx, executionInput("done-video"))
	if err != nil {
		t.Fatal(err)
	}
	poster := "poster.jpg"
	if err := repo.MarkVideoDone(ctx, v.ID, 3600.5, 1<<30, &poster, repository.CompletionKindComplete, false); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetVideo(ctx, v.ID); err != nil || got.Status != repository.VideoStatusDone || got.DurationSeconds == nil || *got.DurationSeconds != 3600.5 ||
		got.SizeBytes == nil || *got.SizeBytes != 1<<30 || got.Thumbnail == nil || *got.Thumbnail != poster || got.DownloadedAt == nil {
		t.Fatalf("finished video = %+v, %v", got, err)
	}
}

func testPlaybackAssetLookupAndReadyBytes(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "bc-1")
	ready := seedDonePlaybackVideo(t, ctx, repo, "asset-ready", "asset-ready", "bc-1", 2)
	building := seedDonePlaybackVideo(t, ctx, repo, "asset-building", "asset-building", "bc-1", 2)
	now := time.Now().UTC().Truncate(time.Second)
	upsertReadyAsset(t, ctx, repo, ready, "asset-ready.mp4", now)
	if _, err := repo.UpsertVideoPlaybackAsset(ctx, &repository.VideoPlaybackAssetInput{VideoID: building, Status: repository.PlaybackAssetStatusBuilding}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetVideoPlaybackAsset(ctx, ready)
	if err != nil || got.Status != repository.PlaybackAssetStatusReady || got.Filename == nil || *got.Filename != "asset-ready.mp4" || got.SizeBytes == nil || *got.SizeBytes != 2048 || got.LastAccessedAt == nil || !got.LastAccessedAt.Equal(now) {
		t.Fatalf("ready asset = %+v, %v", got, err)
	}
	if _, err := repo.GetVideoPlaybackAsset(ctx, ready+building+1); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("asset of an unknown video: %v", err)
	}
	if n, err := repo.SumReadyPlaybackBytes(ctx); err != nil || n != 2048 {
		t.Fatalf("ready bytes = %d, %v; a building asset must not count", n, err)
	}
	if err := repo.DeleteVideoPlaybackAsset(ctx, ready); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteVideoPlaybackAsset(ctx, ready); err != nil {
		t.Fatalf("deleting a missing asset: %v", err)
	}
	if _, err := repo.GetVideoPlaybackAsset(ctx, ready); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("deleted asset survived: %v", err)
	}
	if n, err := repo.SumReadyPlaybackBytes(ctx); err != nil || n != 0 {
		t.Fatalf("ready bytes after delete = %d, %v", n, err)
	}
}

func testListVideoPartsForVideos(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "bc-1")
	first := seedLiveJob(t, ctx, repo, "parts-first", "bc-1")
	second := seedLiveJob(t, ctx, repo, "parts-second", "bc-1")
	third := seedLiveJob(t, ctx, repo, "parts-third", "bc-1")
	for _, p := range []struct {
		video *repository.Video
		index int32
	}{{second, 2}, {first, 3}, {second, 1}, {first, 1}, {third, 1}} {
		if _, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{
			VideoID: p.video.ID, PartIndex: p.index, Filename: fmt.Sprintf("%s-%d.mp4", p.video.JobID, p.index),
			Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4,
		}); err != nil {
			t.Fatalf("create part %d of %s: %v", p.index, p.video.JobID, err)
		}
	}
	if parts, err := repo.ListVideoPartsForVideos(ctx, nil); err != nil || len(parts) != 0 {
		t.Fatalf("parts of no videos = %+v, %v", parts, err)
	}
	if parts, err := repo.ListVideoPartsForVideos(ctx, []int64{third.ID + 1000}); err != nil || len(parts) != 0 {
		t.Fatalf("parts of an unknown video = %+v, %v", parts, err)
	}
	parts, err := repo.ListVideoPartsForVideos(ctx, []int64{second.ID, third.ID + 1000, first.ID, second.ID})
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, partFilenames(parts), []string{"parts-first-1.mp4", "parts-first-3.mp4", "parts-second-1.mp4", "parts-second-2.mp4"})
	for _, p := range parts {
		if p.VideoID != first.ID && p.VideoID != second.ID {
			t.Fatalf("part %+v belongs to a video that was not requested", p)
		}
	}
}

func partFilenames(parts []repository.VideoPart) []string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = p.Filename
	}
	return out
}
