package retention

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newTestRepo(t *testing.T) *sqliteadapter.SQLiteAdapter {
	t.Helper()
	return sqliteadapter.New(testdb.NewSQLiteDB(t))
}

func newLocalStore(t *testing.T) *storage.LocalStorage {
	t.Helper()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	return store
}

// TestExpiredVideoIDs is the pure eligibility decision and the heart of the
// matrix: the per-recording retention-window comparison against an injected
// clock and the corrupt-row collection guards.
func TestExpiredVideoIDs(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { x := base.Add(d); return &x }
	hours := func(h int64) *int64 { return &h }
	vid := func(id int64, b string, d *time.Time, h *int64) repository.RetentionVideo {
		return repository.RetentionVideo{VideoID: id, BroadcasterID: b, DownloadedAt: d, RetentionWindowHours: h}
	}

	cases := []struct {
		name    string
		videos  []repository.RetentionVideo
		now     time.Time
		want    []int64
		wantErr bool
	}{
		{
			name:   "inside window is kept",
			videos: []repository.RetentionVideo{vid(1, "b", at(0), hours(24))},
			now:    base.Add(23 * time.Hour),
			want:   nil,
		},
		{
			name:   "exactly at the boundary is kept",
			videos: []repository.RetentionVideo{vid(1, "b", at(0), hours(24))},
			now:    base.Add(24 * time.Hour),
			want:   nil,
		},
		{
			name:   "one second past the boundary is deleted",
			videos: []repository.RetentionVideo{vid(1, "b", at(0), hours(24))},
			now:    base.Add(24*time.Hour + time.Second),
			want:   []int64{1},
		},
		{
			name:   "same broadcaster keeps per-recording windows",
			videos: []repository.RetentionVideo{vid(1, "b", at(0), hours(12)), vid(2, "b", at(0), hours(48))},
			now:    base.Add(13 * time.Hour),
			want:   []int64{1},
		},
		{
			name: "selects expired across broadcasters, keeps the rest",
			videos: []repository.RetentionVideo{
				vid(1, "b1", at(0), hours(1)),             // 2h old, 1h window  -> expired
				vid(2, "b2", at(0), hours(100)),           // 2h old, 100h window -> kept
				vid(3, "b1", at(-50*time.Hour), hours(1)), // 52h old, 1h window -> expired
			},
			now:  base.Add(2 * time.Hour),
			want: []int64{1, 3},
		},
		{
			name:    "zero window fails loud",
			videos:  []repository.RetentionVideo{vid(1, "b", at(0), hours(0))},
			now:     base,
			wantErr: true,
		},
		{
			name:    "nil window fails loud",
			videos:  []repository.RetentionVideo{vid(1, "b", at(0), nil)},
			now:     base,
			wantErr: true,
		},
		{
			name:    "nil completion fails loud",
			videos:  []repository.RetentionVideo{vid(8, "b", nil, hours(24))},
			now:     base,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := expiredVideoIDs(tc.videos, tc.now)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got ids=%v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExpiredVideoIDs_CollectsEligibilityErrorsAndContinues(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { x := base.Add(d); return &x }
	goodWindow := int64(1)
	zeroWindow := int64(0)
	overflowWindow := repository.MaxRetentionWindowHours + 1

	got, err := expiredVideoIDs([]repository.RetentionVideo{
		{VideoID: 1, BroadcasterID: "good", DownloadedAt: at(-2 * time.Hour), RetentionWindowHours: &goodWindow},
		{VideoID: 2, BroadcasterID: "good", DownloadedAt: nil, RetentionWindowHours: &goodWindow},
		{VideoID: 3, BroadcasterID: "bad-zero", DownloadedAt: at(-2 * time.Hour), RetentionWindowHours: &zeroWindow},
		{VideoID: 4, BroadcasterID: "bad-nil", DownloadedAt: at(-2 * time.Hour), RetentionWindowHours: nil},
		{VideoID: 5, BroadcasterID: "bad-overflow", DownloadedAt: at(-2 * time.Hour), RetentionWindowHours: &overflowWindow},
	}, base)

	if err == nil {
		t.Fatalf("want joined eligibility error, got nil")
	}
	for _, want := range []string{"video 2", "video 3", "video 4", "video 5"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
	if !reflect.DeepEqual(got, []int64{1}) {
		t.Fatalf("expired ids = %v, want [1] despite bad unrelated rows", got)
	}
}

func seedChannelUser(t *testing.T, ctx context.Context, repo repository.Repository, userID, broadcasterID string) {
	t.Helper()
	if _, err := repo.UpsertUser(ctx, &repository.User{ID: userID, Login: userID, DisplayName: userID, Role: "viewer"}); err != nil {
		t.Fatalf("seed user %s: %v", userID, err)
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: broadcasterID, BroadcasterLogin: broadcasterID, BroadcasterName: broadcasterID}); err != nil {
		t.Fatalf("seed channel %s: %v", broadcasterID, err)
	}
}

func seedSchedule(t *testing.T, ctx context.Context, repo repository.Repository, userID, broadcasterID string, deleteRediff bool, hours *int64, disabled bool) {
	t.Helper()
	if _, err := repo.CreateSchedule(ctx, &repository.ScheduleInput{
		BroadcasterID:    broadcasterID,
		RequestedBy:      userID,
		Quality:          "HIGH",
		IsDeleteRediff:   deleteRediff,
		TimeBeforeDelete: hours,
		IsDisabled:       disabled,
	}); err != nil {
		t.Fatalf("seed schedule (%s/%s): %v", broadcasterID, userID, err)
	}
}

func seedDoneVideo(t *testing.T, ctx context.Context, repo repository.Repository, jobID, filename, broadcasterID string) *repository.Video {
	t.Helper()
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: jobID, Filename: filename, DisplayName: broadcasterID, Status: "PENDING",
		Quality: "HIGH", BroadcasterID: broadcasterID, RecordingType: repository.RecordingTypeVideo,
		RetentionWindowHours: ptrInt64(1),
	})
	if err != nil {
		t.Fatalf("create video %s: %v", filename, err)
	}
	if err := repo.MarkVideoDone(ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("mark video %s done: %v", filename, err)
	}
	return v
}

func seedFailedVideo(t *testing.T, ctx context.Context, repo repository.Repository, jobID, filename, broadcasterID, completionKind string) *repository.Video {
	t.Helper()
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: jobID, Filename: filename, DisplayName: broadcasterID, Status: "PENDING",
		Quality: "HIGH", BroadcasterID: broadcasterID, RecordingType: repository.RecordingTypeVideo,
		RetentionWindowHours: ptrInt64(1),
	})
	if err != nil {
		t.Fatalf("create video %s: %v", filename, err)
	}
	if err := repo.MarkVideoFailed(ctx, v.ID, "synthetic failure", completionKind, true); err != nil {
		t.Fatalf("mark video %s failed: %v", filename, err)
	}
	return v
}

