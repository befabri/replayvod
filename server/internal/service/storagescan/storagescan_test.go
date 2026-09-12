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
	"github.com/befabri/replayvod/server/internal/service/storagehealth"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/testdb"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fixture struct {
	ctx   context.Context
	repo  repository.Repository
	store *storage.LocalStorage
	root  string
	mon   *storagehealth.Monitor
	svc   *Service
}

// newFixture attaches a fresh local root, so storage carries the identity the
// database expects and the scan trusts what it says.
func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	root := filepath.Join(t.TempDir(), "data")
	store, err := storage.NewLocal(root)
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	mon := storagehealth.New(repo, store, nil, discardLog(), "local", root)
	if _, err := mon.Attach(ctx); err != nil {
		t.Fatalf("attach: %v", err)
	}
	contracttest.SeedUserChannel(t, ctx, repo, "u-scan", "b-scan")
	return fixture{
		ctx:   ctx,
		repo:  repo,
		store: store,
		root:  root,
		mon:   mon,
		svc:   New(repo, store, mon, discardLog()),
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

func (f fixture) missingEvents(t *testing.T) int {
	t.Helper()
	rows, err := f.repo.ListEventLogs(f.ctx, 500, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range rows {
		if r.Domain == EventDomain && r.EventType == EventRecordingMissing {
			n++
		}
	}
	return n
}

func (f fixture) scanCursor(t *testing.T) int64 {
	t.Helper()
	settings, err := f.repo.GetServerSettings(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	return settings.StorageScanCursor
}

func (f fixture) replaceMarker(t *testing.T, id string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.root, storage.MarkerPath), []byte(id+"\n"), 0o644); err != nil {
		t.Fatal(err)
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
	if want := (Report{Complete: true, Scanned: 2, Missing: 1, Tombstoned: 1}); report != want {
		t.Fatalf("report = %+v, want %+v", report, want)
	}
	f.assertLive(t, present.ID)
	f.assertTombstonedMissing(t, gone.ID)
	f.assertPreviewsKept(t, "gone")
	if !f.exists(t, "thumbnails/present-part01.jpg") || !f.exists(t, "videos/present-part01.mp4") {
		t.Fatal("sweep touched the present recording's objects")
	}
	if f.missingEvents(t) != 0 || len(f.scanSummaries(t)) != 1 {
		t.Fatal("sweep must emit one summary and no per-recording events")
	}
	if f.scanCursor(t) != 0 {
		t.Fatalf("cursor after a complete sweep = %d, want 0", f.scanCursor(t))
	}

	report, err = f.svc.Sweep(f.ctx)
	if err != nil {
		t.Fatalf("second Sweep: %v", err)
	}
	if want := (Report{Complete: true, Scanned: 1}); report != want {
		t.Fatalf("second report = %+v, want %+v", report, want)
	}
	if len(f.scanSummaries(t)) != 1 {
		t.Fatal("a second sweep re-announced the tombstone")
	}
}

func TestSweep_LeavesPartiallyMissingRecordingInPlace(t *testing.T) {
	f := newFixture(t)
	partial := f.seed(t, "partial", 3, 1, 3)

	report, err := f.svc.Sweep(f.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if want := (Report{Complete: true, Scanned: 1, Partial: 1}); report != want {
		t.Fatalf("report = %+v, want %+v", report, want)
	}
	f.assertLive(t, partial.ID)
}

// TestSweep_TombstonesEveryMissingRecordingOnAttachedStorage pins that no
// ratio decides: three of four deleted by hand on attached storage are all
// reconciled, and the one still present is untouched.
func TestSweep_TombstonesEveryMissingRecordingOnAttachedStorage(t *testing.T) {
	f := newFixture(t)
	present := f.seed(t, "present", 1, 1)
	gone := []*repository.Video{f.seed(t, "gone-a", 1), f.seed(t, "gone-b", 1), f.seed(t, "gone-c", 1)}

	report, err := f.svc.Sweep(f.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if want := (Report{Complete: true, Scanned: 4, Missing: 3, Tombstoned: 3}); report != want {
		t.Fatalf("report = %+v, want %+v", report, want)
	}
	f.assertLive(t, present.ID)
	for _, v := range gone {
		f.assertTombstonedMissing(t, v.ID)
	}
}

// TestSweep_TombstonesContiguousBlock covers a whole run of consecutive ids
// deleted by hand, larger than half a page, inside a library bigger than one
// page.
func TestSweep_TombstonesContiguousBlock(t *testing.T) {
	f := newFixture(t)
	const total, blockStart, blockEnd = scanPageSize + 8, 10, 50
	var gone, kept []int64
	for i := range total {
		if i >= blockStart && i < blockEnd {
			gone = append(gone, f.seed(t, fmt.Sprintf("rec-%02d", i), 1).ID)
			continue
		}
		kept = append(kept, f.seed(t, fmt.Sprintf("rec-%02d", i), 1, 1).ID)
	}
	report, err := f.svc.Sweep(f.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if want := (Report{Complete: true, Scanned: total, Missing: len(gone), Tombstoned: len(gone)}); report != want {
		t.Fatalf("report = %+v, want %+v", report, want)
	}
	for _, id := range gone {
		f.assertTombstonedMissing(t, id)
	}
	for _, id := range kept {
		f.assertLive(t, id)
	}
}

// TestSweep_SmallLibrary pins that a tiny library is never refused: one of
// one and two of three are reconciled like any other.
func TestSweep_SmallLibrary(t *testing.T) {
	t.Run("one of one", func(t *testing.T) {
		f := newFixture(t)
		gone := f.seed(t, "gone", 1)

		report, err := f.svc.Sweep(f.ctx)
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		if want := (Report{Complete: true, Scanned: 1, Missing: 1, Tombstoned: 1}); report != want {
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
		if want := (Report{Complete: true, Scanned: 3, Missing: 2, Tombstoned: 2}); report != want {
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
	if err := os.RemoveAll(f.root); err != nil {
		t.Fatalf("unmount root: %v", err)
	}

	report, err := f.svc.Sweep(f.ctx)
	if !errors.Is(err, storage.ErrUnreachable) {
		t.Fatalf("Sweep err = %v, want ErrUnreachable", err)
	}
	if report != (Report{}) {
		t.Fatalf("report = %+v, want empty", report)
	}
	f.assertLive(t, v.ID)

	if _, err := f.svc.MarkMissing(f.ctx, v.ID); !errors.Is(err, storage.ErrUnreachable) {
		t.Fatalf("MarkMissing err = %v, want ErrUnreachable", err)
	}
	f.assertLive(t, v.ID)
	if _, err := os.Stat(f.root); err == nil {
		t.Fatal("a refused scan recreated the root")
	}
}

// TestSweep_RefusesForeignStorageUntilAdopted is the wrong-volume case: a
// directory with another install's marker holds none of our files, and none
// of them may be tombstoned until the operator adopts it deliberately.
func TestSweep_RefusesForeignStorageUntilAdopted(t *testing.T) {
	f := newFixture(t)
	gone := f.seed(t, "gone", 1)
	present := f.seed(t, "present", 1, 1)
	theirs, _ := storage.NewStorageID()
	f.replaceMarker(t, theirs)

	report, err := f.svc.Sweep(f.ctx)
	if !errors.Is(err, storage.ErrUnattached) || report != (Report{}) {
		t.Fatalf("Sweep = %+v, %v; want ErrUnattached and no progress", report, err)
	}
	if _, err := f.svc.MarkMissing(f.ctx, gone.ID); !errors.Is(err, storage.ErrUnattached) {
		t.Fatalf("MarkMissing err = %v, want ErrUnattached", err)
	}
	f.assertLive(t, gone.ID)

	if _, err := f.mon.Adopt(f.ctx, "u-scan"); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	report, err = f.svc.Sweep(f.ctx)
	if err != nil {
		t.Fatalf("Sweep after adopt: %v", err)
	}
	if want := (Report{Complete: true, Scanned: 2, Missing: 1, Tombstoned: 1}); report != want {
		t.Fatalf("report = %+v, want %+v", report, want)
	}
	f.assertTombstonedMissing(t, gone.ID)
	f.assertLive(t, present.ID)
}

// TestSweep_RefusalClearsWhenMarkerReturns pins that a refusal is not sticky:
// the moment the expected marker is visible again the scan proceeds.
func TestSweep_RefusalClearsWhenMarkerReturns(t *testing.T) {
	f := newFixture(t)
	gone := f.seed(t, "gone", 1)
	marker := filepath.Join(f.root, storage.MarkerPath)
	if err := os.Rename(marker, marker+".aside"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Sweep(f.ctx); !errors.Is(err, storage.ErrUnattached) {
		t.Fatalf("Sweep err = %v, want ErrUnattached", err)
	}
	f.assertLive(t, gone.ID)
	if err := os.Rename(marker+".aside", marker); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Sweep(f.ctx); err != nil {
		t.Fatalf("Sweep after the marker returned: %v", err)
	}
	f.assertTombstonedMissing(t, gone.ID)
}

type readyFunc func(context.Context) error

func (f readyFunc) Verify(ctx context.Context) error { return f(ctx) }

func TestSweep_ScansReadOnlyStorage(t *testing.T) {
	f := newFixture(t)
	gone := f.seed(t, "gone", 1)
	svc := New(f.repo, f.store, readyFunc(func(context.Context) error {
		return fmt.Errorf("%w: probe", storage.ErrReadOnly)
	}), discardLog())
	if _, err := svc.Sweep(f.ctx); err != nil {
		t.Fatalf("Sweep on read-only storage: %v", err)
	}
	f.assertTombstonedMissing(t, gone.ID)
}

func TestSweep_WithoutReadinessFailsClosed(t *testing.T) {
	f := newFixture(t)
	gone := f.seed(t, "gone", 1)
	svc := New(f.repo, f.store, nil, discardLog())
	if _, err := svc.Sweep(f.ctx); !errors.Is(err, storage.ErrUnreachable) {
		t.Fatalf("Sweep = %v, want ErrUnreachable", err)
	}
	if _, err := svc.MarkMissing(f.ctx, gone.ID); !errors.Is(err, storage.ErrUnreachable) {
		t.Fatalf("MarkMissing = %v, want ErrUnreachable", err)
	}
	f.assertLive(t, gone.ID)
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
	if want := (Report{Complete: true, Scanned: 2, Missing: 1, Tombstoned: 1}); report != want {
		t.Fatalf("report = %+v, want %+v", report, want)
	}
	f.assertLive(t, legacyPresent.ID)
	f.assertTombstonedMissing(t, legacyGone.ID)
	f.assertLive(t, failed.ID)
}

// statErrStore fails Stat for one path with an error other than not-found.
type statErrStore struct {
	storage.Storage
	failPath string
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
	svc := New(f.repo, statErrStore{Storage: f.store, failPath: "videos/flaky-part01.mp4"}, f.mon, discardLog())

	report, err := svc.Sweep(f.ctx)
	if err == nil || !strings.Contains(err.Error(), "storage hiccup") {
		t.Fatalf("Sweep err = %v, want the stat error surfaced", err)
	}
	if want := (Report{Complete: true, Scanned: 2, Missing: 1}); report != want {
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
	if f.missingEvents(t) != 1 {
		t.Fatalf("recording_missing events = %d, want 1", f.missingEvents(t))
	}

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

// TestMarkMissing_InspectsOnlyTheTarget pins the playback path's scope: with
// every neighbour gone too, one request reconciles exactly the recording it
// asked about and stats nothing else.
func TestMarkMissing_InspectsOnlyTheTarget(t *testing.T) {
	f := newFixture(t)
	var ids []int64
	for i := range 4 {
		ids = append(ids, f.seed(t, fmt.Sprintf("gone-%d", i), 1).ID)
	}
	counting := &statCountingStore{Storage: f.store}
	svc := New(f.repo, counting, f.mon, discardLog())
	changed, err := svc.MarkMissing(f.ctx, ids[0])
	if !changed || err != nil {
		t.Fatalf("mark = %v, %v", changed, err)
	}
	f.assertTombstonedMissing(t, ids[0])
	for _, id := range ids[1:] {
		f.assertLive(t, id)
	}
	if counting.stats != 1 {
		t.Fatalf("stat calls = %d, want 1 (the target's single part)", counting.stats)
	}
}

type statCountingStore struct {
	storage.Storage
	stats int
}

func (s *statCountingStore) Stat(ctx context.Context, path string) (storage.FileInfo, error) {
	s.stats++
	return s.Storage.Stat(ctx, path)
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
	changed, err := New(repo, f.store, f.mon, discardLog()).MarkMissing(f.ctx, v.ID)
	if changed || err != nil {
		t.Fatalf("mark = %v, %v", changed, err)
	}
	f.assertLive(t, v.ID)
	f.assertPreviewsKept(t, v.Filename)
	parts, err := f.repo.ListVideoParts(f.ctx, v.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("part metadata = %+v, %v", parts, err)
	}
	if f.missingEvents(t) != 0 {
		t.Fatal("announced a tombstone that never happened")
	}
}

func TestMarkMissing_NeverDeletesMediaThatReturns(t *testing.T) {
	f := newFixture(t)
	v := f.seed(t, "restored", 1)
	repo := beforeTombstoneRepo{Repository: f.repo, before: func(id int64) { f.save(t, "videos/restored-part01.mp4") }}
	changed, err := New(repo, f.store, f.mon, discardLog()).MarkMissing(f.ctx, v.ID)
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

// pageTrackingRepo records the cursor of every page list and cancels the run
// on the page numbered cancelOn, standing in for scheduler shutdown.
type pageTrackingRepo struct {
	repository.Repository
	pages    []int64
	cancelOn int
	cancel   context.CancelFunc
}

func (r *pageTrackingRepo) ListVideosForStorageScan(ctx context.Context, after int64, limit int) ([]repository.StorageScanVideo, error) {
	if limit > scanPageSize {
		return nil, errors.New("unbounded scan")
	}
	r.pages = append(r.pages, after)
	if len(r.pages) == r.cancelOn && r.cancel != nil {
		r.cancel()
		return nil, ctx.Err()
	}
	return r.Repository.ListVideosForStorageScan(ctx, after, limit)
}

func (r *pageTrackingRepo) ListVideoParts(ctx context.Context, id int64) ([]repository.VideoPart, error) {
	return nil, errors.New("unexpected per-video part query")
}

// TestSweep_BatchesPartsAndResumesAfterCancellation pins that a run cut short by
// cancellation after one full page retains its progress, that the cursor it reached
// is persisted, and that a fresh service (a restarted process) resumes there.
func TestSweep_BatchesPartsAndResumesAfterCancellation(t *testing.T) {
	f := newFixture(t)
	var last *repository.Video
	for i := range scanPageSize + 1 {
		last = f.seed(t, fmt.Sprintf("page-%d", i), 1, 1)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	repo := &pageTrackingRepo{Repository: f.repo, cancelOn: 2, cancel: cancel}
	report, err := New(repo, f.store, f.mon, discardLog()).Sweep(ctx)
	if !errors.Is(err, context.Canceled) || report.Complete || report.Scanned != scanPageSize {
		t.Fatalf("first page = %+v, %v; want an interrupted run with saved progress", report, err)
	}
	if got := f.scanCursor(t); got != last.ID-1 {
		t.Fatalf("persisted cursor = %d, want %d", got, last.ID-1)
	}

	resumed := &pageTrackingRepo{Repository: f.repo}
	report, err = New(resumed, f.store, f.mon, discardLog()).Sweep(f.ctx)
	if err != nil || !report.Complete || report.Scanned != 1 {
		t.Fatalf("resume = %+v, %v", report, err)
	}
	if resumed.pages[0] != last.ID-1 {
		t.Fatalf("resumed at %d, want %d", resumed.pages[0], last.ID-1)
	}
	if f.scanCursor(t) != 0 {
		t.Fatalf("cursor after completion = %d, want 0", f.scanCursor(t))
	}
}

func TestSweep_CancellationBeforeAnyPageIsSurfaced(t *testing.T) {
	f := newFixture(t)
	f.seed(t, "rec", 1, 1)
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	repo := &pageTrackingRepo{Repository: f.repo, cancelOn: 1, cancel: cancel}
	report, err := New(repo, f.store, f.mon, discardLog()).Sweep(ctx)
	if !errors.Is(err, context.Canceled) || report.Scanned != 0 {
		t.Fatalf("no-progress run = %+v, %v; want cancellation surfaced", report, err)
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

func (f fixture) restoredEvents(t *testing.T) int {
	t.Helper()
	rows, err := f.repo.ListEventLogs(f.ctx, 500, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range rows {
		if r.Domain == EventDomain && r.EventType == EventRecordingRestored {
			n++
		}
	}
	return n
}

// tombstoneMissing runs a sweep that reconciles the seeded recording, so the
// restore tests start from a real missing tombstone.
func (f fixture) tombstoneMissing(t *testing.T, id int64) {
	t.Helper()
	if _, err := f.svc.Sweep(f.ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	f.assertTombstonedMissing(t, id)
}

func TestRestore_BringsBackRecordingWhoseMediaReturned(t *testing.T) {
	f := newFixture(t)
	gone := f.seed(t, "gone", 2)
	f.tombstoneMissing(t, gone.ID)

	f.save(t, "videos/gone-part01.mp4")
	f.save(t, "videos/gone-part02.mp4")
	if err := f.svc.Restore(f.ctx, gone.ID); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	f.assertLive(t, gone.ID)
	parts, err := f.repo.ListVideoParts(f.ctx, gone.ID)
	if err != nil || len(parts) != 2 {
		t.Fatalf("parts after restore = %+v, %v", parts, err)
	}
	if f.restoredEvents(t) != 1 {
		t.Fatalf("recording_restored events = %d, want 1", f.restoredEvents(t))
	}
	if err := f.svc.Restore(f.ctx, gone.ID); !errors.Is(err, ErrNotRestorable) {
		t.Fatalf("second restore err = %v, want ErrNotRestorable", err)
	}
}

func TestRestore_RefusesWhilePartOfTheMediaIsStillMissing(t *testing.T) {
	f := newFixture(t)
	gone := f.seed(t, "gone", 3)
	f.tombstoneMissing(t, gone.ID)

	f.save(t, "videos/gone-part02.mp4")
	err := f.svc.Restore(f.ctx, gone.ID)
	var still *StillMissingError
	if !errors.As(err, &still) || !errors.Is(err, ErrStillMissing) || still.Missing != 2 || still.Total != 3 {
		t.Fatalf("Restore err = %v, want 2 of 3 still missing", err)
	}
	if err := f.svc.Restore(f.ctx, gone.ID); err == nil {
		t.Fatal("restore with nothing back succeeded")
	}
	f.assertTombstonedMissing(t, gone.ID)
	if f.restoredEvents(t) != 0 {
		t.Fatal("a refused restore was announced")
	}
}

func TestRestore_RefusesUnattachedStorage(t *testing.T) {
	f := newFixture(t)
	gone := f.seed(t, "gone", 1)
	f.tombstoneMissing(t, gone.ID)
	f.save(t, "videos/gone-part01.mp4")
	theirs, _ := storage.NewStorageID()
	f.replaceMarker(t, theirs)
	if err := f.svc.Restore(f.ctx, gone.ID); !errors.Is(err, storage.ErrUnattached) {
		t.Fatalf("Restore err = %v, want ErrUnattached", err)
	}
	f.assertTombstonedMissing(t, gone.ID)
}

func TestRestore_RefusesDirectoriesAtMediaPaths(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(fmt.Sprintf("automatic=%v", automatic), func(t *testing.T) {
			f := newFixture(t)
			gone := f.seed(t, "gone", 1)
			f.tombstoneMissing(t, gone.ID)
			if err := os.MkdirAll(filepath.Join(f.root, "videos/gone-part01.mp4"), 0o755); err != nil {
				t.Fatal(err)
			}
			var err error
			if automatic {
				_, err = f.svc.Sweep(f.ctx)
			} else {
				err = f.svc.Restore(f.ctx, gone.ID)
			}
			if err == nil {
				t.Fatal("a directory was accepted as restored media")
			}
			f.assertTombstonedMissing(t, gone.ID)
			if f.restoredEvents(t) != 0 {
				t.Fatal("a refused restore was announced")
			}
		})
	}
}

func TestRestore_NotRestorableRows(t *testing.T) {
	f := newFixture(t)
	live := f.seed(t, "live", 1, 1)
	manual := f.seed(t, "manual", 1, 1)
	if err := f.repo.SoftDeleteVideo(f.ctx, manual.ID, repository.DeletionKindManual); err != nil {
		t.Fatal(err)
	}
	queued := f.seed(t, "queued", 1)
	f.tombstoneMissing(t, queued.ID)
	if _, err := f.repo.RequestVideoDelete(f.ctx, queued.ID); err != nil {
		t.Fatal(err)
	}
	f.save(t, "videos/queued-part01.mp4")
	// A failed tombstone without part rows owns no media to check.
	legacy, err := f.repo.CreateVideo(f.ctx, &repository.VideoInput{
		JobID: "legacy-failed", Filename: "legacy-failed", DisplayName: "b-scan",
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		BroadcasterID: "b-scan", RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.repo.MarkVideoFailed(f.ctx, legacy.ID, "boom", repository.CompletionKindPartial, false); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.SoftDeleteVideo(f.ctx, legacy.ID, repository.DeletionKindMissing); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{live.ID, manual.ID, queued.ID, legacy.ID, 0, -1, legacy.ID + 100} {
		if err := f.svc.Restore(f.ctx, id); !errors.Is(err, ErrNotRestorable) {
			t.Fatalf("Restore(%d) err = %v, want ErrNotRestorable", id, err)
		}
	}
	f.assertLive(t, live.ID)
}

// TestSweep_RestoresReturnedMediaAndTombstonesInOnePass pins the scan's two
// phases: a recording whose files came back rejoins the library while one
// whose files left is reconciled, and a second pass changes nothing.
func TestSweep_RestoresReturnedMediaAndTombstonesInOnePass(t *testing.T) {
	f := newFixture(t)
	returned := f.seed(t, "returned", 2)
	stillGone := f.seed(t, "still-gone", 1)
	f.tombstoneMissing(t, returned.ID)
	f.assertTombstonedMissing(t, stillGone.ID)
	f.save(t, "videos/returned-part01.mp4")
	f.save(t, "videos/returned-part02.mp4")
	fresh := f.seed(t, "fresh-gone", 1)

	report, err := f.svc.Sweep(f.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if want := (Report{Complete: true, Scanned: 1, Missing: 1, Tombstoned: 1, Restored: 1}); report != want {
		t.Fatalf("report = %+v, want %+v", report, want)
	}
	f.assertLive(t, returned.ID)
	f.assertTombstonedMissing(t, stillGone.ID)
	f.assertTombstonedMissing(t, fresh.ID)
	if f.restoredEvents(t) != 0 || len(f.scanSummaries(t)) != 2 {
		t.Fatal("mixed sweep must add one summary and no individual restore events")
	}
	assertScanSummary(t, f.scanSummaries(t)[0], report, false)

	report, err = f.svc.Sweep(f.ctx)
	if err != nil {
		t.Fatalf("second Sweep: %v", err)
	}
	if want := (Report{Complete: true, Scanned: 1}); report != want {
		t.Fatalf("second report = %+v, want %+v", report, want)
	}
}

func TestSweep_CancellationDuringRestorePhaseIsSurfaced(t *testing.T) {
	f := newFixture(t)
	returned := f.seed(t, "returned", 1)
	f.tombstoneMissing(t, returned.ID)
	f.save(t, "videos/returned-part01.mp4")
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	repo := &cancelOnMissingListRepo{Repository: f.repo, cancel: cancel}
	report, err := New(repo, f.store, f.mon, discardLog()).Sweep(ctx)
	if !errors.Is(err, context.Canceled) || report.Complete {
		t.Fatalf("run cut during restore = %+v, %v; want cancellation, incomplete", report, err)
	}
	f.assertTombstonedMissing(t, returned.ID)
}

type cancelOnMissingListRepo struct {
	repository.Repository
	cancel context.CancelFunc
}

func (r *cancelOnMissingListRepo) ListMissingTombstones(ctx context.Context, after int64, limit int) ([]repository.StorageScanVideo, error) {
	r.cancel()
	return nil, ctx.Err()
}

// TestRestore_RefusesWhenTheVODWasArchivedAgain pins the one-open-row rule on
// the restore path: a tombstoned archive whose VOD was archived anew is
// refused with a reason, and the sweep passes over it without failing.
func TestRestore_RefusesWhenTheVODWasArchivedAgain(t *testing.T) {
	f := newFixture(t)
	vod := "vod-1"
	archive := func(jobID string, present bool) *repository.Video {
		t.Helper()
		v, err := f.repo.CreateVideo(f.ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: "b-scan",
			Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
			BroadcasterID: "b-scan", RecordingType: repository.RecordingTypeVideo,
			Source: repository.VideoSourceVOD, TwitchVideoID: &vod,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.repo.CreateVideoPart(f.ctx, &repository.VideoPartInput{
			VideoID: v.ID, PartIndex: 1, Filename: jobID + "-part01.mp4",
			Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4,
		}); err != nil {
			t.Fatal(err)
		}
		poster := "thumbnails/" + jobID + "-part01.jpg"
		if err := f.repo.MarkVideoDone(f.ctx, v.ID, 60, 1024, &poster, repository.CompletionKindComplete, false); err != nil {
			t.Fatal(err)
		}
		f.save(t, poster)
		if present {
			f.save(t, "videos/"+jobID+"-part01.mp4")
		}
		return v
	}
	old := archive("old", false)
	f.tombstoneMissing(t, old.ID)
	f.save(t, "videos/old-part01.mp4")
	fresh := archive("fresh", true)

	if err := f.svc.Restore(f.ctx, old.ID); !errors.Is(err, ErrArchivedAgain) {
		t.Fatalf("Restore err = %v, want ErrArchivedAgain", err)
	}
	report, err := f.svc.Sweep(f.ctx)
	if err != nil || report.Restored != 0 || !report.Complete {
		t.Fatalf("sweep beside a re-archive = %+v, %v; want a clean run restoring nothing", report, err)
	}
	f.assertTombstonedMissing(t, old.ID)
	f.assertLive(t, fresh.ID)

	if err := f.repo.SoftDeleteVideo(f.ctx, fresh.ID, repository.DeletionKindManual); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Restore(f.ctx, old.ID); err != nil {
		t.Fatalf("Restore once the re-archive is gone: %v", err)
	}
	f.assertLive(t, old.ID)
}

// TestSweep_EmptyRootIsNeverInitializedOverALibrary is the upgrade hole: a
// database with recordings but no storage id starts against an empty mount
// point. Nothing may be tombstoned; once the real volume is attached the
// library is found intact.
func TestSweep_EmptyRootIsNeverInitializedOverALibrary(t *testing.T) {
	f := newFixture(t)
	var ids []int64
	for i := range 3 {
		ids = append(ids, f.seed(t, fmt.Sprintf("rec-%d", i), 1, 1).ID)
	}
	// Forget the identity, as an install upgraded to this version has none.
	if _, err := f.repo.SetStorageID(f.ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.root, storage.MarkerPath)); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(t.TempDir(), "unmounted")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	emptyStore, err := storage.NewLocal(empty)
	if err != nil {
		t.Fatal(err)
	}
	mon := storagehealth.New(f.repo, emptyStore, nil, discardLog(), "local", empty)
	if _, err := mon.Attach(f.ctx); !errors.Is(err, storage.ErrUnattached) {
		t.Fatalf("attach to the empty mount point = %v, want ErrUnattached", err)
	}
	if _, err := New(f.repo, emptyStore, mon, discardLog()).Sweep(f.ctx); !errors.Is(err, storage.ErrUnattached) {
		t.Fatalf("sweep against the empty mount point = %v, want refused", err)
	}
	for _, id := range ids {
		f.assertLive(t, id)
	}

	realMon := storagehealth.New(f.repo, f.store, nil, discardLog(), "local", f.root)
	if status, err := realMon.Attach(f.ctx); err != nil || status.State != storagehealth.StateAttached {
		t.Fatalf("attach to the real volume = %+v, %v", status, err)
	}
	report, err := New(f.repo, f.store, realMon, discardLog()).Sweep(f.ctx)
	if err != nil || report.Missing != 0 || report.Tombstoned != 0 || !report.Complete {
		t.Fatalf("sweep on the real volume = %+v, %v; want everything present", report, err)
	}
}
