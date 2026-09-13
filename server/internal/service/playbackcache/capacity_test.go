package playbackcache

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
	"github.com/befabri/replayvod/server/internal/testutil/mediatest"
)

type fullStorageProbe struct {
	*storage.LocalStorage
	cause error
}

func (s fullStorageProbe) ProbeWrite(context.Context) error {
	return &os.PathError{Op: "create", Path: s.Root, Err: s.cause}
}

func TestFullDiskPermitsCachePruningAndPausesBuilds(t *testing.T) {
	for _, cause := range []error{syscall.ENOSPC, syscall.EDQUOT} {
		for _, reconcile := range []bool{false, true} {
			name := "prune"
			if reconcile {
				name = "reconcile"
			}
			t.Run(cause.Error()+"/"+name, func(t *testing.T) {
				svc, repo, local, runner := cacheFixture(t, nil)
				ctx := t.Context()
				attached, err := storage.Attach(ctx, local, "")
				if err != nil {
					t.Fatal(err)
				}
				probe := fullStorageProbe{local, cause}
				mediatest.SetGate(svc.store, gateFunc(func() error { return storage.Ready(ctx, probe, attached.ID) }))
				if err := svc.BuildNow(ctx, 42); !errors.Is(err, storage.ErrFull) {
					t.Fatalf("build on full storage: %v", err)
				}
				if runner.calls != 0 || repo.asset != nil {
					t.Fatal("full disk admitted a build")
				}
				name := "vod-42-playback.mp4"
				size := int64(14)
				if err := local.Save(ctx, storagekeys.Video(name), strings.NewReader("existing cache")); err != nil {
					t.Fatal(err)
				}
				repo.ready = []repository.VideoPlaybackAsset{{VideoID: 42, Status: repository.PlaybackAssetStatusReady, Filename: &name, SizeBytes: &size}}
				svc.fsStat = func(string) (int64, int64, error) { return 1000, 0, nil }
				prune := svc.Prune
				if reconcile {
					prune = svc.Reconcile
				}
				if err := prune(ctx); err != nil {
					t.Fatalf("full disk blocked pruning: %v", err)
				}
				if len(repo.ready) != 0 {
					t.Fatal("prune retained cache row")
				}
				if exists, err := local.Exists(ctx, storagekeys.Video(name)); err != nil || exists {
					t.Fatalf("prune failed to free cache bytes: exists=%v err=%v", exists, err)
				}
			})
		}
	}
}

func TestBuildThatFillsStorageReclaimsItsUnpublishedArtifact(t *testing.T) {
	var gateErr error
	svc, repo, local, runner := cacheFixture(t, gateFunc(func() error { return gateErr }))
	runner.beforeWrite = func() { gateErr = storage.ErrFull }
	if err := svc.BuildNow(t.Context(), 42); !errors.Is(err, storage.ErrFull) {
		t.Fatalf("full disk build: %v", err)
	}
	if runner.calls != 1 || repo.asset == nil || repo.asset.Status == repository.PlaybackAssetStatusReady {
		t.Fatalf("full disk finalized a playback build: calls=%d asset=%+v", runner.calls, repo.asset)
	}
	assertPlaybackFiles(t, svc, local)
}

// stubStore is a non-LocalStorage storage.Storage so capacity() takes the
// object-storage branch. capacity never calls any of these on that path.
type stubStore struct{}

func (stubStore) Save(context.Context, string, io.Reader) error { return nil }

func (stubStore) Open(context.Context, string) (io.ReadSeekCloser, error) {
	return nil, nil
}

func (stubStore) Delete(context.Context, string) error { return nil }

func (stubStore) Exists(context.Context, string) (bool, error) {
	return false, nil
}

func (stubStore) Stat(context.Context, string) (storage.FileInfo, error) {
	return storage.FileInfo{}, nil
}