func ptrInt64(v int64) *int64 { return &v }

func seedDoneVideoNoRetention(t *testing.T, ctx context.Context, repo repository.Repository, jobID, filename, broadcasterID string) *repository.Video {
	t.Helper()
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: jobID, Filename: filename, DisplayName: broadcasterID, Status: "PENDING",
		Quality: "HIGH", BroadcasterID: broadcasterID, RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("create video %s: %v", filename, err)
	}
	if err := repo.MarkVideoDone(ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("mark video %s done: %v", filename, err)
	}
	return v
}

// TestRetentionQueries pins the query-side retention filter:
// ListFinishedVideosForRetention returns visible terminal rows that can own
// reclaimable objects only when the recording itself has a snapshotted
// retention window.
func TestRetentionQueries(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)

	// b-del: retained recordings plus terminal shapes that should be filtered.
	seedChannelUser(t, ctx, repo, "u-1", "b-del")
	seedSchedule(t, ctx, repo, "u-1", "b-del", true, ptrInt64(24), false)
	vDel := seedDoneVideo(t, ctx, repo, "job-del", "rec-del", "b-del")
	vPartial := seedFailedVideo(t, ctx, repo, "job-partial", "rec-partial", "b-del", repository.CompletionKindPartial)
	vCancelled := seedFailedVideo(t, ctx, repo, "job-cancelled", "rec-cancelled", "b-del", repository.CompletionKindCancelled)
	vFailedNoSalvage := seedFailedVideo(t, ctx, repo, "job-failed-nosalvage", "rec-failed-nosalvage", "b-del", repository.CompletionKindComplete)

	// b-min: same broadcaster shape, but retention now comes from the video row
	// rather than live schedule rows.
	seedChannelUser(t, ctx, repo, "u-1", "b-min")
	seedChannelUser(t, ctx, repo, "u-2", "b-min")
	seedSchedule(t, ctx, repo, "u-1", "b-min", true, ptrInt64(48), false)
	seedSchedule(t, ctx, repo, "u-2", "b-min", true, ptrInt64(12), false)
	vMin := seedDoneVideo(t, ctx, repo, "job-min", "rec-min", "b-min")

	// b-keep: same broadcaster has a delete schedule, but this video has no
	// snapshotted retention policy (manual/unrelated recording) and must be
	// excluded.
	seedChannelUser(t, ctx, repo, "u-1", "b-keep")
	seedSchedule(t, ctx, repo, "u-1", "b-keep", true, ptrInt64(1), false)
	vKeep := seedDoneVideoNoRetention(t, ctx, repo, "job-keep", "rec-keep", "b-keep")

	// b-disabled: disabled delete schedule and no video retention snapshot.
	seedChannelUser(t, ctx, repo, "u-1", "b-disabled")
	seedSchedule(t, ctx, repo, "u-1", "b-disabled", true, ptrInt64(24), true)
	vDisabled := seedDoneVideoNoRetention(t, ctx, repo, "job-disabled", "rec-disabled", "b-disabled")

	// A PENDING (unfinished) and a soft-deleted video: neither is a video
	// candidate, even with a retention snapshot.
	if _, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-pending", Filename: "rec-pending", DisplayName: "b-del", Status: "PENDING",
		Quality: "HIGH", BroadcasterID: "b-del", RecordingType: repository.RecordingTypeVideo,
		RetentionWindowHours: ptrInt64(1),
	}); err != nil {
		t.Fatalf("create pending video: %v", err)
	}
	vGone := seedDoneVideo(t, ctx, repo, "job-gone", "rec-gone", "b-del")
	if err := repo.SoftDeleteVideo(ctx, vGone.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	vNotDue, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-not-due", Filename: "rec-not-due", DisplayName: "b-del", Status: "PENDING",
		Quality: "HIGH", BroadcasterID: "b-del", RecordingType: repository.RecordingTypeVideo,
		RetentionWindowHours: ptrInt64(24),
	})
	if err != nil {
		t.Fatalf("create not-due video: %v", err)
	}
	if err := repo.MarkVideoDone(ctx, vNotDue.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("mark not-due video done: %v", err)
	}

	// Videos: every due DONE visible recording with a retention window plus due
	// FAILED partial/cancelled rows. FAILED rows without salvage and no-retention
	// manual/unrelated recordings stay out.
	videos, err := repo.ListFinishedVideosForRetention(ctx, time.Now().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("ListFinishedVideosForRetention: %v", err)
	}
	gotVids := make(map[int64]bool, len(videos))
	for _, v := range videos {
		if v.DownloadedAt == nil {
			t.Fatalf("video %d has nil downloaded_at; query filters it NOT NULL", v.VideoID)
		}
		if v.RetentionWindowHours == nil {
			t.Fatalf("video %d has nil retention_window_hours; query filters it NOT NULL", v.VideoID)
		}
		gotVids[v.VideoID] = true
	}
	wantVids := map[int64]bool{vDel.ID: true, vMin.ID: true, vPartial.ID: true, vCancelled.ID: true}
	if !reflect.DeepEqual(gotVids, wantVids) {
		t.Fatalf("retention videos = %v, want %v (no-retention ids %d/%d, failed-no-salvage id %d, and not-due id %d excluded)",
			gotVids, wantVids, vKeep.ID, vDisabled.ID, vFailedNoSalvage.ID, vNotDue.ID)
	}
}

