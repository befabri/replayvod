package storagescan

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/befabri/replayvod/server/internal/testutil/mediatest"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
)

type interruptedRestoreRepo struct {
	repository.Repository
	pages  []int64
	cancel context.CancelFunc
}

func (r *interruptedRestoreRepo) ListMissingTombstones(ctx context.Context, page repository.BatchPage) ([]repository.StorageScanVideo, error) {
	r.pages = append(r.pages, page.AfterID())
	if r.cancel != nil && len(r.pages) == 2 {
		r.cancel()
		return nil, ctx.Err()
	}
	return r.Repository.ListMissingTombstones(ctx, page)
}

func TestSweepResumesRestorePastMissingPrefixAfterRestart(t *testing.T) {
	f := newFixture(t)
	var returned *repository.Video
	for i := range scanPageSize + 1 {
		returned = f.seed(t, fmt.Sprintf("restore-page-%d", i), 1)
	}
	if _, err := f.svc.Sweep(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.save(t, "videos/"+returned.Filename+"-part01.mp4")
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	first := &interruptedRestoreRepo{Repository: f.repo, cancel: cancel}
	report, err := New(first, mediatest.New(t, first, f.store, f.mon, nil), discardLog()).Sweep(ctx)
	if !errors.Is(err, context.Canceled) || report.Complete || report.Restored != 0 {
		t.Fatalf("interrupted restore = %+v, %v", report, err)
	}
	// A persisted restore cursor must survive a new service and skip earlier missing files.
	next := &interruptedRestoreRepo{Repository: f.repo}
	report, err = New(next, mediatest.New(t, next, f.store, f.mon, nil), discardLog()).Sweep(f.ctx)
	if len(next.pages) == 0 || next.pages[0] != returned.ID-1 {
		t.Fatalf("restore restarted at cursors %v, want first %d", next.pages, returned.ID-1)
	}
	if err != nil || !report.Complete || report.Restored != 1 {
		t.Fatalf("resumed restore = %+v, %v", report, err)
	}
	f.assertLive(t, returned.ID)
}

func (f fixture) restoreCursor(t *testing.T) *int64 {
	t.Helper()
	settings, err := f.repo.GetServerSettings(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	return settings.StorageRestoreCursor
}

func TestSweepReplaysPartlyRestoredPageAndWrapsAfterCompletion(t *testing.T) {
	f := newFixture(t)
	first := f.seed(t, "partial-page-first", 1)
	second := f.seed(t, "partial-page-second", 1)
	older := f.seed(t, "still-missing", 1)
	if _, err := f.svc.Sweep(f.ctx); err != nil {
		t.Fatal(err)
	}
	for _, v := range []*repository.Video{first, second} {
		f.save(t, "videos/"+v.Filename+"-part01.mp4")
	}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	r := &cancelAfterRestoreRepo{Repository: f.repo, cancel: cancel}
	report, err := New(r, mediatest.New(t, r, f.store, f.mon, nil), discardLog()).Sweep(ctx)
	if !errors.Is(err, context.Canceled) || report.Complete || report.Restored != 1 {
		t.Fatalf("partial page = %+v, %v", report, err)
	}
	if cursor := f.restoreCursor(t); cursor == nil || *cursor != 0 {
		t.Fatalf("partly restored page advanced cursor: %v", cursor)
	}
	f.assertLive(t, first.ID)
	f.assertTombstonedMissing(t, second.ID)
	report, err = New(f.repo, mediatest.New(t, f.repo, f.store, f.mon, nil), discardLog()).Sweep(f.ctx)
	if err != nil || !report.Complete || report.Restored != 1 || report.Scanned != 0 {
		t.Fatalf("resumed partial page = %+v, %v", report, err)
	}
	f.assertLive(t, second.ID)
	if cursor := f.restoreCursor(t); cursor != nil {
		t.Fatalf("finished pass retains cursor %d", *cursor)
	}
	// Completed passes must wrap so media returning behind the cursor can be restored.
	f.save(t, "videos/"+older.Filename+"-part01.mp4")
	report, err = New(f.repo, mediatest.New(t, f.repo, f.store, f.mon, nil), discardLog()).Sweep(f.ctx)
	if err != nil || report.Restored != 1 || !report.Complete {
		t.Fatalf("wrapped restore = %+v, %v", report, err)
	}
	f.assertLive(t, older.ID)
}

type failedRestorePageRepo struct {
	*interruptedRestoreRepo
	id  int64
	err error
}

func (r *failedRestorePageRepo) RestoreMissingVideo(ctx context.Context, id int64) error {
	if id == r.id {
		return r.err
	}
	return r.Repository.RestoreMissingVideo(ctx, id)
}

func TestSweepRestoreFailureDoesNotStarveLaterPages(t *testing.T) {
	f := newFixture(t)
	var first, last *repository.Video
	for i := range scanPageSize + 1 {
		last = f.seed(t, fmt.Sprintf("restore-error-%d", i), 1)
		if i == 0 {
			first = last
		}
	}
	if _, err := f.svc.Sweep(f.ctx); err != nil {
		t.Fatal(err)
	}
	for _, v := range []*repository.Video{first, last} {
		f.save(t, "videos/"+v.Filename+"-part01.mp4")
	}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	failure := errors.New("restore update failed")
	r := &failedRestorePageRepo{interruptedRestoreRepo: &interruptedRestoreRepo{Repository: f.repo, cancel: cancel}, id: first.ID, err: failure}
	report, err := New(r, mediatest.New(t, r, f.store, f.mon, nil), discardLog()).Sweep(ctx)
	if !errors.Is(err, failure) || !errors.Is(err, context.Canceled) || report.Complete {
		t.Fatalf("failed restore page = %+v, %v", report, err)
	}
	if cursor := f.restoreCursor(t); cursor == nil || *cursor != last.ID-1 {
		t.Fatalf("failed row blocked completed page: %v", cursor)
	}
	report, err = New(f.repo, mediatest.New(t, f.repo, f.store, f.mon, nil), discardLog()).Sweep(f.ctx)
	if err != nil || report.Restored != 1 || !report.Complete {
		t.Fatalf("later page = %+v, %v", report, err)
	}
	f.assertLive(t, last.ID)
	f.assertTombstonedMissing(t, first.ID)
	report, err = New(f.repo, mediatest.New(t, f.repo, f.store, f.mon, nil), discardLog()).Sweep(f.ctx)
	if err != nil || report.Restored != 1 {
		t.Fatalf("retry failed row = %+v, %v", report, err)
	}
	f.assertLive(t, first.ID)
}

type failedRestoreCursorRepo struct{ repository.Repository }

func (r failedRestoreCursorRepo) SetStorageRestoreCursor(ctx context.Context, cursor *int64) error {
	if cursor != nil && *cursor > 0 {
		return errors.New("restore cursor unavailable")
	}
	return r.Repository.SetStorageRestoreCursor(ctx, cursor)
}

func TestSweepRestoreCursorFailureKeepsPageReplayable(t *testing.T) {
	f := newFixture(t)
	v := f.seed(t, "cursor-write-failed", 1)
	if _, err := f.svc.Sweep(f.ctx); err != nil {
		t.Fatal(err)
	}
	report, err := New(failedRestoreCursorRepo{f.repo}, mediatest.New(t, failedRestoreCursorRepo{f.repo}, f.store, f.mon, nil), discardLog()).Sweep(f.ctx)
	if err == nil || report.Complete {
		t.Fatalf("failed cursor = %+v, %v", report, err)
	}
	if cursor := f.restoreCursor(t); cursor == nil || *cursor != 0 {
		t.Fatalf("failed cursor persisted progress: %v", cursor)
	}
	f.save(t, "videos/"+v.Filename+"-part01.mp4")
	report, err = New(f.repo, mediatest.New(t, f.repo, f.store, f.mon, nil), discardLog()).Sweep(f.ctx)
	if err != nil || !report.Complete || report.Restored != 1 {
		t.Fatalf("retry cursor = %+v, %v", report, err)
	}
	f.assertLive(t, v.ID)
}

type cancelRestoreInspectionStore struct {
	storage.Storage
	cancel context.CancelFunc
}

func (s cancelRestoreInspectionStore) Stat(ctx context.Context, path string) (storage.FileInfo, error) {
	s.cancel()
	return s.Storage.Stat(ctx, path)
}

func TestSweepReplaysInterruptedRestoreInspection(t *testing.T) {
	f := newFixture(t)
	v := f.seed(t, "interrupted-inspection", 1)
	if _, err := f.svc.Sweep(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.save(t, "videos/"+v.Filename+"-part01.mp4")
	// Start in restoration so cancellation exercises tombstone inspection.
	start := int64(0)
	if err := f.repo.SetStorageRestoreCursor(f.ctx, &start); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	store := cancelRestoreInspectionStore{Storage: f.store, cancel: cancel}
	report, err := New(f.repo, mediatest.New(t, f.repo, store, f.mon, nil), discardLog()).Sweep(ctx)
	if !errors.Is(err, context.Canceled) || report.Complete || report.Restored != 0 {
		t.Fatalf("interrupted inspection = %+v, %v", report, err)
	}
	if cursor := f.restoreCursor(t); cursor == nil || *cursor != 0 {
		t.Fatalf("incomplete inspection advanced cursor: %v", cursor)
	}
	f.assertTombstonedMissing(t, v.ID)
	report, err = New(f.repo, mediatest.New(t, f.repo, f.store, f.mon, nil), discardLog()).Sweep(f.ctx)
	if err != nil || !report.Complete || report.Restored != 1 {
		t.Fatalf("resumed inspection = %+v, %v", report, err)
	}
	f.assertLive(t, v.ID)
}