// TestCapacity checks local free-space limits and object-storage library budgets
// through deterministic capacity inputs.
func TestCapacity(t *testing.T) {
	ctx := context.Background()
	local, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}

	t.Run("local percent binds below ceiling", func(t *testing.T) {
		svc := New(&fakeRepo{}, cacheMedia(t, &fakeRepo{}, local, nil, nil), "", nil)
		svc.fsStat = func(string) (int64, int64, error) { return 1000, 900, nil }
		b, err := svc.capacity(ctx, 10, 0)
		if err != nil || !b.known || b.configured != 100 || b.current != 100 {
			t.Fatalf("capacity = %+v (err %v), want configured/current 100", b, err)
		}
	})

	t.Run("local free-space ceiling binds below percent", func(t *testing.T) {
		svc := New(&fakeRepo{}, cacheMedia(t, &fakeRepo{}, local, nil, nil), "", nil)
		svc.fsStat = func(string) (int64, int64, error) { return 1000, 80, nil }
		b, _ := svc.capacity(ctx, 50, 0)
		if b.configured != 500 || b.current != 30 {
			t.Fatalf("capacity = %+v, want configured 500, current 30", b)
		}
	})

	t.Run("disk pressure clamps current to zero but not configured", func(t *testing.T) {
		svc := New(&fakeRepo{}, cacheMedia(t, &fakeRepo{}, local, nil, nil), "", nil)
		svc.fsStat = func(string) (int64, int64, error) { return 1000, 10, nil }
		b, _ := svc.capacity(ctx, 50, 0)
		if !b.known || b.current != 0 || b.configured != 500 {
			t.Fatalf("capacity = %+v, want current 0, configured 500 under disk pressure", b)
		}
	})

	t.Run("current cache bytes raise the ceiling", func(t *testing.T) {
		svc := New(&fakeRepo{}, cacheMedia(t, &fakeRepo{}, local, nil, nil), "", nil)
		svc.fsStat = func(string) (int64, int64, error) { return 1000, 100, nil }
		b, _ := svc.capacity(ctx, 90, 200)
		if b.current != 250 {
			t.Fatalf("capacity current = %d, want 250 (current + avail - reserve)", b.current)
		}
	})

	t.Run("object storage uses library size with no free-space clamp", func(t *testing.T) {
		svc := New(&fakeRepo{statsTotal: 1000}, cacheMedia(t, &fakeRepo{statsTotal: 1000}, stubStore{}, nil, nil), "", nil)
		b, err := svc.capacity(ctx, 10, 0)
		if err != nil || !b.known || b.configured != 100 || b.current != 100 || b.buildHeadroom != 100 {
			t.Fatalf("capacity = %+v (err %v), want configured/current/buildHeadroom 100", b, err)
		}
	})

	t.Run("buildHeadroom excludes existing cache bytes (free space only)", func(t *testing.T) {
		svc := New(&fakeRepo{}, cacheMedia(t, &fakeRepo{}, local, nil, nil), "", nil)
		svc.fsStat = func(string) (int64, int64, error) { return 1000, 100, nil }
		// Existing cache bytes are reclaimable by pruning but unavailable to new builds.
		b, _ := svc.capacity(ctx, 90, 200)
		if b.current != 250 {
			t.Fatalf("current = %d, want 250", b.current)
		}
		if b.buildHeadroom != 50 {
			t.Fatalf("buildHeadroom = %d, want 50 (free - reserve, NOT incl. existing cache)", b.buildHeadroom)
		}
	})
}

// TestBuildNowDefersOversizeEstimate preserves retry after a budget increase.
func TestBuildNowDefersOversizeEstimate(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewLocal(t.TempDir())
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 10},
		video:    &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
		parts:    compatibleParts(), // 8 bytes of parts
	}
	savePartFiles(t, ctx, store, repo.parts)
	runner := &fakeRunner{body: []byte("playback")}
	svc := New(repo, cacheMedia(t, repo, store, nil, nil), "", nil)
	svc.SetRunner(runner)
	svc.capacityOverride = func(int64) (int64, bool) { return 5, true } // cap < 8

	if err := svc.BuildNow(ctx, 42); err != nil {
		t.Fatalf("BuildNow: %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("concat calls = %d, want 0 (estimate exceeds cap)", runner.calls)
	}
	if repo.asset != nil {
		t.Fatalf("asset = %#v, want no row (deferred for retry, not terminal)", repo.asset)
	}
}

