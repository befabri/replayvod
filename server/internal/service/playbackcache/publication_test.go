package playbackcache

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/testutil/mediatest"
)

func TestCachePublicationCannotRecreateAssetAfterRetention(t *testing.T) {
	svc, repo, store, deleter, video := publicationFixture(t)
	publishing := make(chan struct{})
	release := make(chan struct{})
	buildDone := make(chan error, 1)
	repo.beforeReady = func() { close(publishing); <-release }
	finished := make(chan struct{})
	go func() { defer close(finished); buildDone <- svc.BuildNow(t.Context(), video.ID) }()
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() { unblock(); <-finished })
	select {
	case <-publishing:
	case err := <-buildDone:
		t.Fatalf("build never reached publication: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("build did not reach publication barrier")
	}
	// Publication keeps recording ownership through its guarded transaction.
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := deleter.DeleteRecording(ctx, video, repository.DeletionKindRetention); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("retention passed in-flight cache publication: %v", err)
	}
	unblock()
	if err := <-buildDone; err != nil {
		t.Fatal(err)
	}
	if err := deleter.DeleteRecording(t.Context(), video, repository.DeletionKindRetention); err != nil {
		t.Fatal(err)
	}
	assertPurged(t, repo, store, video.ID)
}

func TestCacheConcatDoesNotHoldRecordingOwnership(t *testing.T) {
	svc, repo, store, deleter, video := publicationFixture(t)
	concat := make(chan struct{})
	release := make(chan struct{})
	buildDone := make(chan error, 1)
	svc.SetRunner(&fakeRunner{body: []byte("playback"), beforeWrite: func() { close(concat); <-release }})
	finished := make(chan struct{})
	go func() { defer close(finished); buildDone <- svc.BuildNow(t.Context(), video.ID) }()
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() { unblock(); <-finished })
	select {
	case <-concat:
	case err := <-buildDone:
		t.Fatalf("build never reached concat: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("build did not reach concat barrier")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := deleter.DeleteRecording(ctx, video, repository.DeletionKindRetention); err != nil {
		t.Fatalf("retention waited for expensive concat: %v", err)
	}
	unblock()
	if err := <-buildDone; err != nil {
		t.Fatal(err)
	}
	assertPurged(t, repo, store, video.ID)
	if entries, err := os.ReadDir(svc.store.Scratch().Root()); err != nil || len(entries) != 0 {
		t.Fatalf("discarded concat left scratch data: %v, %v", entries, err)
	}
}

func TestCachePublicationWaitHonorsCancellation(t *testing.T) {
	svc, repo, store, _, video := publicationFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	owned := make(chan struct{})
	release := make(chan struct{})
	svc.SetRunner(&fakeRunner{body: []byte("playback"), beforeWrite: func() {
		unlock, err := mediatest.Locks(svc.store).Lock(ctx, video.ID)
		if err != nil {
			t.Error(err)
		} else {
			go func() { <-release; unlock() }()
		}
		close(owned)
	}})
	buildDone := make(chan error, 1)
	finished := make(chan struct{})
	go func() { defer close(finished); buildDone <- svc.BuildNow(ctx, video.ID) }()
	t.Cleanup(func() {
		cancel()
		close(release)
		<-finished
	})
	select {
	case <-owned:
	case <-time.After(5 * time.Second):
		t.Fatal("concat did not yield recording ownership")
	}
	cancel()
	select {
	case err := <-buildDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled publication wait returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled build waited for recording ownership to be released")
	}
	asset, err := repo.GetVideoPlaybackAsset(t.Context(), video.ID)
	if err != nil || asset.Status != repository.PlaybackAssetStatusBuilding {
		t.Fatalf("canceled build lost retryable state: %+v, %v", asset, err)
	}
	assertPlaybackFiles(t, svc, store)
	if entries, err := os.ReadDir(svc.store.Scratch().Root()); err != nil || len(entries) != 0 {
		t.Fatalf("canceled wait retained scratch output: %v, %v", entries, err)
	}
}