// objectsFor lists every storage path a two-part recording named "rec"
// owns in the deletion tests below: part files, per-part thumbnail + strip,
// the video-level thumbnail, the audio waveform artifact, and two live
// snapshots.
func objectsFor() []string {
	return []string{
		"videos/rec-part01.mp4",
		"videos/rec-part02.mp4",
		"thumbnails/rec-part01.jpg",
		"thumbnails/rec-part01-strip.jpg",
		"thumbnails/rec-part02.jpg",
		"thumbnails/rec-part02-strip.jpg",
		"thumbnails/rec-snap00.jpg",
		"thumbnails/rec-snap01.jpg",
		"thumbnails/rec-waveform.json",
		"videos/rec-playback.mp4",
	}
}

// seedRecordingWithObjects builds a finished two-part recording with a
// snapshotted 1h retention window and writes all of its objects into store.
// Returns the video.
func seedRecordingWithObjects(t *testing.T, ctx context.Context, repo repository.Repository, store storage.Storage) *repository.Video {
	t.Helper()
	seedChannelUser(t, ctx, repo, "u-1", "b-1")
	seedSchedule(t, ctx, repo, "u-1", "b-1", true, ptrInt64(1), false)

	v, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-1", Filename: "rec", DisplayName: "b-1", Status: "PENDING",
		Quality: "HIGH", BroadcasterID: "b-1", RecordingType: repository.RecordingTypeVideo,
		RetentionWindowHours: ptrInt64(1),
	})
	if err != nil {
		t.Fatalf("create video: %v", err)
	}
	for i, name := range []string{"rec-part01.mp4", "rec-part02.mp4"} {
		if _, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{
			VideoID: v.ID, PartIndex: int32(i + 1), Filename: name,
			Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4,
		}); err != nil {
			t.Fatalf("create part %s: %v", name, err)
		}
	}
	thumb := "thumbnails/rec-part01.jpg"
	if err := repo.MarkVideoDone(ctx, v.ID, 60, 1024, &thumb, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("mark done: %v", err)
	}
	playbackName := "rec-playback.mp4"
	playbackMime := "video/mp4"
	dur, size := 60.0, int64(2048)
	at := time.Now().UTC()
	if _, err := repo.UpsertVideoPlaybackAsset(ctx, &repository.VideoPlaybackAssetInput{
		VideoID:         v.ID,
		Status:          repository.PlaybackAssetStatusReady,
		Filename:        &playbackName,
		MimeType:        &playbackMime,
		DurationSeconds: &dur,
		SizeBytes:       &size,
		GeneratedAt:     &at,
		LastAccessedAt:  &at,
	}); err != nil {
		t.Fatalf("seed playback asset: %v", err)
	}
	for _, p := range objectsFor() {
		if err := store.Save(ctx, p, strings.NewReader("data")); err != nil {
			t.Fatalf("seed object %s: %v", p, err)
		}
	}
	return v
}

func assertObjectsGone(t *testing.T, ctx context.Context, store storage.Storage) {
	t.Helper()
	assertPathsGone(t, ctx, store, objectsFor())
}

func assertObjectsExist(t *testing.T, ctx context.Context, store storage.Storage) {
	t.Helper()
	for _, p := range objectsFor() {
		ok, err := store.Exists(ctx, p)
		if err != nil {
			t.Fatalf("exists %s: %v", p, err)
		}
		if !ok {
			t.Fatalf("object %s missing before retention should purge it", p)
		}
	}
}

