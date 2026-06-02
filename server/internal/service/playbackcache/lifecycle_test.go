package playbackcache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

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

// TestCapacity exercises the budget math directly through the fsStat seam — the
// local free-space ceiling, the negative-clamp under disk pressure, and the
// object-storage library-size path. Without this a sign error or an
// off-by-diskReserveFraction mistake would ship green.
func TestCapacity(t *testing.T) {
	ctx := context.Background()
	local, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}

	t.Run("local percent binds below ceiling", func(t *testing.T) {
		svc := New(&fakeRepo{}, local, t.TempDir(), "", nil)
		svc.fsStat = func(string) (int64, int64, error) { return 1000, 900, nil }
		// configured = 1000/100*10 = 100; ceiling = 0 + 900 - 1000/20(=50) = 850.
		b, err := svc.capacity(ctx, 10, 0)
		if err != nil || !b.known || b.configured != 100 || b.current != 100 {
			t.Fatalf("capacity = %+v (err %v), want configured/current 100", b, err)
		}
	})

	t.Run("local free-space ceiling binds below percent", func(t *testing.T) {
		svc := New(&fakeRepo{}, local, t.TempDir(), "", nil)
		svc.fsStat = func(string) (int64, int64, error) { return 1000, 80, nil }
		// configured = 1000/100*50 = 500; ceiling = 0 + 80 - 50 = 30 -> current 30,
		// configured stays 500 (the artifact-size cap is unchanged by free space).
		b, _ := svc.capacity(ctx, 50, 0)
		if b.configured != 500 || b.current != 30 {
			t.Fatalf("capacity = %+v, want configured 500, current 30", b)
		}
	})

	t.Run("disk pressure clamps current to zero but not configured", func(t *testing.T) {
		svc := New(&fakeRepo{}, local, t.TempDir(), "", nil)
		svc.fsStat = func(string) (int64, int64, error) { return 1000, 10, nil }
		// ceiling = 0 + 10 - 50 = -40 -> current clamped to 0; configured stays
		// positive so BuildNow defers (transient) rather than failing permanently.
		b, _ := svc.capacity(ctx, 50, 0)
		if !b.known || b.current != 0 || b.configured != 500 {
			t.Fatalf("capacity = %+v, want current 0, configured 500 under disk pressure", b)
		}
	})

	t.Run("current cache bytes raise the ceiling", func(t *testing.T) {
		svc := New(&fakeRepo{}, local, t.TempDir(), "", nil)
		svc.fsStat = func(string) (int64, int64, error) { return 1000, 100, nil }
		// configured = 900; ceiling = current(200) + 100 - 50 = 250 -> current 250.
		b, _ := svc.capacity(ctx, 90, 200)
		if b.current != 250 {
			t.Fatalf("capacity current = %d, want 250 (current + avail - reserve)", b.current)
		}
	})

	t.Run("object storage uses library size with no free-space clamp", func(t *testing.T) {
		svc := New(&fakeRepo{statsTotal: 1000}, stubStore{}, t.TempDir(), "", nil)
		// 1000/100*10 = 100, independent of fsStat (not a local store).
		b, err := svc.capacity(ctx, 10, 0)
		if err != nil || !b.known || b.configured != 100 || b.current != 100 || b.buildHeadroom != 100 {
			t.Fatalf("capacity = %+v (err %v), want configured/current/buildHeadroom 100", b, err)
		}
	})

	t.Run("buildHeadroom excludes existing cache bytes (free space only)", func(t *testing.T) {
		svc := New(&fakeRepo{}, local, t.TempDir(), "", nil)
		svc.fsStat = func(string) (int64, int64, error) { return 1000, 100, nil }
		// configured = 900; reserve = 50; cache currently holds 200.
		// current (Prune target) re-adds reclaimable cache bytes: min(900, 200+100-50)=250.
		// buildHeadroom (new-build admission) uses free space ONLY: min(900, 100-50)=50.
		// The split is the ENOSPC fix: a new build must fit in real free space, not
		// the total cap that counts the not-yet-freed existing cache.
		b, _ := svc.capacity(ctx, 90, 200)
		if b.current != 250 {
			t.Fatalf("current = %d, want 250", b.current)
		}
		if b.buildHeadroom != 50 {
			t.Fatalf("buildHeadroom = %d, want 50 (free - reserve, NOT incl. existing cache)", b.buildHeadroom)
		}
	})
}

