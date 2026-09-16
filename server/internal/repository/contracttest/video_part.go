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
	if job, err := repo.GetJobByVideoID(ctx, v.ID); err != nil || job.ID != "parts-job" || job.VideoID != v.ID {
		t.Fatalf("job by video = %+v, %v", job, err)
	}
	if _, err := repo.GetJobByVideoID(ctx, v.ID+1); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("job of an unknown video: %v", err)
	}
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
	if got, err := repo.GetVideoPart(ctx, p1.ID); err != nil || got.PartIndex != 1 || got.Filename != "parts-job-1.mp4" || got.StartMediaSeq != 0 || got.EndMediaSeq != nil {
		t.Fatalf("part = %+v, %v", got, err)
	}
	if _, err := repo.GetVideoPart(ctx, p1.ID+p2.ID+1); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing part: %v", err)
	}
	if got, err := repo.GetVideoPartByIndex(ctx, v.ID, 2); err != nil || got.ID != p2.ID || got.StartMediaSeq != 100 {
		t.Fatalf("part by index = %+v, %v", got, err)
	}
	if _, err := repo.GetVideoPartByIndex(ctx, v.ID, 3); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing part index: %v", err)
	}
	if n, err := repo.CountVideoParts(ctx, v.ID); err != nil || n != 2 {
		t.Fatalf("part count = %d, %v", n, err)
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
	if err := repo.DeleteVideoParts(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteVideoParts(ctx, v.ID); err != nil {
		t.Fatalf("deleting parts twice: %v", err)
	}
	if n, err := repo.CountVideoParts(ctx, v.ID); err != nil || n != 0 {
		t.Fatalf("part count after delete = %d, %v", n, err)
	}
	if ok, err := repo.HasFinalizedVideoParts(ctx, v.ID); err != nil || ok {
		t.Fatalf("deleted parts still reported: %v, %v", ok, err)
	}
	if _, err := repo.GetVideoPart(ctx, p1.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("deleted part survived: %v", err)
	}
}

func testVideoCountsAndMissingThumbnails(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "execution-channel")
	create := func(job string) *repository.Video {
		t.Helper()
		v, err := repo.CreateVideo(ctx, executionInput(job))
		if err != nil {
			t.Fatalf("create %s: %v", job, err)
		}
		return v
	}
	create("count-pending")
	bare, withPoster := create("count-done-1"), create("count-done-2")
	if err := repo.MarkVideoDone(ctx, bare.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatal(err)
	}
	poster := "poster.jpg"
	if err := repo.MarkVideoDone(ctx, withPoster.ID, 60, 1024, &poster, repository.CompletionKindComplete, false); err != nil {
		t.Fatal(err)
	}
	for status, want := range map[string]int64{repository.VideoStatusPending: 1, repository.VideoStatusDone: 2, repository.VideoStatusFailed: 0} {
		if n, err := repo.CountVideosByStatus(ctx, status); err != nil || n != want {
			t.Fatalf("count of %s = %d, %v; want %d", status, n, err, want)
		}
	}
	if missing, err := repo.ListVideosMissingThumbnail(ctx); err != nil || len(missing) != 1 || missing[0].ID != bare.ID {
		t.Fatalf("videos missing a poster = %v, %v", videoJobIDs(missing), err)
	}
	if err := repo.SetVideoThumbnail(ctx, bare.ID, "late.jpg"); err != nil {
		t.Fatal(err)
	}
	if missing, err := repo.ListVideosMissingThumbnail(ctx); err != nil || len(missing) != 0 {
		t.Fatalf("videos missing a poster after set = %v, %v", videoJobIDs(missing), err)
	}
	if err := repo.SoftDeleteVideo(ctx, withPoster.ID, repository.DeletionKindManual); err != nil {
		t.Fatal(err)
	}
	if n, err := repo.CountVideosByStatus(ctx, repository.VideoStatusDone); err != nil || n != 1 {
		t.Fatalf("done count after tombstone = %d, %v", n, err)
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