func assertPathsGone(t *testing.T, ctx context.Context, store storage.Storage, paths []string) {
	t.Helper()
	for _, p := range paths {
		ok, err := store.Exists(ctx, p)
		if err != nil {
			t.Fatalf("exists %s: %v", p, err)
		}
		if ok {
			t.Fatalf("object %s still present after sweep", p)
		}
	}
}

func TestSweep_SkipsRecordingWithUnfrozenPendingWebhook(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	store := newLocalStore(t)
	svc := New(repo, store, discardLog())

	v := seedRecordingWithObjects(t, ctx, repo, store)
	delivery, err := repo.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
		MessageID:     "msg-retention-block",
		DedupeKey:     "dedupe-retention-block",
		Event:         "recording.completed",
		VideoID:       v.ID,
		NextAttemptAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateRecordingWebhookDelivery: %v", err)
	}
	now := time.Now().Add(48 * time.Hour)

	deleted, err := svc.Sweep(ctx, now)
	if err != nil {
		t.Fatalf("Sweep with unfrozen webhook: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("deleted = %d, want 0 while pending webhook parts are not frozen", deleted)
	}
	assertObjectsExist(t, ctx, store)
	got, err := repo.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.DeletedAt != nil {
		t.Fatalf("video was tombstoned despite unfrozen pending webhook")
	}

	if err := repo.SetRecordingWebhookDeliveryFrozenParts(ctx, delivery.ID, "[]"); err != nil {
		t.Fatalf("SetRecordingWebhookDeliveryFrozenParts: %v", err)
	}
	deleted, err = svc.Sweep(ctx, now)
	if err != nil {
		t.Fatalf("Sweep after freezing webhook parts: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1 after webhook parts are frozen", deleted)
	}
	assertObjectsGone(t, ctx, store)
}

// TestSweep_DeletesExpiredRecording covers the happy path end to end: every
// object removed, part rows gone, video tombstoned — then an idempotent
// re-run that finds nothing (the tombstone drops it from the candidate set).
func TestSweep_DeletesExpiredRecording(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	store := newLocalStore(t)
	svc := New(repo, store, discardLog())

	v := seedRecordingWithObjects(t, ctx, repo, store)
	now := time.Now().Add(48 * time.Hour) // well past the 1h window

	deleted, err := svc.Sweep(ctx, now)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}

	assertObjectsGone(t, ctx, store)

	parts, err := repo.ListVideoParts(ctx, v.ID)
	if err != nil {
		t.Fatalf("ListVideoParts: %v", err)
	}
	if len(parts) != 0 {
		t.Fatalf("part rows = %d, want 0", len(parts))
	}
	if _, err := repo.GetVideoPlaybackAsset(ctx, v.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("playback asset row still present after sweep: err = %v", err)
	}
	got, err := repo.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.DeletedAt == nil {
		t.Fatalf("video not tombstoned; deleted_at is nil")
	}

	// Idempotent: the tombstone removes it from the candidate set, so a
	// second sweep is a no-op rather than an error.
	deleted, err = svc.Sweep(ctx, now)
	if err != nil {
		t.Fatalf("second Sweep: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("second sweep deleted = %d, want 0", deleted)
	}
}

func TestSweep_DeletesLegacySingleFileRecording(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	store := newLocalStore(t)
	svc := New(repo, store, discardLog())

	seedChannelUser(t, ctx, repo, "u-1", "b-legacy")
	seedSchedule(t, ctx, repo, "u-1", "b-legacy", true, ptrInt64(1), false)
	v := seedDoneVideo(t, ctx, repo, "job-legacy", "legacy-rec", "b-legacy")
	if err := repo.SetVideoThumbnail(ctx, v.ID, "thumbnails/legacy-rec.jpg"); err != nil {
		t.Fatalf("set legacy thumbnail: %v", err)
	}

	paths := []string{
		"videos/legacy-rec.mp4",
		"thumbnails/legacy-rec.jpg",
		"thumbnails/legacy-rec-snap00.jpg",
	}
	for _, p := range paths {
		if err := store.Save(ctx, p, strings.NewReader("data")); err != nil {
			t.Fatalf("seed object %s: %v", p, err)
		}
	}

	deleted, err := svc.Sweep(ctx, time.Now().Add(48*time.Hour))
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	assertPathsGone(t, ctx, store, paths)

	parts, err := repo.ListVideoParts(ctx, v.ID)
	if err != nil {
		t.Fatalf("ListVideoParts: %v", err)
	}
	if len(parts) != 0 {
		t.Fatalf("legacy part rows = %d, want 0", len(parts))
	}
	got, err := repo.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.DeletedAt == nil {
		t.Fatalf("legacy video not tombstoned")
	}
}