func compatibleParts() []repository.VideoPart {
	fps := 60.0
	return []repository.VideoPart{
		{PartIndex: 1, Filename: "vod-42-01.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264, SegmentFormat: "mp4", DurationSeconds: 10, SizeBytes: 4},
		{PartIndex: 2, Filename: "vod-42-02.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264, SegmentFormat: "mp4", DurationSeconds: 12, SizeBytes: 4},
	}
}

func savePartFiles(t *testing.T, ctx context.Context, store *storage.LocalStorage, parts []repository.VideoPart) {
	t.Helper()
	for _, p := range parts {
		if err := store.Save(ctx, storagekeys.Video(p.Filename), bytes.NewReader([]byte("part"))); err != nil {
			t.Fatalf("save %s: %v", p.Filename, err)
		}
	}
}

// A recording whose estimate exceeds the cap is DEFERRED (no row), not marked
// terminally unavailable, so the periodic reconciler rebuilds it once the owner
// raises max_percent (or, on object storage, the library grows).
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
	svc := New(repo, store, t.TempDir(), "", nil)
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

// A built artifact that overshoots the cap is dropped and marked terminally
// unavailable (NOT failed): ffmpeg would reproduce the same oversize output, so
// a retryable verdict would relaunch the same doomed concat every cooldown. The
// retryable cap gate is the pre-build estimate defer, not this post-build guard.
func TestBuildNowMarksOversizeOutputUnavailable(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewLocal(t.TempDir())
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 10},
		video:    &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
		parts:    compatibleParts(),
	}
	savePartFiles(t, ctx, store, repo.parts)
	// Estimate (8) is under cap (20) so the build runs, but the produced file
	// (100 bytes) exceeds the cap and must be rejected + deleted.
	runner := &fakeRunner{body: make([]byte, 100)}
	svc := New(repo, store, t.TempDir(), "", nil)
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
	if exists, _ := store.Exists(ctx, storagekeys.Video("vod-42-playback.mp4")); exists {
		t.Fatal("oversize artifact was not deleted")
	}
}

func TestBuildNowSkipsWhenReadyArtifactExists(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewLocal(t.TempDir())
	name := "vod-42-playback.mp4"
	if err := store.Save(ctx, storagekeys.Video(name), bytes.NewReader([]byte("existing"))); err != nil {
		t.Fatalf("seed artifact: %v", err)
	}
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 10},
		video:    &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
		parts:    compatibleParts(),
		asset:    &repository.VideoPlaybackAsset{VideoID: 42, Status: repository.PlaybackAssetStatusReady, Filename: &name},
	}
	runner := &fakeRunner{body: []byte("playback")}
	svc := New(repo, store, t.TempDir(), "", nil)
	svc.SetRunner(runner)

	if err := svc.BuildNow(ctx, 42); err != nil {
		t.Fatalf("BuildNow: %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("concat calls = %d, want 0 (already ready)", runner.calls)
	}
	if len(repo.events) != 0 {
		t.Fatalf("events = %v, want none (idempotent skip)", repo.events)
	}
}

func TestBuildNowMarksMissingPartUnavailable(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewLocal(t.TempDir())
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 10},
		video:    &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
		parts:    compatibleParts(),
	}
	// Part files are intentionally NOT saved: a row whose source file is gone
	// must be marked permanently unavailable, not retried by the reconciler.
	runner := &fakeRunner{body: []byte("playback")}
	svc := New(repo, store, t.TempDir(), "", nil)
	svc.SetRunner(runner)
	svc.capacityOverride = func(int64) (int64, bool) { return 1 << 40, true }

	if err := svc.BuildNow(ctx, 42); err != nil {
		t.Fatalf("BuildNow: %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("concat calls = %d, want 0 (source part missing)", runner.calls)
	}
	if repo.asset == nil || repo.asset.Status != repository.PlaybackAssetStatusUnavailable {
		t.Fatalf("asset = %#v, want unavailable", repo.asset)
	}
}