func TestPruneCannotDeleteCacheFromStaleSnapshot(t *testing.T) {
	for _, duringPublication := range []bool{false, true} {
		name := "rebuilt"
		if duringPublication {
			name = "publishing"
		}
		t.Run(name, func(t *testing.T) {
			svc, repo, store, _, video := publicationFixture(t)
			old := time.Now().Add(-time.Hour)
			filename, mime := "vod-42-playback.mp4", "video/mp4"
			size, duration := int64(8), 22.0
			if _, err := repo.UpsertVideoPlaybackAsset(t.Context(), &repository.VideoPlaybackAssetInput{
				VideoID: video.ID, Status: repository.PlaybackAssetStatusReady,
				Filename: &filename, MimeType: &mime, SizeBytes: &size, DurationSeconds: &duration,
				GeneratedAt: &old, LastAccessedAt: &old,
			}); err != nil {
				t.Fatal(err)
			}
			// The old ready row has lost its file, allowing a legitimate rebuild.
			// Pause pruning after it captures that old row and before it evicts.
			snapshot, releaseSnapshot := make(chan struct{}), make(chan struct{})
			var captured atomic.Bool
			repo.afterListReady = func() {
				if captured.CompareAndSwap(false, true) {
					close(snapshot)
					<-releaseSnapshot
				}
			}
			pruner := New(repo, cacheMedia(t, repo, store, svc.store, mediatest.Locks(svc.store)), "", svc.log)
			t.Cleanup(pruner.Close)
			pruner.capacityOverride = func(int64) (int64, bool) { return 0, true }
			svc.capacityOverride = func(int64) (int64, bool) { return 1 << 40, true }
			pruneCtx, cancelPrune := context.WithTimeout(t.Context(), time.Second)
			buildCtx, cancelBuild := context.WithCancel(t.Context())
			pruneDone, buildDone := make(chan error, 1), make(chan error, 1)
			pruneFinished, buildFinished := make(chan struct{}), make(chan struct{})
			publishing, releaseBuild := make(chan struct{}), make(chan struct{})
			var snapshotOnce, buildOnce sync.Once
			unblockSnapshot := func() { snapshotOnce.Do(func() { close(releaseSnapshot) }) }
			unblockBuild := func() { buildOnce.Do(func() { close(releaseBuild) }) }
			buildStarted := false
			t.Cleanup(func() {
				cancelPrune()
				cancelBuild()
				unblockSnapshot()
				unblockBuild()
				<-pruneFinished
				if buildStarted {
					<-buildFinished
				}
			})
			go func() { defer close(pruneFinished); pruneDone <- pruner.Prune(pruneCtx) }()
			select {
			case <-snapshot:
			case err := <-pruneDone:
				t.Fatalf("prune never captured a snapshot: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("prune did not reach snapshot barrier")
			}
			if duringPublication {
				repo.beforeReady = func() { close(publishing); <-releaseBuild }
			}
			buildStarted = true
			go func() { defer close(buildFinished); buildDone <- svc.BuildNow(buildCtx, video.ID) }()
			if duringPublication {
				select {
				case <-publishing:
				case err := <-buildDone:
					t.Fatalf("build never reached publication: %v", err)
				case <-time.After(5 * time.Second):
					t.Fatal("build did not reach publication barrier")
				}
				unblockSnapshot()
				select {
				case err := <-pruneDone:
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Errorf("prune crossed cache publication: %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("prune ignored canceled publication wait")
				}
				unblockBuild()
			}
			select {
			case err := <-buildDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("build did not finish after releasing publication")
			}
			if !duringPublication {
				unblockSnapshot()
				select {
				case err := <-pruneDone:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("prune did not finish after releasing snapshot")
				}
			}
			asset, err := repo.GetVideoPlaybackAsset(t.Context(), video.ID)
			if err != nil || asset.Status != repository.PlaybackAssetStatusReady {
				t.Fatalf("stale prune discarded rebuilt row: %+v, %v", asset, err)
			}
			path, err := store.LocalPath(storagekeys.Video(*asset.Filename))
			if err != nil {
				t.Fatal(err)
			}
			if bytes, err := os.ReadFile(path); err != nil || string(bytes) != "playback" {
				t.Fatalf("stale prune discarded rebuilt artifact: %q, %v", bytes, err)
			}
		})
	}
}