func TestSweep_KeepsRecordingWithoutRetentionWindowOnDeleteScheduledBroadcaster(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	store := newLocalStore(t)
	svc := New(repo, store, discardLog())

	seedChannelUser(t, ctx, repo, "u-1", "b-mixed")
	seedSchedule(t, ctx, repo, "u-1", "b-mixed", true, ptrInt64(1), false)

	retained, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-retained", Filename: "retained-rec", DisplayName: "b-mixed", Status: "PENDING",
		Quality: "HIGH", BroadcasterID: "b-mixed", RecordingType: repository.RecordingTypeVideo,
		RetentionWindowHours: ptrInt64(1),
	})
	if err != nil {
		t.Fatalf("create retained video: %v", err)
	}
	if err := repo.MarkVideoDone(ctx, retained.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("mark retained done: %v", err)
	}
	manual := seedDoneVideoNoRetention(t, ctx, repo, "job-manual", "manual-rec", "b-mixed")

	for _, p := range []string{"videos/retained-rec.mp4", "videos/manual-rec.mp4"} {
		if err := store.Save(ctx, p, strings.NewReader("data")); err != nil {
			t.Fatalf("seed object %s: %v", p, err)
		}
	}

	deleted, err := svc.Sweep(ctx, time.Now().Add(48*time.Hour))
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (only the recording with a retention snapshot)", deleted)
	}

	gotRetained, err := repo.GetVideo(ctx, retained.ID)
	if err != nil {
		t.Fatalf("GetVideo retained: %v", err)
	}
	if gotRetained.DeletedAt == nil {
		t.Fatalf("retained recording not tombstoned")
	}
	gotManual, err := repo.GetVideo(ctx, manual.ID)
	if err != nil {
		t.Fatalf("GetVideo manual: %v", err)
	}
	if gotManual.DeletedAt != nil {
		t.Fatalf("manual/no-retention recording was tombstoned")
	}
	if ok, _ := store.Exists(ctx, "videos/retained-rec.mp4"); ok {
		t.Fatalf("retained object still present")
	}
	if ok, _ := store.Exists(ctx, "videos/manual-rec.mp4"); !ok {
		t.Fatalf("manual/no-retention object was deleted")
	}
}

func TestSweep_DeletesFailedPartialRecording(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	store := newLocalStore(t)
	svc := New(repo, store, discardLog())

	seedChannelUser(t, ctx, repo, "u-1", "b-failed")
	seedSchedule(t, ctx, repo, "u-1", "b-failed", true, ptrInt64(1), false)
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-failed", Filename: "failed-rec", DisplayName: "b-failed", Status: "PENDING",
		Quality: "HIGH", BroadcasterID: "b-failed", RecordingType: repository.RecordingTypeVideo,
		RetentionWindowHours: ptrInt64(1),
	})
	if err != nil {
		t.Fatalf("create failed video: %v", err)
	}
	part, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{
		VideoID: v.ID, PartIndex: 1, Filename: "failed-rec-part01.mp4",
		Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4,
		StartMediaSeq: 100,
	})
	if err != nil {
		t.Fatalf("create failed part: %v", err)
	}
	thumb := "thumbnails/failed-rec-part01.jpg"
	if err := repo.FinalizeVideoPart(ctx, &repository.VideoPartFinalize{
		ID:              part.ID,
		DurationSeconds: 60,
		SizeBytes:       2048,
		Thumbnail:       &thumb,
		EndMediaSeq:     130,
	}); err != nil {
		t.Fatalf("finalize failed part: %v", err)
	}
	if err := repo.MarkVideoFailed(ctx, v.ID, "synthetic failure", repository.CompletionKindPartial, true); err != nil {
		t.Fatalf("mark failed partial: %v", err)
	}

	paths := []string{
		"videos/failed-rec-part01.mp4",
		"thumbnails/failed-rec-part01.jpg",
		"thumbnails/failed-rec-part01-strip.jpg",
		"thumbnails/failed-rec-snap00.jpg",
	}
	for _, p := range paths {
		if err := store.Save(ctx, p, strings.NewReader("data")); err != nil {
			t.Fatalf("seed object %s: %v", p, err)
		}
	}

	deleted, err := svc.Sweep(ctx, time.Now().Add(48*time.Hour))
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	assertPathsGone(t, ctx, store, paths)
	got, err := repo.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.DeletedAt == nil {
		t.Fatalf("failed partial video not tombstoned")
	}
}

// TestSweep_KeepsRecordingInsideWindow guards the negative path: a finished
// recording whose window hasn't elapsed is left fully intact.
func TestSweep_KeepsRecordingInsideWindow(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	store := newLocalStore(t)
	svc := New(repo, store, discardLog())

	v := seedRecordingWithObjects(t, ctx, repo, store)
	now := time.Now().Add(30 * time.Minute) // inside the 1h window

	deleted, err := svc.Sweep(ctx, now)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("deleted = %d, want 0", deleted)
	}
	got, err := repo.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.DeletedAt != nil {
		t.Fatalf("recording inside window was tombstoned")
	}
	if ok, _ := store.Exists(ctx, "videos/rec-part01.mp4"); !ok {
		t.Fatalf("recording inside window had its object deleted")
	}
}

// faultyStore injects a single Delete failure on a chosen path, then
// delegates — modelling a transient object-store error mid-purge.
type faultyStore struct {
	storage.Storage
	failOn string
	failed bool
}

func (f *faultyStore) Delete(ctx context.Context, path string) error {
	if !f.failed && path == f.failOn {
		f.failed = true
		return errors.New("injected delete failure")
	}
	return f.Storage.Delete(ctx, path)
}