// A near-full disk must DEFER the build (transient), not mark it permanently
// unavailable — otherwise enabling the feature on a full-disk library kills it
// forever, with no recovery when space is later freed.
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
	svc := New(repo, store, t.TempDir(), "", nil)
	svc.SetRunner(runner)

	// Near-full disk: avail(10) < reserve(total/20 = 50), so the current budget
	// is 0 while the configured cap (100) easily fits the 8-byte estimate.
	avail := int64(10)
	svc.fsStat = func(string) (int64, int64, error) { return 1000, avail, nil }

	if err := svc.BuildNow(ctx, 42); err != nil {
		t.Fatalf("BuildNow (deferred): %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("concat calls = %d, want 0 (no room to build)", runner.calls)
	}
	if repo.asset != nil {
		t.Fatalf("deferred build recorded %#v; want no row so the reconciler retries", repo.asset)
	}

	// Free space; the reconciler's next retry now succeeds.
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

// An active cache (enabled, maxPercent>0) with a zero current budget — the disk
// is full of recordings — evicts everything: the cache yields to recordings.
// This is the intentional counterpart to TestPruneLeavesCacheWhenDisabled,
// where maxPercent=0 means "off" and the cache is left untouched.
func TestPruneEvictsActiveCacheUnderDiskPressure(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewLocal(t.TempDir())
	name := "rec-playback.mp4"
	if err := store.Save(ctx, storagekeys.Video(name), bytes.NewReader([]byte("artifact"))); err != nil {
		t.Fatalf("save: %v", err)
	}
	size := int64(len("artifact"))
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheMaxPercent: 10},
		ready: []repository.VideoPlaybackAsset{
			{VideoID: 1, Status: repository.PlaybackAssetStatusReady, Filename: &name, SizeBytes: &size, LastAccessedAt: ptrTime(time.Now())},
		},
	}
	svc := New(repo, store, t.TempDir(), "", nil)
	svc.capacityOverride = func(int64) (int64, bool) { return 0, true } // active but no room

	if err := svc.Prune(ctx); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(repo.ready) != 0 {
		t.Fatalf("ready = %#v, want empty (cache yields to recordings)", repo.ready)
	}
	if exists, _ := store.Exists(ctx, storagekeys.Video(name)); exists {
		t.Fatal("artifact not evicted under disk pressure")
	}
}

// If retention soft-deletes and purges the video while ffmpeg runs, the build
// must NOT commit a ready row + artifact file afterward — both would leak
// (neither retention nor the reconciler revisits soft-deleted videos).
func TestBuildNowDropsArtifactWhenVideoDeletedMidBuild(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewLocal(t.TempDir())
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 10},
		video:    &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
		parts:    compatibleParts(),
	}
	savePartFiles(t, ctx, store, repo.parts)
	deletedAt := time.Now().UTC()
	runner := &fakeRunner{body: []byte("playback"), beforeWrite: func() {
		// A retention sweep soft-deletes the video mid-concat.
		repo.video.DeletedAt = &deletedAt
	}}
	svc := New(repo, store, t.TempDir(), "", nil)
	svc.SetRunner(runner)
	svc.capacityOverride = func(int64) (int64, bool) { return 1 << 40, true }

	if err := svc.BuildNow(ctx, 42); err != nil {
		t.Fatalf("BuildNow: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("concat calls = %d, want 1 (build ran before the delete was observed)", runner.calls)
	}
	if repo.asset != nil {
		t.Fatalf("left row %#v for a soft-deleted video; want none (no dangling row)", repo.asset)
	}
	if exists, _ := store.Exists(ctx, storagekeys.Video("vod-42-playback.mp4")); exists {
		t.Fatal("leaked artifact file for a soft-deleted video")
	}
}

// A transient error on the pre-commit re-check must fail SAFE: drop the
// artifact and leave the 'building' row for the reconciler, never commit a
// ready row for an unverified video.
func TestBuildNowFailsSafeOnRecheckError(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewLocal(t.TempDir())
	repo := &fakeRepo{
		settings:   &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 10},
		video:      &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
		parts:      compatibleParts(),
		recheckErr: errors.New("db blip"),
	}
	savePartFiles(t, ctx, store, repo.parts)
	runner := &fakeRunner{body: []byte("playback")}
	svc := New(repo, store, t.TempDir(), "", nil)
	svc.SetRunner(runner)
	svc.capacityOverride = func(int64) (int64, bool) { return 1 << 40, true }

	if err := svc.BuildNow(ctx, 42); err == nil {
		t.Fatal("BuildNow succeeded; want error from the transient re-check")
	}
	if repo.asset == nil || repo.asset.Status != repository.PlaybackAssetStatusBuilding {
		t.Fatalf("asset = %#v, want building left for the reconciler (not committed ready)", repo.asset)
	}
	if exists, _ := store.Exists(ctx, storagekeys.Video("vod-42-playback.mp4")); exists {
		t.Fatal("artifact not dropped on fail-safe re-check")
	}
}