// TestBuildNowMarksOversizeOutputUnavailable prevents repeated oversized builds.
func TestBuildNowMarksOversizeOutputUnavailable(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewLocal(t.TempDir())
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 10},
		video:    &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
		parts:    compatibleParts(),
	}
	savePartFiles(t, ctx, store, repo.parts)
	runner := &fakeRunner{body: make([]byte, 100)}
	svc := New(repo, cacheMedia(t, repo, store, nil, nil), "", nil)
	svc.SetRunner(runner)
	svc.capacityOverride = func(int64) (int64, bool) { return 20, true }

	if err := svc.BuildNow(ctx, 42); err != nil {
		t.Fatalf("BuildNow: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("concat calls = %d, want 1", runner.calls)
	}
	if repo.asset == nil || repo.asset.Status != repository.PlaybackAssetStatusUnavailable {
		t.Fatalf("asset = %#v, want unavailable (terminal) after oversize output", repo.asset)
	}
	assertPlaybackFiles(t, svc, store)
}

// TestBuildNowDefersUnderDiskPressureThenRecovers checks that temporary disk
// pressure cannot permanently disable playback generation.
func TestBuildNowDefersUnderDiskPressureThenRecovers(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewLocal(t.TempDir())
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 10},
		video:    &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
		parts:    compatibleParts(),
	}
	savePartFiles(t, ctx, store, repo.parts)
	runner := &fakeRunner{body: []byte("playback")}
	svc := New(repo, cacheMedia(t, repo, store, nil, nil), "", nil)
	svc.SetRunner(runner)

	avail := int64(10)
	svc.fsStat = func(string) (int64, int64, error) { return 1000, avail, nil }

	if err := svc.BuildNow(ctx, 42); err != nil {
		t.Fatalf("BuildNow (deferred): %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("concat calls = %d, want 0 (no room to build)", runner.calls)
	}
	if repo.asset != nil {
		t.Fatalf("deferred build recorded %#v; want no row so the next play can retry", repo.asset)
	}

	avail = 900
	if err := svc.BuildNow(ctx, 42); err != nil {
		t.Fatalf("BuildNow (after space freed): %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("concat calls = %d, want 1 after space freed", runner.calls)
	}
	if repo.asset == nil || repo.asset.Status != repository.PlaybackAssetStatusReady {
		t.Fatalf("asset = %#v, want ready after recovery", repo.asset)
	}
}

// TestBuildNowDefersWhenArtifactExceedsFreeSpace checks that occupied cache bytes
// cannot fund a build before eviction has freed them.
func TestBuildNowDefersWhenArtifactExceedsFreeSpace(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewLocal(t.TempDir())
	fps := 60.0
	cachedName := "old-playback.mp4"
	cachedSize := int64(500)
	parts := []repository.VideoPart{
		{PartIndex: 1, Filename: "vod-42-01.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264, SegmentFormat: "mp4", DurationSeconds: 10, SizeBytes: 150},
		{PartIndex: 2, Filename: "vod-42-02.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264, SegmentFormat: "mp4", DurationSeconds: 12, SizeBytes: 150},
	}
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 90},
		video:    &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
		parts:    parts,
		ready: []repository.VideoPlaybackAsset{
			{VideoID: 1, Status: repository.PlaybackAssetStatusReady, Filename: &cachedName, SizeBytes: &cachedSize, LastAccessedAt: ptrTime(time.Now())},
		},
	}
	savePartFiles(t, ctx, store, parts)
	runner := &fakeRunner{body: []byte("playback")}
	svc := New(repo, cacheMedia(t, repo, store, nil, nil), "", nil)
	svc.SetRunner(runner)
	svc.fsStat = func(string) (int64, int64, error) { return 1000, 100, nil }

	if err := svc.BuildNow(ctx, 42); err != nil {
		t.Fatalf("BuildNow: %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("concat calls = %d, want 0 (artifact doesn't fit free space — would ENOSPC)", runner.calls)
	}
	if repo.asset != nil {
		t.Fatalf("asset = %#v, want no row (deferred, retryable)", repo.asset)
	}
}