// TestSweep_PartialFailureRecovers pins crash-safety in miniature: a storage
// failure mid-purge aborts before any DB write, so the recording stays
// selectable and untombstoned; once the store recovers, the next sweep
// converges — no orphaned row, no orphaned files.
func TestSweep_PartialFailureRecovers(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	base := newLocalStore(t)
	faulty := &faultyStore{Storage: base, failOn: "videos/rec-part02.mp4"}
	svc := New(repo, faulty, discardLog())

	v := seedRecordingWithObjects(t, ctx, repo, base)
	now := time.Now().Add(48 * time.Hour)

	if _, err := svc.Sweep(ctx, now); err == nil {
		t.Fatalf("Sweep: want error from injected failure, got nil")
	}

	// The DB must be untouched: purge failed before FinalizeRetentionDelete, so
	// the recording is still a live candidate.
	got, err := repo.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.DeletedAt != nil {
		t.Fatalf("video tombstoned despite a storage failure mid-purge")
	}
	parts, err := repo.ListVideoParts(ctx, v.ID)
	if err != nil {
		t.Fatalf("ListVideoParts: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("part rows = %d, want 2 (DB must be untouched on failure)", len(parts))
	}

	// faulty.failed is now set, so the retry's deletes all delegate to the
	// real store: the sweep converges.
	deleted, err := svc.Sweep(ctx, now)
	if err != nil {
		t.Fatalf("retry Sweep: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("retry deleted = %d, want 1", deleted)
	}
	assertObjectsGone(t, ctx, base)
	got, err = repo.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo after retry: %v", err)
	}
	if got.DeletedAt == nil {
		t.Fatalf("video not tombstoned after successful retry")
	}
}

// seedRecordingWithSnapshots builds a finished single-part recording with a
// snapshotted 1h retention window and writes its part objects plus a contiguous
// run of n live snapshots (snap00..snap0(n-1)). Paths are written as literals,
// independent of storagekeys, so the test stays an oracle for the keys rather
// than re-deriving them from the code under test. Returns the video.
func seedRecordingWithSnapshots(t *testing.T, ctx context.Context, repo repository.Repository, store storage.Storage, n int) *repository.Video {
	t.Helper()
	seedChannelUser(t, ctx, repo, "u-1", "b-1")
	seedSchedule(t, ctx, repo, "u-1", "b-1", true, ptrInt64(1), false)

	v, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-1", Filename: "rec", DisplayName: "b-1", Status: "PENDING",
		Quality: "HIGH", BroadcasterID: "b-1", RecordingType: repository.RecordingTypeVideo,
		RetentionWindowHours: ptrInt64(1),
	})
	if err != nil {
		t.Fatalf("create video: %v", err)
	}
	if _, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{
		VideoID: v.ID, PartIndex: 1, Filename: "rec-part01.mp4",
		Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4,
	}); err != nil {
		t.Fatalf("create part: %v", err)
	}
	thumb := "thumbnails/rec-part01.jpg"
	if err := repo.MarkVideoDone(ctx, v.ID, 60, 1024, &thumb, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("mark done: %v", err)
	}
	objs := []string{
		"videos/rec-part01.mp4",
		"thumbnails/rec-part01.jpg",
		"thumbnails/rec-part01-strip.jpg",
	}
	for i := range n {
		objs = append(objs, fmt.Sprintf("thumbnails/rec-snap%02d.jpg", i))
	}
	for _, p := range objs {
		if err := store.Save(ctx, p, strings.NewReader("data")); err != nil {
			t.Fatalf("seed object %s: %v", p, err)
		}
	}
	return v
}

// TestSweep_SnapshotPurgeFailureKeepsIndexZeroSentinel pins the highest-index-
// first deletion order in purgeSnapshots. A recording carries a contiguous run
// of live snapshots (snap00..snap03). A Delete failure mid-purge must leave a
// contiguous 0..k prefix — crucially including index 0, the probe's sentinel —
// so the next sweep re-discovers the recording instead of breaking on a hole at
// index 0 and stranding the tail forever.
//
// The injected failure hits snap01. Deleting highest-first (snap03, snap02,
// snap01, ...) aborts on snap01 before snap00 is reached, so the survivors are
// exactly {snap00, snap01}. Deleting lowest-first would remove snap00 first and
// orphan snap01..snap03 behind a hole — this test fails under that order, both
// on the surviving-sentinel assertion and on the convergent retry.
func TestSweep_SnapshotPurgeFailureKeepsIndexZeroSentinel(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	base := newLocalStore(t)
	faulty := &faultyStore{Storage: base, failOn: "thumbnails/rec-snap01.jpg"}
	svc := New(repo, faulty, discardLog())

	v := seedRecordingWithSnapshots(t, ctx, repo, base, 4)
	now := time.Now().Add(48 * time.Hour) // past the 1h window

	if _, err := svc.Sweep(ctx, now); err == nil {
		t.Fatalf("Sweep: want error from injected snapshot-delete failure, got nil")
	} else if !strings.Contains(err.Error(), "thumbnails/rec-snap01.jpg") {
		t.Fatalf("error %q does not name the failing snapshot", err)
	}

	// Survivors form a contiguous 0..1 prefix: index 0 (the sentinel the probe
	// keys on) and index 1 (the failed delete) remain; 2 and 3 are gone.
	for _, i := range []int{0, 1} {
		if ok, _ := base.Exists(ctx, fmt.Sprintf("thumbnails/rec-snap%02d.jpg", i)); !ok {
			t.Fatalf("snapshot index %d missing; survivors must be the contiguous 0..1 prefix", i)
		}
	}
	for _, i := range []int{2, 3} {
		if ok, _ := base.Exists(ctx, fmt.Sprintf("thumbnails/rec-snap%02d.jpg", i)); ok {
			t.Fatalf("snapshot index %d still present; highest-first deletion should have removed it", i)
		}
	}

	// DB untouched: the snapshot purge runs inside purgeObjects, before any DB
	// write, so the recording stays a live, untombstoned candidate.
	got, err := repo.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.DeletedAt != nil {
		t.Fatalf("video tombstoned despite a snapshot-purge failure mid-pass")
	}

	// Store recovered: the next sweep re-discovers the 0..1 prefix (it would be
	// invisible behind a hole at index 0 under lowest-first deletion) and
	// converges — every snapshot gone, video tombstoned.
	deleted, err := svc.Sweep(ctx, now)
	if err != nil {
		t.Fatalf("retry Sweep: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("retry deleted = %d, want 1", deleted)
	}
	for i := range 4 {
		if ok, _ := base.Exists(ctx, fmt.Sprintf("thumbnails/rec-snap%02d.jpg", i)); ok {
			t.Fatalf("snapshot index %d still present after convergent retry", i)
		}
	}
	got, err = repo.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo after retry: %v", err)
	}
	if got.DeletedAt == nil {
		t.Fatalf("video not tombstoned after convergent retry")
	}
}