// existsErrStore is a local store whose Exists always errors, to exercise the
// idempotency probe's transient-error handling.
type existsErrStore struct {
	*storage.LocalStorage
	err error
}

func (s existsErrStore) Exists(context.Context, string) (bool, error) {
	return false, s.err
}

// A transient Exists error on a ready artifact must NOT trigger a rebuild —
// "couldn't check" isn't "missing". The healthy artifact is left alone.
func TestBuildNowSkipsRebuildOnExistsError(t *testing.T) {
	ctx := context.Background()
	local, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	name := "vod-42-playback.mp4"
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 10},
		video:    &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
		parts:    compatibleParts(),
		asset:    &repository.VideoPlaybackAsset{VideoID: 42, Status: repository.PlaybackAssetStatusReady, Filename: &name},
	}
	runner := &fakeRunner{body: []byte("playback")}
	svc := New(repo, existsErrStore{LocalStorage: local, err: errors.New("HeadObject 500")}, t.TempDir(), "", nil)
	svc.SetRunner(runner)

	if err := svc.BuildNow(ctx, 42); err != nil {
		t.Fatalf("BuildNow: %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("concat calls = %d, want 0 (transient probe error must not rebuild)", runner.calls)
	}
	if len(repo.events) != 0 {
		t.Fatalf("events = %v, want none (no rebuild)", repo.events)
	}
}

// A build must be admitted against actual free space, not the total-cache cap.
// Here the artifact fits the cap (which counts the reclaimable existing cache)
// but NOT free space, so building it would ENOSPC before the post-build prune
// could reclaim anything. It must defer.
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
		// Existing cache occupies 500 bytes (counts toward the Prune target, but is
		// NOT free until the post-build prune).
		ready: []repository.VideoPlaybackAsset{
			{VideoID: 1, Status: repository.PlaybackAssetStatusReady, Filename: &cachedName, SizeBytes: &cachedSize, LastAccessedAt: ptrTime(time.Now())},
		},
	}
	savePartFiles(t, ctx, store, parts)
	runner := &fakeRunner{body: []byte("playback")}
	svc := New(repo, store, t.TempDir(), "", nil)
	svc.SetRunner(runner)
	// total=1000, avail=100 -> reserve=50. configured=900.
	// current = min(900, 500+100-50) = 550  (a 300-byte artifact "fits" the cap)
	// buildHeadroom = min(900, 100-50) = 50  (but only 50 bytes are actually free)
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

// If the video is soft-deleted mid-build AND the artifact overshoots the cap,
// the freshness re-check must win over the oversize verdict — otherwise an
// 'unavailable' row dangles for a deleted video that nothing reclaims.
func TestBuildNowDeletedMidBuildBeatsOversizeVerdict(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewLocal(t.TempDir())
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 10},
		video:    &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
		parts:    compatibleParts(),
	}
	savePartFiles(t, ctx, store, repo.parts)
	deletedAt := time.Now().UTC()
	// Output (100) exceeds the cap (20) AND the video is soft-deleted mid-build.
	runner := &fakeRunner{body: make([]byte, 100), beforeWrite: func() { repo.video.DeletedAt = &deletedAt }}
	svc := New(repo, store, t.TempDir(), "", nil)
	svc.SetRunner(runner)
	svc.capacityOverride = func(int64) (int64, bool) { return 20, true }

	if err := svc.BuildNow(ctx, 42); err != nil {
		t.Fatalf("BuildNow: %v", err)
	}
	if repo.asset != nil {
		t.Fatalf("left row %#v for a soft-deleted video; the freshness re-check must precede the oversize verdict", repo.asset)
	}
	for _, ev := range repo.events {
		if ev == repository.PlaybackAssetStatusUnavailable {
			t.Fatal("recorded an unavailable row for a soft-deleted video")
		}
	}
	if exists, _ := store.Exists(ctx, storagekeys.Video("vod-42-playback.mp4")); exists {
		t.Fatal("leaked artifact for a soft-deleted video")
	}
}

