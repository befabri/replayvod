package contracttest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// testListVideosForStorageScan pins the candidate set of the storage scan:
// live DONE rows, FAILED rows that salvaged parts, nothing queued for manual
// deletion, nothing tombstoned, and nothing whose pending webhook delivery has
// not frozen its part list yet. The video_id filter narrows to one row.
func testListVideosForStorageScan(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u-scan", "b-scan")

	mk := func(jobID string, parts int) *repository.Video {
		t.Helper()
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: "b-scan",
			Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
			BroadcasterID: "b-scan", RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("CreateVideo %s: %v", jobID, err)
		}
		for i := 1; i <= parts; i++ {
			if _, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{
				VideoID: v.ID, PartIndex: int32(i), Filename: fmt.Sprintf("%s-part%02d.mp4", jobID, i),
				Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4,
			}); err != nil {
				t.Fatalf("CreateVideoPart %s: %v", jobID, err)
			}
		}
		return v
	}
	done := func(v *repository.Video) *repository.Video {
		t.Helper()
		if err := repo.MarkVideoDone(ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
			t.Fatalf("MarkVideoDone %s: %v", v.JobID, err)
		}
		return v
	}

	doneParts := done(mk("scan-done", 2))
	legacy := done(mk("scan-legacy", 0))
	failedEmpty := mk("scan-failed-empty", 0)
	if err := repo.MarkVideoFailed(ctx, failedEmpty.ID, "boom", repository.CompletionKindCancelled, false); err != nil {
		t.Fatalf("MarkVideoFailed empty: %v", err)
	}
	failedSalvage := mk("scan-failed-salvage", 1)
	if err := repo.MarkVideoFailed(ctx, failedSalvage.ID, "boom", repository.CompletionKindPartial, false); err != nil {
		t.Fatalf("MarkVideoFailed salvage: %v", err)
	}
	mk("scan-pending", 1)
	queued := done(mk("scan-queued", 1))
	if _, err := repo.RequestVideoDelete(ctx, queued.ID); err != nil {
		t.Fatalf("RequestVideoDelete: %v", err)
	}
	gone := done(mk("scan-gone", 1))
	if err := repo.SoftDeleteVideo(ctx, gone.ID, repository.DeletionKindMissing); err != nil {
		t.Fatalf("SoftDeleteVideo: %v", err)
	}
	// A pending webhook delivery does not hold a recording back: the tombstone
	// keeps its part rows, so the delivery can still freeze them.
	delivering := done(mk("scan-delivering", 1))
	if _, err := repo.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
		MessageID: "msg-scan-delivering", DedupeKey: "dedupe-scan-delivering", Event: "recording.completed",
		VideoID: delivering.ID, NextAttemptAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateRecordingWebhookDelivery: %v", err)
	}
	_ = delivering

	rows, err := repo.ListVideosForStorageScan(ctx, 0, 100)
	if err != nil {
		t.Fatalf("ListVideosForStorageScan: %v", err)
	}
	assertStringSlice(t, scanFilenames(rows), []string{"scan-done", "scan-legacy", "scan-failed-salvage", "scan-delivering"})
	for _, r := range rows {
		want := repository.VideoStatusDone
		if r.Filename == "scan-failed-salvage" {
			want = repository.VideoStatusFailed
		}
		if r.Status != want {
			t.Fatalf("%s status = %q, want %q", r.Filename, r.Status, want)
		}
	}

	for _, id := range []int64{queued.ID, gone.ID, failedEmpty.ID} {
		changed, err := repo.TombstoneMissingVideo(ctx, id)
		if err != nil || changed {
			t.Fatalf("ineligible tombstone %d = %v, %v", id, changed, err)
		}
	}

	row, err := repo.GetVideoForStorageScan(ctx, doneParts.ID)
	if err != nil || row.VideoID != doneParts.ID {
		t.Fatalf("exact candidate = %+v, %v", row, err)
	}
	for _, id := range []int64{0, -1, legacy.ID + 1000, gone.ID, queued.ID, failedEmpty.ID} {
		row, err = repo.GetVideoForStorageScan(ctx, id)
		if !errors.Is(err, repository.ErrNotFound) || row != nil {
			t.Fatalf("candidate %d = %+v, %v", id, row, err)
		}
	}
	var paged []repository.StorageScanVideo
	var cursor int64
	for {
		page, err := repo.ListVideosForStorageScan(ctx, cursor, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		if len(page) != 1 || page[0].VideoID <= cursor {
			t.Fatalf("invalid page: %+v", page)
		}
		paged = append(paged, page...)
		cursor = page[0].VideoID
	}
	assertStringSlice(t, scanFilenames(paged), scanFilenames(rows))
	for _, limit := range []int{0, -1, 1001} {
		if _, err := repo.ListVideosForStorageScan(ctx, 0, limit); err == nil {
			t.Fatalf("accepted limit %d", limit)
		}
	}

	changed, err := repo.TombstoneMissingVideo(ctx, doneParts.ID)
	if err != nil || !changed {
		t.Fatalf("tombstone = %v, %v", changed, err)
	}
	changed, err = repo.TombstoneMissingVideo(ctx, doneParts.ID)
	if err != nil || changed {
		t.Fatalf("repeated tombstone = %v, %v", changed, err)
	}
	parts, err := repo.ListVideoParts(ctx, doneParts.ID)
	if err != nil || len(parts) != 2 {
		t.Fatalf("tombstone lost part metadata: %+v, %v", parts, err)
	}

}

func scanFilenames(rows []repository.StorageScanVideo) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Filename
	}
	return out
}

// testSoftDeleteVideoThumbnail pins the poster rule: a missing-media tombstone
// keeps the thumbnail path, every other kind clears it because the purge
// deleted the object.
func testSoftDeleteVideoThumbnail(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u-poster", "b-poster")

	mk := func(jobID, kind string) *repository.Video {
		t.Helper()
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: "b-poster",
			Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
			BroadcasterID: "b-poster", RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("CreateVideo %s: %v", jobID, err)
		}
		poster := "thumbnails/" + jobID + "-part01.jpg"
		if err := repo.MarkVideoDone(ctx, v.ID, 60, 1024, &poster, repository.CompletionKindComplete, false); err != nil {
			t.Fatalf("MarkVideoDone %s: %v", jobID, err)
		}
		var deleteErr error
		if kind == repository.DeletionKindMissing {
			_, deleteErr = repo.TombstoneMissingVideo(ctx, v.ID)
		} else {
			deleteErr = repo.SoftDeleteVideo(ctx, v.ID, kind)
		}
		if err := deleteErr; err != nil {
			t.Fatalf("SoftDeleteVideo %s: %v", jobID, err)
		}
		got, err := repo.GetVideo(ctx, v.ID)
		if err != nil {
			t.Fatalf("GetVideo %s: %v", jobID, err)
		}
		return got
	}

	if got := mk("poster-missing", repository.DeletionKindMissing); got.Thumbnail == nil || *got.Thumbnail != "thumbnails/poster-missing-part01.jpg" {
		t.Fatalf("missing tombstone thumbnail = %v, want kept", got.Thumbnail)
	}
	if got := mk("poster-retention", repository.DeletionKindRetention); got.Thumbnail != nil {
		t.Fatalf("retention tombstone thumbnail = %q, want cleared", *got.Thumbnail)
	}
	if got := mk("poster-manual", repository.DeletionKindManual); got.Thumbnail != nil {
		t.Fatalf("manual tombstone thumbnail = %q, want cleared", *got.Thumbnail)
	}
}