// TestSweep_PurgesSnapshotsBeyondReaderCap pins the orphan fix: a recording can
// own more live snapshots than the video API's 500-frame reader cap (a long
// stream captures one every ~5 min), and retention must delete all of them.
// With 501 contiguous snapshots the sweep tombstones the video in the same pass,
// so anything left unpurged is stranded forever — the old maxSnapshots=500 probe
// left snap500 behind. Every snapshot, including index 500, must be gone.
func TestSweep_PurgesSnapshotsBeyondReaderCap(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	store := newLocalStore(t)
	svc := New(repo, store, discardLog())

	const n = 501 // one past the old 500 ceiling that used to strand the tail
	v := seedRecordingWithSnapshots(t, ctx, repo, store, n)

	deleted, err := svc.Sweep(ctx, time.Now().Add(48*time.Hour))
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	for i := range n {
		if ok, _ := store.Exists(ctx, fmt.Sprintf("thumbnails/rec-snap%02d.jpg", i)); ok {
			t.Fatalf("snapshot index %d still present; purge must not cap below the recording's snapshot count", i)
		}
	}
	got, err := repo.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.DeletedAt == nil {
		t.Fatalf("video not tombstoned after purging >500 snapshots")
	}
}

// seedFinishedSinglePart builds one finished single-part recording (DONE; once
// swept at now+48h it is well past its snapshotted 1h window) under the given
// broadcaster and filename, and writes its part object, thumbnail, strip, and
// one snapshot. Job ID and object paths are namespaced by filename so several
// can coexist. Returns the video.
func seedFinishedSinglePart(t *testing.T, ctx context.Context, repo repository.Repository, store storage.Storage, broadcasterID, filename string) *repository.Video {
	t.Helper()
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-" + filename, Filename: filename, DisplayName: broadcasterID, Status: "PENDING",
		Quality: "HIGH", BroadcasterID: broadcasterID, RecordingType: repository.RecordingTypeVideo,
		RetentionWindowHours: ptrInt64(1),
	})
	if err != nil {
		t.Fatalf("create video %s: %v", filename, err)
	}
	if _, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{
		VideoID: v.ID, PartIndex: 1, Filename: filename + "-part01.mp4",
		Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4,
	}); err != nil {
		t.Fatalf("create part %s: %v", filename, err)
	}
	thumb := "thumbnails/" + filename + "-part01.jpg"
	if err := repo.MarkVideoDone(ctx, v.ID, 60, 1024, &thumb, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("mark %s done: %v", filename, err)
	}
	for _, p := range []string{
		"videos/" + filename + "-part01.mp4",
		"thumbnails/" + filename + "-part01.jpg",
		"thumbnails/" + filename + "-part01-strip.jpg",
		"thumbnails/" + filename + "-snap00.jpg",
	} {
		if err := store.Save(ctx, p, strings.NewReader("data")); err != nil {
			t.Fatalf("seed object %s: %v", p, err)
		}
	}
	return v
}

