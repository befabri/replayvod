package playbackcache

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/recordinglock"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/service/retention"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/testdb"
)

type publicationRepo struct {
	repository.Repository
	beforeReady    func()
	afterListReady func()
}

func (r *publicationRepo) GetServerSettings(context.Context) (*repository.ServerSettings, error) {
	return &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 100}, nil
}

func (r *publicationRepo) UpsertVideoPlaybackAsset(ctx context.Context, input *repository.VideoPlaybackAssetInput) (*repository.VideoPlaybackAsset, error) {
	if input.Status == repository.PlaybackAssetStatusReady && r.beforeReady != nil {
		r.beforeReady()
	}
	return r.Repository.UpsertVideoPlaybackAsset(ctx, input)
}

func (r *publicationRepo) ListReadyVideoPlaybackAssets(ctx context.Context) ([]repository.VideoPlaybackAsset, error) {
	entries, err := r.Repository.ListReadyVideoPlaybackAssets(ctx)
	if err == nil && r.afterListReady != nil {
		r.afterListReady()
	}
	return entries, err
}

func publicationFixture(t *testing.T) (*Service, *publicationRepo, *storage.LocalStorage, *retention.Service, *repository.Video) {
	ctx := t.Context()
	repo := &publicationRepo{Repository: sqliteadapter.New(testdb.NewSQLiteDB(t))}
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "b", BroadcasterLogin: "b", BroadcasterName: "B"}); err != nil {
		t.Fatal(err)
	}
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{JobID: "job", Filename: "vod-42", DisplayName: "B", BroadcasterID: "b", Status: repository.VideoStatusDone, Quality: repository.QualityHigh, RecordingType: repository.RecordingTypeVideo})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range compatibleParts() {
		row, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{VideoID: v.ID, PartIndex: part.PartIndex, Filename: part.Filename, Quality: part.Quality, FPS: part.FPS, Codec: part.Codec, SegmentFormat: repository.SegmentFormatFMP4})
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.FinalizeVideoPart(ctx, &repository.VideoPartFinalize{ID: row.ID, DurationSeconds: part.DurationSeconds, SizeBytes: part.SizeBytes, EndMediaSeq: 1}); err != nil {
			t.Fatal(err)
		}
		if err := store.Save(ctx, storagekeys.Video(part.Filename), strings.NewReader("part")); err != nil {
			t.Fatal(err)
		}
	}
	log := slog.New(slog.DiscardHandler)
	locks := &recordinglock.Locks{}
	gate := gateFunc(func() error { return nil })
	deleter := retention.New(repo, store, gate, log, retention.WithRecordingLocks(locks))
	svc := New(repo, store, gate, t.TempDir(), "", log, WithRecordingLocks(locks))
	svc.SetRunner(&fakeRunner{body: []byte("playback")})
	t.Cleanup(svc.Close)
	return svc, repo, store, deleter, v
}

func assertPurged(t *testing.T, repo repository.Repository, store storage.Storage, videoID int64) {
	t.Helper()
	v, err := repo.GetVideo(t.Context(), videoID)
	if err != nil || v.DeletedAt == nil {
		t.Fatalf("recording was not deleted: %+v, %v", v, err)
	}
	if asset, err := repo.GetVideoPlaybackAsset(t.Context(), videoID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("publication recreated asset after retention: %+v, %v", asset, err)
	}
	if exists, err := store.Exists(t.Context(), storagekeys.Video("vod-42-playback.mp4")); err != nil || exists {
		t.Fatalf("publication recreated artifact after retention: exists=%v, %v", exists, err)
	}
}

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
	// The barrier is before the actual SQLite upsert, so no database lock
	// can incidentally serialize this purge. Only recording ownership can.
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
	if entries, err := os.ReadDir(svc.scratch); err != nil || len(entries) != 0 {
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
		unlock, err := svc.recordingLocks.Lock(ctx, video.ID)
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
	if exists, err := store.Exists(t.Context(), storagekeys.Video("vod-42-playback.mp4")); err != nil || exists {
		t.Fatalf("canceled build published an artifact: exists=%v, %v", exists, err)
	}
	if entries, err := os.ReadDir(svc.scratch); err != nil || len(entries) != 0 {
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
			pruner := New(repo, store, svc.gate, t.TempDir(), "", svc.log, WithRecordingLocks(svc.recordingLocks))
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
			path, err := store.LocalPath(storagekeys.Video(filename))
			if err != nil {
				t.Fatal(err)
			}
			if bytes, err := os.ReadFile(path); err != nil || string(bytes) != "playback" {
				t.Fatalf("stale prune discarded rebuilt artifact: %q, %v", bytes, err)
			}
		})
	}
}
