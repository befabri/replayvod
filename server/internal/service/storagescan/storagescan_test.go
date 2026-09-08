package storagescan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/contracttest"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fixture struct {
	ctx   context.Context
	repo  repository.Repository
	store *storage.LocalStorage
	svc   *Service
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	// Deleted media leaves the videos directory behind; only an unmounted
	// volume takes it away.
	if err := os.MkdirAll(filepath.Join(store.Root, "videos"), 0o755); err != nil {
		t.Fatalf("create videos dir: %v", err)
	}
	contracttest.SeedUserChannel(t, ctx, repo, "u-scan", "b-scan")
	return fixture{
		ctx:   ctx,
		repo:  repo,
		store: store,
		svc:   New(repo, store, discardLog()),
	}
}

// seed creates a finished recording named jobID with parts part rows. Media
// files are written only for the part indexes listed in present, so a caller
// shapes a fully present, partial, or missing recording. The poster and the
// first part's strip are always written: a tombstone must keep the poster and
// preserve the strip.
func (f fixture) seed(t *testing.T, jobID string, parts int, present ...int) *repository.Video {
	t.Helper()
	v, err := f.repo.CreateVideo(f.ctx, &repository.VideoInput{
		JobID: jobID, Filename: jobID, DisplayName: "b-scan",
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		BroadcasterID: "b-scan", RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("CreateVideo %s: %v", jobID, err)
	}
	for i := 1; i <= parts; i++ {
		name := fmt.Sprintf("%s-part%02d.mp4", jobID, i)
		if _, err := f.repo.CreateVideoPart(f.ctx, &repository.VideoPartInput{
			VideoID: v.ID, PartIndex: int32(i), Filename: name,
			Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4,
		}); err != nil {
			t.Fatalf("CreateVideoPart %s: %v", name, err)
		}
	}
	poster := fmt.Sprintf("thumbnails/%s-part01.jpg", jobID)
	if err := f.repo.MarkVideoDone(f.ctx, v.ID, 60, 1024, &poster, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("MarkVideoDone %s: %v", jobID, err)
	}
	for _, i := range present {
		f.save(t, fmt.Sprintf("videos/%s-part%02d.mp4", jobID, i))
	}
	f.save(t, poster)
	f.save(t, fmt.Sprintf("thumbnails/%s-part01-strip.jpg", jobID))
	return v
}

func (f fixture) save(t *testing.T, path string) {
	t.Helper()
	if err := f.store.Save(f.ctx, path, strings.NewReader("data")); err != nil {
		t.Fatalf("save %s: %v", path, err)
	}
}

func (f fixture) exists(t *testing.T, path string) bool {
	t.Helper()
	ok, err := f.store.Exists(f.ctx, path)
	if err != nil {
		t.Fatalf("exists %s: %v", path, err)
	}
	return ok
}

func (f fixture) assertLive(t *testing.T, id int64) {
	t.Helper()
	v, err := f.repo.GetVideo(f.ctx, id)
	if err != nil {
		t.Fatalf("GetVideo %d: %v", id, err)
	}
	if v.DeletedAt != nil {
		t.Fatalf("recording %d was tombstoned (kind %v), want live", id, v.DeletionKind)
	}
}

// assertPreviewsKept checks that a missing-media tombstone left the
// poster and the rest of the preview assets referenced by its metadata.
func (f fixture) assertPreviewsKept(t *testing.T, jobID string) {
	t.Helper()
	if !f.exists(t, fmt.Sprintf("thumbnails/%s-part01.jpg", jobID)) {
		t.Fatalf("tombstone purged the poster of %s", jobID)
	}
	if !f.exists(t, fmt.Sprintf("thumbnails/%s-part01-strip.jpg", jobID)) {
		t.Fatalf("tombstone deleted the strip of %s", jobID)
	}
}

func (f fixture) assertTombstonedMissing(t *testing.T, id int64) {
	t.Helper()
	v, err := f.repo.GetVideo(f.ctx, id)
	if err != nil {
		t.Fatalf("GetVideo %d: %v", id, err)
	}
	if v.DeletedAt == nil {
		t.Fatalf("recording %d still live, want tombstoned", id)
	}
	if v.DeletionKind == nil || *v.DeletionKind != repository.DeletionKindMissing {
		t.Fatalf("recording %d deletion kind = %v, want %q", id, v.DeletionKind, repository.DeletionKindMissing)
	}
	if v.Thumbnail == nil {
		t.Fatalf("recording %d lost its poster path on a missing-media tombstone", id)
	}
}

func TestSweep_TombstonesRecordingWhoseMediaIsGone(t *testing.T) {
	f := newFixture(t)
	present := f.seed(t, "present", 2, 1, 2)
	gone := f.seed(t, "gone", 2)

	report, err := f.svc.Sweep(f.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if want := (Report{Scanned: 2, Missing: 1, Tombstoned: 1}); report != want {
		t.Fatalf("report = %+v, want %+v", report, want)
	}
	f.assertLive(t, present.ID)
	f.assertTombstonedMissing(t, gone.ID)
	f.assertPreviewsKept(t, "gone")
	if !f.exists(t, "thumbnails/present-part01.jpg") || !f.exists(t, "videos/present-part01.mp4") {
		t.Fatal("sweep touched the present recording's objects")
	}

	report, err = f.svc.Sweep(f.ctx)
	if err != nil {
		t.Fatalf("second Sweep: %v", err)
	}
	if want := (Report{Scanned: 1}); report != want {
		t.Fatalf("second report = %+v, want %+v", report, want)
	}
}

func TestSweep_LeavesPartiallyMissingRecordingInPlace(t *testing.T) {
	f := newFixture(t)
	partial := f.seed(t, "partial", 3, 1, 3)

	report, err := f.svc.Sweep(f.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if want := (Report{Scanned: 1, Partial: 1}); report != want {
		t.Fatalf("report = %+v, want %+v", report, want)
	}
	f.assertLive(t, partial.ID)
}

func TestSweep_RefusesWhenMostOfTheLibraryIsMissing(t *testing.T) {
	f := newFixture(t)
	present := f.seed(t, "present", 1, 1)
	gone := []*repository.Video{f.seed(t, "gone-a", 1), f.seed(t, "gone-b", 1), f.seed(t, "gone-c", 1)}

	report, err := f.svc.Sweep(f.ctx)
	if !errors.Is(err, ErrTooManyMissing) {
		t.Fatalf("Sweep err = %v, want ErrTooManyMissing", err)
	}
	if want := (Report{Scanned: 4, Missing: 3}); report != want {
		t.Fatalf("report = %+v, want %+v", report, want)
	}
	f.assertLive(t, present.ID)
	for _, v := range gone {
		f.assertLive(t, v.ID)
	}
}

// TestSweep_SmallLibraryIsNotRefused pins the floor: one of one and two of
// three exceed maxMissingShare but stay under minMissingToRefuse, so the sweep
// tombstones them.
func TestSweep_SmallLibraryIsNotRefused(t *testing.T) {
	t.Run("one of one", func(t *testing.T) {
		f := newFixture(t)
		gone := f.seed(t, "gone", 1)

		report, err := f.svc.Sweep(f.ctx)
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		if want := (Report{Scanned: 1, Missing: 1, Tombstoned: 1}); report != want {
			t.Fatalf("report = %+v, want %+v", report, want)
		}
		f.assertTombstonedMissing(t, gone.ID)
	})
	t.Run("two of three", func(t *testing.T) {
		f := newFixture(t)
		present := f.seed(t, "present", 1, 1)
		goneA := f.seed(t, "gone-a", 1)
		goneB := f.seed(t, "gone-b", 1)

		report, err := f.svc.Sweep(f.ctx)
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		if want := (Report{Scanned: 3, Missing: 2, Tombstoned: 2}); report != want {
			t.Fatalf("report = %+v, want %+v", report, want)
		}
		f.assertLive(t, present.ID)
		f.assertTombstonedMissing(t, goneA.ID)
		f.assertTombstonedMissing(t, goneB.ID)
	})
}

func TestSweep_RefusesWhenStorageRootIsUnreachable(t *testing.T) {
	f := newFixture(t)
	v := f.seed(t, "rec", 1, 1)
	if err := os.RemoveAll(filepath.Join(f.store.Root, "videos")); err != nil {
		t.Fatalf("unmount videos dir: %v", err)
	}

	report, err := f.svc.Sweep(f.ctx)
	if !errors.Is(err, ErrStorageUnreachable) {
		t.Fatalf("Sweep err = %v, want ErrStorageUnreachable", err)
	}
	if report != (Report{}) {
		t.Fatalf("report = %+v, want empty", report)
	}
	f.assertLive(t, v.ID)

	if _, err := f.svc.MarkMissing(f.ctx, v.ID); !errors.Is(err, ErrStorageUnreachable) {
		t.Fatalf("MarkMissing err = %v, want ErrStorageUnreachable", err)
	}
	f.assertLive(t, v.ID)
}

func TestSweep_HandlesLegacySingleFileAndFailedRows(t *testing.T) {
	f := newFixture(t)
	legacyPresent := f.seed(t, "legacy-present", 0)
	f.save(t, "videos/legacy-present.mp4")
	legacyGone := f.seed(t, "legacy-gone", 0)
	failed, err := f.repo.CreateVideo(f.ctx, &repository.VideoInput{
		JobID: "failed", Filename: "failed", DisplayName: "b-scan",
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		BroadcasterID: "b-scan", RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("CreateVideo failed: %v", err)
	}
	if err := f.repo.MarkVideoFailed(f.ctx, failed.ID, "boom", repository.CompletionKindCancelled, false); err != nil {
		t.Fatalf("MarkVideoFailed: %v", err)
	}

	report, err := f.svc.Sweep(f.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if want := (Report{Scanned: 2, Missing: 1, Tombstoned: 1}); report != want {
		t.Fatalf("report = %+v, want %+v", report, want)
	}
	f.assertLive(t, legacyPresent.ID)
	f.assertTombstonedMissing(t, legacyGone.ID)
	f.assertLive(t, failed.ID)
}

// statErrStore fails Stat for one path with an error other than not-found. It
// implements the same root probe as the underlying store.
type statErrStore struct {
	storage.Storage
	failPath string
}

func (s statErrStore) ProbeRoot(ctx context.Context) error {
	return s.Storage.(storage.RootProber).ProbeRoot(ctx)
}

func (s statErrStore) Stat(ctx context.Context, path string) (storage.FileInfo, error) {
	if path == s.failPath {
		return storage.FileInfo{}, errors.New("storage hiccup")
	}
	return s.Storage.Stat(ctx, path)
}

func TestSweep_StatErrorNeverCountsAsMissing(t *testing.T) {
	f := newFixture(t)
	flaky := f.seed(t, "flaky", 1)
	gone := f.seed(t, "gone", 1)
	svc := New(f.repo, statErrStore{Storage: f.store, failPath: "videos/flaky-part01.mp4"}, discardLog())

	report, err := svc.Sweep(f.ctx)
	if err == nil || !strings.Contains(err.Error(), "storage hiccup") {
		t.Fatalf("Sweep err = %v, want the stat error surfaced", err)
	}
	if want := (Report{Scanned: 2, Missing: 1}); report != want {
		t.Fatalf("report = %+v, want %+v", report, want)
	}
	f.assertLive(t, flaky.ID)
	f.assertLive(t, gone.ID)
}

func TestMarkMissing(t *testing.T) {
	f := newFixture(t)
	gone := f.seed(t, "gone", 1)
	partial := f.seed(t, "partial", 2, 2)
	present := f.seed(t, "present", 1, 1)

	tombstoned, err := f.svc.MarkMissing(f.ctx, gone.ID)
	if err != nil || !tombstoned {
		t.Fatalf("MarkMissing gone = (%v, %v), want (true, nil)", tombstoned, err)
	}
	f.assertTombstonedMissing(t, gone.ID)
	f.assertPreviewsKept(t, "gone")

	tombstoned, err = f.svc.MarkMissing(f.ctx, partial.ID)
	if err != nil || tombstoned {
		t.Fatalf("MarkMissing partial = (%v, %v), want (false, nil)", tombstoned, err)
	}
	f.assertLive(t, partial.ID)

	tombstoned, err = f.svc.MarkMissing(f.ctx, present.ID)
	if err != nil || tombstoned {
		t.Fatalf("MarkMissing present = (%v, %v), want (false, nil)", tombstoned, err)
	}
	f.assertLive(t, present.ID)

	// Already tombstoned rows and unknown ids are not candidates.
	tombstoned, err = f.svc.MarkMissing(f.ctx, gone.ID)
	if err != nil || tombstoned {
		t.Fatalf("MarkMissing again = (%v, %v), want (false, nil)", tombstoned, err)
	}
	tombstoned, err = f.svc.MarkMissing(f.ctx, 999)
	if err != nil || tombstoned {
		t.Fatalf("MarkMissing unknown = (%v, %v), want (false, nil)", tombstoned, err)
	}
}

func TestMarkMissing_SkipsRecordingQueuedForManualDelete(t *testing.T) {
	f := newFixture(t)
	queued := f.seed(t, "queued", 1)
	if _, err := f.repo.RequestVideoDelete(f.ctx, queued.ID); err != nil {
		t.Fatalf("RequestVideoDelete: %v", err)
	}

	tombstoned, err := f.svc.MarkMissing(f.ctx, queued.ID)
	if err != nil || tombstoned {
		t.Fatalf("MarkMissing queued = (%v, %v), want (false, nil)", tombstoned, err)
	}
	f.assertLive(t, queued.ID)
}

func TestMarkMissing_InvalidIDsNeverSelectAnotherRecording(t *testing.T) {
	f := newFixture(t)
	v := f.seed(t, "gone", 1)
	for _, id := range []int64{0, -1, v.ID + 100} {
		changed, err := f.svc.MarkMissing(f.ctx, id)
		if changed || err != nil {
			t.Fatalf("MarkMissing(%d) = %v, %v", id, changed, err)
		}
	}
	f.assertLive(t, v.ID)
}

func TestMarkMissing_UsesSweepOutageGuard(t *testing.T) {
	f := newFixture(t)
	var first int64
	for i := range 4 {
		v := f.seed(t, fmt.Sprintf("gone-%d", i), 1)
		if i == 0 {
			first = v.ID
		}
	}
	changed, err := f.svc.MarkMissing(f.ctx, first)
	if changed || !errors.Is(err, ErrTooManyMissing) {
		t.Fatalf("mark = %v, %v", changed, err)
	}
	f.assertLive(t, first)
}

type beforeTombstoneRepo struct {
	repository.Repository
	before func(int64)
}

func (r beforeTombstoneRepo) TombstoneMissingVideo(ctx context.Context, id int64) (bool, error) {
	r.before(id)
	return r.Repository.TombstoneMissingVideo(ctx, id)
}

func TestMarkMissing_ManualRequestWinsDuringInspection(t *testing.T) {
	f := newFixture(t)
	v := f.seed(t, "manual-race", 1)
	repo := beforeTombstoneRepo{Repository: f.repo, before: func(id int64) {
		if _, err := f.repo.RequestVideoDelete(f.ctx, id); err != nil {
			t.Fatal(err)
		}
	}}
	changed, err := New(repo, f.store, discardLog()).MarkMissing(f.ctx, v.ID)
	if changed || err != nil {
		t.Fatalf("mark = %v, %v", changed, err)
	}
	f.assertLive(t, v.ID)
	f.assertPreviewsKept(t, v.Filename)
	parts, err := f.repo.ListVideoParts(f.ctx, v.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("part metadata = %+v, %v", parts, err)
	}
}

func TestMarkMissing_NeverDeletesMediaThatReturns(t *testing.T) {
	f := newFixture(t)
	v := f.seed(t, "restored", 1)
	repo := beforeTombstoneRepo{Repository: f.repo, before: func(id int64) { f.save(t, "videos/restored-part01.mp4") }}
	changed, err := New(repo, f.store, discardLog()).MarkMissing(f.ctx, v.ID)
	if !changed || err != nil {
		t.Fatalf("mark = %v, %v", changed, err)
	}
	if !f.exists(t, "videos/restored-part01.mp4") {
		t.Fatal("discovery deleted restored media")
	}
	parts, err := f.repo.ListVideoParts(f.ctx, v.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("lost recovery metadata: %+v, %v", parts, err)
	}
}

func TestMarkMissing_PreservesSnapshotPoster(t *testing.T) {
	f := newFixture(t)
	v := f.seed(t, "snapshot", 1)
	poster := "thumbnails/snapshot-snap00.jpg"
	f.save(t, poster)
	if err := f.repo.SetVideoThumbnail(f.ctx, v.ID, poster); err != nil {
		t.Fatal(err)
	}
	changed, err := f.svc.MarkMissing(f.ctx, v.ID)
	if !changed || err != nil {
		t.Fatalf("mark = %v, %v", changed, err)
	}
	got, err := f.repo.GetVideo(f.ctx, v.ID)
	if err != nil || got.Thumbnail == nil || *got.Thumbnail != poster || !f.exists(t, poster) {
		t.Fatalf("snapshot poster lost: %+v, %v", got, err)
	}
}

func TestSweep_RequiresRootProbe(t *testing.T) {
	f := newFixture(t)
	v := f.seed(t, "gone", 1)
	store := struct{ storage.Storage }{f.store}
	_, err := New(f.repo, store, discardLog()).Sweep(f.ctx)
	if !errors.Is(err, ErrStorageUnreachable) {
		t.Fatalf("Sweep = %v", err)
	}
	f.assertLive(t, v.ID)
}

type pageTrackingRepo struct {
	repository.Repository
	pages  []int64
	cancel context.CancelFunc
}

func (r *pageTrackingRepo) ListVideosForStorageScan(ctx context.Context, after int64, limit int) ([]repository.StorageScanVideo, error) {
	if limit > scanPageSize {
		return nil, errors.New("unbounded scan")
	}
	r.pages = append(r.pages, after)
	if len(r.pages) == 2 && r.cancel != nil {
		r.cancel()
		return nil, ctx.Err()
	}
	return r.Repository.ListVideosForStorageScan(ctx, after, limit)
}
func (r *pageTrackingRepo) ListVideoParts(ctx context.Context, id int64) ([]repository.VideoPart, error) {
	return nil, errors.New("unexpected per-video part query")
}

func TestSweep_BatchesPartsAndResumesAfterDeadline(t *testing.T) {
	f := newFixture(t)
	var last *repository.Video
	for i := range scanPageSize + 1 {
		last = f.seed(t, fmt.Sprintf("page-%d", i), 1, 1)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	repo := &pageTrackingRepo{Repository: f.repo, cancel: cancel}
	svc := New(repo, f.store, discardLog())
	report, err := svc.Sweep(ctx)
	if !errors.Is(err, context.Canceled) || report.Scanned != scanPageSize {
		t.Fatalf("first page = %+v, %v", report, err)
	}
	report, err = svc.Sweep(f.ctx)
	if err != nil || report.Scanned != 1 {
		t.Fatalf("resume = %+v, %v", report, err)
	}
	if repo.pages[2] != last.ID-1 {
		t.Fatalf("resumed at %d, want %d", repo.pages[2], last.ID-1)
	}
}

func TestMarkMissing_PreservesReadyPlaybackCopy(t *testing.T) {
	f := newFixture(t)
	v := f.seed(t, "playback-copy", 2)
	name, mime := "playback-copy.mp4", "video/mp4"
	duration, size, now := float64(60), int64(100), time.Now()
	f.save(t, "videos/"+name)
	_, err := f.repo.UpsertVideoPlaybackAsset(f.ctx, &repository.VideoPlaybackAssetInput{
		VideoID: v.ID, Status: repository.PlaybackAssetStatusReady,
		Filename: &name, MimeType: &mime, DurationSeconds: &duration, SizeBytes: &size,
		GeneratedAt: &now, LastAccessedAt: &now,
	})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := f.svc.MarkMissing(f.ctx, v.ID)
	if err != nil || changed {
		t.Fatalf("playable copy tombstoned: %v, %v", changed, err)
	}
	f.assertLive(t, v.ID)
}