// TestSweep_OneRecordingFailsOthersSucceedAndErrorAggregates pins Sweep's
// per-recording error aggregation across a multi-recording pass. Three expired
// recordings share one broadcaster; a single injected Delete failure hits only
// recb's part object. The sweep must isolate that failure: reca and recc are
// fully deleted, recb is left untouched (its purge aborts before any DB write,
// so it stays a live candidate), the returned count reflects only the two
// successes, and the joined error names the failed recording. A naive sweep
// that aborted on the first failure, or counted it as deleted, fails here.
//
// recb is always the one that fails regardless of iteration order: the
// faultyStore's single failure budget is keyed to a path only recb owns. So the
// assertions don't depend on the query's row order.
func TestSweep_OneRecordingFailsOthersSucceedAndErrorAggregates(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)
	base := newLocalStore(t)
	faulty := &faultyStore{Storage: base, failOn: "videos/recb-part01.mp4"}
	svc := New(repo, faulty, discardLog())

	seedChannelUser(t, ctx, repo, "u-1", "b-1")
	seedSchedule(t, ctx, repo, "u-1", "b-1", true, ptrInt64(1), false)
	a := seedFinishedSinglePart(t, ctx, repo, base, "b-1", "reca")
	b := seedFinishedSinglePart(t, ctx, repo, base, "b-1", "recb")
	c := seedFinishedSinglePart(t, ctx, repo, base, "b-1", "recc")
	now := time.Now().Add(48 * time.Hour)

	deleted, err := svc.Sweep(ctx, now)
	if err == nil {
		t.Fatalf("Sweep: want joined error from recb's failure, got nil")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("recording %d", b.ID)) {
		t.Fatalf("error %q does not name the failed recording %d", err, b.ID)
	}
	// Only the two clean recordings count; the failed one must not inflate it.
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2 (reca + recc; recb failed)", deleted)
	}

	// reca and recc: fully deleted — tombstoned and their objects gone.
	for _, v := range []*repository.Video{a, c} {
		got, err := repo.GetVideo(ctx, v.ID)
		if err != nil {
			t.Fatalf("GetVideo %d: %v", v.ID, err)
		}
		if got.DeletedAt == nil {
			t.Fatalf("recording %d not tombstoned despite a clean purge", v.ID)
		}
	}
	assertPathsGone(t, ctx, base, []string{"videos/reca-part01.mp4", "videos/recc-part01.mp4"})

	// recb: untouched — purge aborted before FinalizeRetentionDelete, so the
	// row and its parts survive for the next sweep to retry.
	gotB, err := repo.GetVideo(ctx, b.ID)
	if err != nil {
		t.Fatalf("GetVideo recb: %v", err)
	}
	if gotB.DeletedAt != nil {
		t.Fatalf("failed recording %d tombstoned; its DB writes must not have run", b.ID)
	}
	partsB, err := repo.ListVideoParts(ctx, b.ID)
	if err != nil {
		t.Fatalf("ListVideoParts recb: %v", err)
	}
	if len(partsB) != 1 {
		t.Fatalf("failed recording %d parts = %d, want 1 (DB untouched on failure)", b.ID, len(partsB))
	}

	// Store recovered: the next sweep finds only recb (reca/recc are tombstoned
	// out of the candidate set) and converges with no error.
	deleted, err = svc.Sweep(ctx, now)
	if err != nil {
		t.Fatalf("retry Sweep: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("retry deleted = %d, want 1 (recb)", deleted)
	}
	gotB, err = repo.GetVideo(ctx, b.ID)
	if err != nil {
		t.Fatalf("GetVideo recb after retry: %v", err)
	}
	if gotB.DeletedAt == nil {
		t.Fatalf("recb not tombstoned after convergent retry")
	}
}

// finalizeFailRepo fails the first FinalizeRetentionDelete, then delegates,
// modelling a crash/DB hiccup after the object purge but before the commit.
type finalizeFailRepo struct {
	repository.Repository
	failed bool
}

func (r *finalizeFailRepo) FinalizeRetentionDelete(ctx context.Context, videoID int64) error {
	if !r.failed {
		r.failed = true
		return errors.New("injected finalize failure")
	}
	return r.Repository.FinalizeRetentionDelete(ctx, videoID)
}

// TestSweep_FinalizeFailureConverges pins crash-safety at the DB-commit step:
// objects are purged before FinalizeRetentionDelete, so when that commit fails
// the recording is left untombstoned (still a candidate) with its objects
// already gone. The next sweep re-selects it, re-purges idempotently, and the
// now-succeeding commit converges — no orphaned row, no double-counting.
func TestSweep_FinalizeFailureConverges(t *testing.T) {
	ctx := context.Background()
	repo := &finalizeFailRepo{Repository: newTestRepo(t)}
	store := newLocalStore(t)
	svc := New(repo, store, discardLog())

	v := seedRecordingWithObjects(t, ctx, repo, store)
	now := time.Now().Add(48 * time.Hour)

	deleted, err := svc.Sweep(ctx, now)
	if err == nil {
		t.Fatalf("Sweep: want error from injected finalize failure, got nil")
	}
	if deleted != 0 {
		t.Fatalf("deleted = %d, want 0 (commit failed)", deleted)
	}
	// The object purge runs before the commit, so the bytes are already gone...
	assertObjectsGone(t, ctx, store)
	// ...but the row must be untouched: an atomic FinalizeRetentionDelete that
	// failed tombstones nothing and drops no parts, so it stays a candidate.
	got, err := repo.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo: %v", err)
	}
	if got.DeletedAt != nil {
		t.Fatalf("video tombstoned despite a failed finalize")
	}
	parts, err := repo.ListVideoParts(ctx, v.ID)
	if err != nil {
		t.Fatalf("ListVideoParts: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("part rows = %d, want 2 (commit failed, DB untouched)", len(parts))
	}

	// Next sweep: the re-purge is an idempotent no-op and the commit now lands.
	deleted, err = svc.Sweep(ctx, now)
	if err != nil {
		t.Fatalf("retry Sweep: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("retry deleted = %d, want 1", deleted)
	}
	got, err = repo.GetVideo(ctx, v.ID)
	if err != nil {
		t.Fatalf("GetVideo after retry: %v", err)
	}
	if got.DeletedAt == nil {
		t.Fatalf("video not tombstoned after convergent retry")
	}
}