// When the LRU victim's row delete fails, Prune must stop, not skip ahead and
// evict a NEWER entry to compensate (which would invert LRU).
func TestPruneStopsOnRowDeleteFailure(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewLocal(t.TempDir())
	oldName, newName := "old-playback.mp4", "new-playback.mp4"
	for _, n := range []string{oldName, newName} {
		if err := store.Save(ctx, storagekeys.Video(n), bytes.NewReader([]byte("artifact"))); err != nil {
			t.Fatalf("save %s: %v", n, err)
		}
	}
	size := int64(len("artifact"))
	now := time.Now()
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheMaxPercent: 10},
		ready: []repository.VideoPlaybackAsset{
			{VideoID: 1, Status: repository.PlaybackAssetStatusReady, Filename: &oldName, SizeBytes: &size, LastAccessedAt: ptrTime(now.Add(-time.Hour))},
			{VideoID: 2, Status: repository.PlaybackAssetStatusReady, Filename: &newName, SizeBytes: &size, LastAccessedAt: ptrTime(now)},
		},
		// The oldest (LRU) victim's row delete fails.
		deleteAssetErr:      errors.New("db unavailable"),
		deleteAssetErrForID: 1,
	}
	svc := New(repo, store, t.TempDir(), "", nil)
	svc.capacityOverride = func(int64) (int64, bool) { return size, true } // budget fits one; total is two

	if err := svc.Prune(ctx); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	// Neither evicted: the older one's delete failed and we stopped rather than
	// evicting the newer to compensate. (Old behavior would evict video 2.)
	if len(repo.ready) != 2 {
		t.Fatalf("ready = %#v, want both kept (no LRU inversion)", repo.ready)
	}
	if exists, _ := store.Exists(ctx, storagekeys.Video(newName)); !exists {
		t.Fatal("newer artifact evicted to compensate for the un-deletable older one (LRU inversion)")
	}
}

// cancelRunner simulates a build interrupted by graceful shutdown.
type cancelRunner struct{ calls int }

func (r *cancelRunner) Concat(context.Context, string, string) error {
	r.calls++
	return context.Canceled
}

// A graceful shutdown (context.Canceled) must drop the building row so the
// reconciler rebuilds it immediately on next boot, NOT record it as a failure
// that then waits out the retry cooldown.
func TestBuildNowInterruptDropsRowForPromptRetry(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewLocal(t.TempDir())
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 10},
		video:    &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
		parts:    compatibleParts(),
	}
	savePartFiles(t, ctx, store, repo.parts)
	runner := &cancelRunner{}
	svc := New(repo, store, t.TempDir(), "", nil)
	svc.SetRunner(runner)
	svc.capacityOverride = func(int64) (int64, bool) { return 1 << 40, true }

	err := svc.BuildNow(ctx, 42)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildNow err = %v, want context.Canceled", err)
	}
	if repo.asset != nil {
		t.Fatalf("interrupted build left row %#v; want it dropped for prompt retry", repo.asset)
	}
	for _, ev := range repo.events {
		if ev == repository.PlaybackAssetStatusFailed {
			t.Fatal("interrupted build recorded as failed; want no failure row")
		}
	}
}

// blockingRunner blocks in Concat until its context is canceled, recording the
// cancellation cause. It lets the Close test prove an in-flight build is killed
// rather than left to run (and orphan its ffmpeg child) past shutdown.
type blockingRunner struct {
	started chan struct{}
	once    sync.Once
	ctxErr  error
}

func (r *blockingRunner) Concat(ctx context.Context, _, _ string) error {
	r.once.Do(func() { close(r.started) })
	<-ctx.Done()
	r.ctxErr = ctx.Err()
	return ctx.Err()
}

func TestCloseCancelsInflightBuild(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewLocal(t.TempDir())
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 10},
		video:    &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
		parts:    compatibleParts(),
	}
	savePartFiles(t, ctx, store, repo.parts)
	runner := &blockingRunner{started: make(chan struct{})}
	svc := New(repo, store, t.TempDir(), "", nil)
	svc.SetRunner(runner)
	svc.capacityOverride = func(int64) (int64, bool) { return 1 << 40, true }

	svc.StartBuild(ctx, 42)
	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("build never reached the runner")
	}

	done := make(chan struct{})
	go func() { svc.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return; in-flight build was not canceled")
	}
	if runner.ctxErr == nil {
		t.Fatal("runner was not canceled by Close")
	}
}
