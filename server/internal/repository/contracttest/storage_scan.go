package contracttest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

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
	// Missing-media tombstones retain part rows so pending deliveries can freeze them.
	delivering := done(mk("scan-delivering", 1))
	if _, err := repo.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
		MessageID: "msg-scan-delivering", DedupeKey: "dedupe-scan-delivering", Event: "recording.completed",
		VideoID: delivering.ID, NextAttemptAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateRecordingWebhookDelivery: %v", err)
	}

	rows, err := repo.ListVideosForStorageScan(ctx, batchPage(t, 0, 100))
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

	// Retrying archives may have part rows before objects exist, so scanning them
	// could tombstone recoverable work.
	retryVOD := "vod-retrying"
	retrying, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "scan-retrying", Filename: "scan-retrying", DisplayName: "b-scan",
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		BroadcasterID: "b-scan", RecordingType: repository.RecordingTypeVideo,
		Source: repository.VideoSourceVOD, TwitchVideoID: &retryVOD,
	})
	if err != nil {
		t.Fatalf("CreateVideo scan-retrying: %v", err)
	}
	if _, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{
		VideoID: retrying.ID, PartIndex: 1, Filename: "scan-retrying-part01.mp4",
		Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4,
	}); err != nil {
		t.Fatalf("CreateVideoPart scan-retrying: %v", err)
	}
	if err := repo.MarkArchiveFailedForRetry(ctx, retrying.ID, "upload blipped", repository.CompletionKindComplete, false, time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatalf("MarkArchiveFailedForRetry: %v", err)
	}
	if rows, err := repo.ListVideosForStorageScan(ctx, batchPage(t, 0, 100)); err != nil || slices.Contains(scanFilenames(rows), "scan-retrying") {
		t.Fatalf("scan candidates = %v, %v; want the retrying archive excluded", scanFilenames(rows), err)
	}
	if _, err := repo.GetVideoForStorageScan(ctx, retrying.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("retrying archive as a scan candidate err = %v, want ErrNotFound", err)
	}
	if changed, err := repo.TombstoneMissingVideo(ctx, retrying.ID); err != nil || changed {
		t.Fatalf("tombstone of a retrying archive = %v, %v; want refused", changed, err)
	}
	// Attachment must see media owners that the reconciliation query excludes.
	witnesses, err := repo.ListVideosForStorageWitness(ctx, batchSize(t, 100))
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, scanFilenames(witnesses), []string{
		"scan-done", "scan-legacy", "scan-failed-salvage", "scan-pending",
		"scan-queued", "scan-gone", "scan-delivering", "scan-retrying",
	})
	if err := repo.SoftDeleteVideo(ctx, gone.ID, repository.DeletionKindManual); err != nil {
		t.Fatal(err)
	}
	witnesses, err = repo.ListVideosForStorageWitness(ctx, batchSize(t, 100))
	if err != nil || slices.Contains(scanFilenames(witnesses), "scan-gone") {
		t.Fatalf("permanently removed media still blocks attachment: %+v, %v", witnesses, err)
	}
	if witnesses, err := repo.ListVideosForStorageWitness(ctx, batchSize(t, 1)); err != nil || len(witnesses) != 1 || witnesses[0].VideoID != doneParts.ID {
		t.Fatalf("bounded witness sample = %+v, %v", witnesses, err)
	}
	if _, err := repo.ListVideosForStorageWitness(ctx, repository.BatchSize{}); err == nil {
		t.Fatal("accepted uninitialized witness size")
	}
	if _, err := repo.ListVideosForStorageScan(ctx, repository.BatchPage{}); err == nil {
		t.Fatal("accepted uninitialized scan page")
	}
	// Valid value objects preserve bounded SQL pagination.
	for _, limit := range []int{1, repository.MaxBatchSize} {
		got, err := repo.ListVideosForStorageWitness(ctx, batchSize(t, limit))
		if err != nil || len(got) != min(limit, len(witnesses)) {
			t.Fatalf("witness sample limit %d = %+v, %v", limit, got, err)
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
		page, err := repo.ListVideosForStorageScan(ctx, batchPage(t, cursor, 1))
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
	for _, limit := range []int{1, repository.MaxBatchSize} {
		got, err := repo.ListVideosForStorageScan(ctx, batchPage(t, 0, limit))
		if err != nil || len(got) != min(limit, len(rows)) {
			t.Fatalf("scan page limit %d = %+v, %v", limit, got, err)
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

// testMissingTombstoneRestoreAndPermanentRemoval pins the reversible tombstone:
// only the missing kind restores, a queued manual delete wins over a restore,
// and removing a missing tombstone permanently flips it to manual and clears
// the poster the purge deleted.
func testMissingTombstoneRestoreAndPermanentRemoval(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u-restore", "b-restore")

	mk := func(jobID string) *repository.Video {
		t.Helper()
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: "b-restore",
			Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
			BroadcasterID: "b-restore", RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("CreateVideo %s: %v", jobID, err)
		}
		if _, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{
			VideoID: v.ID, PartIndex: 1, Filename: jobID + "-part01.mp4",
			Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4,
		}); err != nil {
			t.Fatalf("CreateVideoPart %s: %v", jobID, err)
		}
		poster := "thumbnails/" + jobID + "-part01.jpg"
		if err := repo.MarkVideoDone(ctx, v.ID, 60, 1024, &poster, repository.CompletionKindComplete, false); err != nil {
			t.Fatalf("MarkVideoDone %s: %v", jobID, err)
		}
		return v
	}
	missing := mk("restore-missing")
	queued := mk("restore-queued")
	live := mk("restore-live")
	purged := mk("restore-purged")
	if err := repo.SoftDeleteVideo(ctx, purged.ID, repository.DeletionKindRetention); err != nil {
		t.Fatal(err)
	}
	for _, v := range []*repository.Video{missing, queued} {
		if changed, err := repo.TombstoneMissingVideo(ctx, v.ID); err != nil || !changed {
			t.Fatalf("tombstone %s = %v, %v", v.JobID, changed, err)
		}
	}

	rows, err := repo.ListMissingTombstones(ctx, batchPage(t, 0, 100))
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, scanFilenames(rows), []string{"restore-missing", "restore-queued"})
	if _, err := repo.ListMissingTombstones(ctx, repository.BatchPage{}); err == nil {
		t.Fatal("accepted uninitialized tombstone page")
	}
	for _, limit := range []int{1, repository.MaxBatchSize} {
		got, err := repo.ListMissingTombstones(ctx, batchPage(t, 0, limit))
		if err != nil || len(got) != min(limit, len(rows)) {
			t.Fatalf("tombstone page limit %d = %+v, %v", limit, got, err)
		}
	}
	if row, err := repo.GetMissingTombstone(ctx, missing.ID); err != nil || row.VideoID != missing.ID || row.Status != repository.VideoStatusDone {
		t.Fatalf("GetMissingTombstone = %+v, %v", row, err)
	}
	for _, id := range []int64{0, -1, live.ID, purged.ID, purged.ID + 1000} {
		if _, err := repo.GetMissingTombstone(ctx, id); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("GetMissingTombstone(%d) err = %v, want ErrNotFound", id, err)
		}
		if err := repo.RestoreMissingVideo(ctx, id); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("RestoreMissingVideo(%d) err = %v, want ErrNotFound", id, err)
		}
	}

	// A queued manual delete prevents restoration of a missing-media tombstone.
	if _, err := repo.RequestVideoDelete(ctx, queued.ID); err != nil {
		t.Fatalf("RequestVideoDelete on a missing tombstone: %v", err)
	}
	pending, err := repo.ListVideosPendingManualDelete(ctx, batchPage(t, 0, 10))
	if err != nil || len(pending) != 1 || pending[0].ID != queued.ID {
		t.Fatalf("pending manual deletes = %+v, %v; want the queued tombstone", pending, err)
	}
	if err := repo.RestoreMissingVideo(ctx, queued.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("restore of a queued tombstone err = %v, want ErrNotFound", err)
	}
	rows, err = repo.ListMissingTombstones(ctx, batchPage(t, 0, 100))
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, scanFilenames(rows), []string{"restore-missing"})
	if err := repo.SoftDeleteVideo(ctx, queued.ID, repository.DeletionKindManual); err != nil {
		t.Fatalf("SoftDeleteVideo on a queued missing tombstone: %v", err)
	}
	got, err := repo.GetVideo(ctx, queued.ID)
	if err != nil || got.DeletedAt == nil || got.DeletionKind == nil || *got.DeletionKind != repository.DeletionKindManual || got.Thumbnail != nil {
		t.Fatalf("permanently removed tombstone = %+v, %v; want manual kind and no poster", got, err)
	}
	if _, err := repo.RequestVideoDelete(ctx, queued.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("RequestVideoDelete on a manual tombstone err = %v, want ErrNotFound", err)
	}
	if _, err := repo.RequestVideoDelete(ctx, purged.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("RequestVideoDelete on a retention tombstone err = %v, want ErrNotFound", err)
	}

	if err := repo.RestoreMissingVideo(ctx, missing.ID); err != nil {
		t.Fatalf("RestoreMissingVideo: %v", err)
	}
	got, err = repo.GetVideo(ctx, missing.ID)
	if err != nil || got.DeletedAt != nil || got.DeletionKind != nil || got.Thumbnail == nil {
		t.Fatalf("restored video = %+v, %v; want live with its poster", got, err)
	}
	parts, err := repo.ListVideoParts(ctx, missing.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("restored parts = %+v, %v", parts, err)
	}
	if err := repo.RestoreMissingVideo(ctx, missing.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("second restore err = %v, want ErrNotFound", err)
	}
	if changed, err := repo.TombstoneMissingVideo(ctx, missing.ID); err != nil || !changed {
		t.Fatalf("re-tombstone after restore = %v, %v", changed, err)
	}

	// One open row per VOD: a tombstoned archive whose VOD was archived again
	// cannot come back until one of the two is gone.
	vod := "vod-77"
	mkArchive := func(jobID string) *repository.Video {
		t.Helper()
		v, err := repo.CreateVideo(ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: "b-restore",
			Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
			BroadcasterID: "b-restore", RecordingType: repository.RecordingTypeVideo,
			Source: repository.VideoSourceVOD, TwitchVideoID: &vod,
		})
		if err != nil {
			t.Fatalf("CreateVideo %s: %v", jobID, err)
		}
		if err := repo.MarkVideoDone(ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
			t.Fatalf("MarkVideoDone %s: %v", jobID, err)
		}
		return v
	}
	first := mkArchive("restore-vod-first")
	if changed, err := repo.TombstoneMissingVideo(ctx, first.ID); err != nil || !changed {
		t.Fatalf("tombstone first archive = %v, %v", changed, err)
	}
	second := mkArchive("restore-vod-second")
	if err := repo.RestoreMissingVideo(ctx, first.ID); !errors.Is(err, repository.ErrDuplicate) {
		t.Fatalf("restore beside an open re-archive err = %v, want ErrDuplicate", err)
	}
	if got, err := repo.GetVideo(ctx, first.ID); err != nil || got.DeletedAt == nil {
		t.Fatalf("refused restore changed the tombstone: %+v, %v", got, err)
	}
	if err := repo.SoftDeleteVideo(ctx, second.ID, repository.DeletionKindManual); err != nil {
		t.Fatal(err)
	}
	if err := repo.RestoreMissingVideo(ctx, first.ID); err != nil {
		t.Fatalf("restore once the re-archive is gone: %v", err)
	}
}
