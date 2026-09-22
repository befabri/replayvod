package storagescan

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/befabri/replayvod/server/internal/downloader/remux"
	"github.com/befabri/replayvod/server/internal/testutil/mediatest"

	"github.com/befabri/replayvod/server/internal/recordinglock"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/service/playbackcache"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

type publishingCacheRepo struct {
	repository.Repository
	publishing chan struct{}
	release    chan struct{}
	inspected  chan struct{}
	observed   sync.Once
}

type publishingCacheTx struct {
	repository.Repository
	owner *publishingCacheRepo
}

func (r *publishingCacheRepo) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error { return fn(publishingCacheTx{tx, r}) })
}
func (r publishingCacheTx) UpsertVideoPlaybackAsset(ctx context.Context, in *repository.VideoPlaybackAssetInput) (*repository.VideoPlaybackAsset, error) {
	if in.Status == repository.PlaybackAssetStatusReady {
		close(r.owner.publishing)
		<-r.owner.release
	}
	return r.Repository.UpsertVideoPlaybackAsset(ctx, in)
}

func (r *publishingCacheRepo) UpsertVideoPlaybackAsset(ctx context.Context, in *repository.VideoPlaybackAssetInput) (*repository.VideoPlaybackAsset, error) {
	if in.Status == repository.PlaybackAssetStatusReady {
		close(r.publishing)
		<-r.release
	}
	return r.Repository.UpsertVideoPlaybackAsset(ctx, in)
}

func (r *publishingCacheRepo) GetVideoPlaybackAsset(ctx context.Context, id int64) (*repository.VideoPlaybackAsset, error) {
	row, err := r.Repository.GetVideoPlaybackAsset(ctx, id)
	if err == nil && row.Status == repository.PlaybackAssetStatusBuilding {
		r.observed.Do(func() { close(r.inspected) })
	}
	return row, err
}

type preparedCacheRunner struct{}

func (preparedCacheRunner) Concat(_ context.Context, _, output string, _ remux.FileOperations) error {
	return os.WriteFile(output, []byte("playback copy"), 0600)
}

func awaitPublicationResult(t *testing.T, name string, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not stop", name)
		return nil
	}
}

func joinPublicationWorker(t *testing.T, name string, done <-chan struct{}) {
	t.Helper()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Errorf("%s did not finish during cleanup", name)
	}
}

// TestMissingScanPreservesCopyPublishedDuringInspection requires a fresh missing
// check under recording ownership after pending cache publication completes.
func TestMissingScanPreservesCopyPublishedDuringInspection(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(map[bool]string{false: "playback report", true: "bulk scan"}[automatic], func(t *testing.T) {
			f := newFixture(t)
			video := f.seed(t, "publishing-copy", 2, 1, 2)
			parts, err := f.repo.ListVideoParts(f.ctx, video.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, part := range parts {
				if err := f.repo.FinalizeVideoPart(f.ctx, &repository.VideoPartFinalize{ID: part.ID, DurationSeconds: 1, SizeBytes: 4, EndMediaSeq: 1}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.repo.UpsertPlaybackCacheConfig(f.ctx, true, 100, true); err != nil {
				t.Fatal(err)
			}
			repo := &publishingCacheRepo{Repository: f.repo, publishing: make(chan struct{}), release: make(chan struct{}), inspected: make(chan struct{})}
			locks := &recordinglock.Locks{}
			cache := playbackcache.New(repo, mediatest.New(t, repo, f.store, f.mon, locks), "", discardLog())
			cache.SetRunner(preparedCacheRunner{})
			t.Cleanup(cache.Close)
			scan := New(repo, mediatest.New(t, repo, f.store, f.mon, locks), discardLog())
			buildCtx, cancelBuild := context.WithCancel(t.Context())
			buildDone := make(chan error, 1)
			buildFinished := make(chan struct{})
			var scanFinished chan struct{}
			cancelScan := func() {}
			go func() { defer close(buildFinished); buildDone <- cache.BuildNow(buildCtx, video.ID) }()
			var release sync.Once
			unblock := func() { release.Do(func() { close(repo.release) }) }
			t.Cleanup(func() {
				cancelBuild()
				cancelScan()
				unblock()
				joinPublicationWorker(t, "cache build", buildFinished)
				joinPublicationWorker(t, "missing scan", scanFinished)
			})
			select {
			case <-repo.publishing:
			case err := <-buildDone:
				t.Fatalf("build never reached READY publication: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("build never reached publication")
			}
			// Remove source media after concat finishes but before its replacement becomes READY.
			for _, part := range parts {
				if err := f.store.Delete(f.ctx, storagekeys.Video(part.Filename)); err != nil {
					t.Fatal(err)
				}
			}
			scanCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			cancelScan = cancel
			scanDone := make(chan error, 1)
			scanFinished = make(chan struct{})
			go func() {
				defer close(scanFinished)
				if automatic {
					_, err := scan.Sweep(scanCtx)
					scanDone <- err
				} else {
					_, err := scan.MarkMissing(scanCtx, video.ID)
					scanDone <- err
				}
			}()
			// Both scans must wait while the READY transaction retains recording ownership.
			select {
			case err := <-scanDone:
				t.Fatalf("scan passed active publication: %v", err)
			case <-time.After(30 * time.Millisecond):
			}

			unblock()
			if err := awaitPublicationResult(t, "cache build", buildDone); err != nil {
				t.Fatal(err)
			}
			if err := awaitPublicationResult(t, "missing scan", scanDone); err != nil {
				t.Fatal(err)
			}
			f.assertLive(t, video.ID)
			asset, err := f.repo.GetVideoPlaybackAsset(f.ctx, video.ID)
			if err != nil || asset.Status != repository.PlaybackAssetStatusReady || asset.Filename == nil {
				t.Fatalf("published copy lost: %+v, %v", asset, err)
			}
			if !f.exists(t, storagekeys.Video(*asset.Filename)) {
				t.Fatal("published file lost")
			}
			if _, err := scan.Sweep(f.ctx); err != nil {
				t.Fatal(err)
			}
			f.assertLive(t, video.ID)
		})
	}
}

func TestMissingScanWaitHonorsCancellation(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(map[bool]string{false: "playback report", true: "bulk scan"}[automatic], func(t *testing.T) {
			f := newFixture(t)
			v := f.seed(t, "waiting-for-publication", 1)
			locks := &recordinglock.Locks{}
			unlock, err := locks.Lock(f.ctx, v.ID)
			if err != nil {
				t.Fatal(err)
			}
			release := sync.OnceFunc(unlock)
			scan := New(f.repo, mediatest.New(t, f.repo, f.store, f.mon, locks), discardLog())
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			stop := time.AfterFunc(50*time.Millisecond, cancel)
			defer stop.Stop()
			done := make(chan error, 1)
			finished := make(chan struct{})
			t.Cleanup(func() {
				cancel()
				release()
				joinPublicationWorker(t, "cancelled missing scan", finished)
			})
			go func() {
				defer close(finished)
				var scanErr error
				if automatic {
					_, scanErr = scan.Sweep(ctx)
				} else {
					_, scanErr = scan.MarkMissing(ctx, v.ID)
				}
				done <- scanErr
			}()
			err = awaitPublicationResult(t, "cancelled missing scan", done)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("blocked missing scan returned %v", err)
			}
			f.assertLive(t, v.ID)
		})
	}
}

// TestMissingSweepDeadlineKeepsIncompletePageRetryable runs in a bubble, whose
// clock reaches the deadline only once the sweep is durably blocked on
// publication, never while inspection is still doing I/O.
func TestMissingSweepDeadlineKeepsIncompletePageRetryable(t *testing.T) {
	synctest.Test(t, testMissingSweepDeadlineKeepsIncompletePageRetryable)
}

func testMissingSweepDeadlineKeepsIncompletePageRetryable(t *testing.T) {
	f := newFixture(t)
	v := f.seed(t, "publication-deadline", 1)
	locks := &recordinglock.Locks{}
	unlock, err := locks.Lock(f.ctx, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	release := sync.OnceFunc(unlock)
	scan := New(f.repo, mediatest.New(t, f.repo, f.store, f.mon, locks), discardLog())
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	type outcome struct {
		report Report
		err    error
	}
	done := make(chan outcome, 1)
	finished := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		release()
		joinPublicationWorker(t, "deadline missing scan", finished)
	})
	go func() {
		defer close(finished)
		report, err := scan.Sweep(ctx)
		done <- outcome{report, err}
	}()
	var report Report
	select {
	case result := <-done:
		report, err = result.report, result.err
	case <-time.After(5 * time.Second):
		t.Fatal("missing scan ignored its deadline while waiting for publication")
	}
	if err != nil || report.Complete || report.Scanned != 1 || report.Tombstoned != 0 {
		t.Fatalf("deadline after bulk progress = %+v, %v", report, err)
	}
	f.assertLive(t, v.ID)
	if cursor := f.scanCursor(t); cursor != 0 {
		t.Fatalf("unfinished page advanced cursor to %d", cursor)
	}
	release()
	report, err = scan.Sweep(f.ctx)
	if err != nil || !report.Complete || report.Tombstoned != 1 {
		t.Fatalf("retry unfinished page = %+v, %v", report, err)
	}
	f.assertTombstonedMissing(t, v.ID)
}
