package playbackcache

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

type fakeRepo struct {
	repository.Repository
	settings            *repository.ServerSettings
	video               *repository.Video
	parts               []repository.VideoPart
	asset               *repository.VideoPlaybackAsset
	ready               []repository.VideoPlaybackAsset
	readyErr            error
	statsTotal          int64
	recheckErr          error
	deleteAssetErr      error
	deleteAssetErrForID int64 // 0 = apply deleteAssetErr to all videoIDs
	getVideoCalls       int
	events              []string
}

func (r *fakeRepo) VideoStatsTotals(context.Context, string) (*repository.VideoStatsTotals, error) {
	return &repository.VideoStatsTotals{TotalSize: r.statsTotal}, nil
}

func (r *fakeRepo) GetServerSettings(context.Context) (*repository.ServerSettings, error) {
	if r.settings == nil {
		return nil, repository.ErrNotFound
	}
	return r.settings, nil
}

func (r *fakeRepo) GetVideo(context.Context, int64) (*repository.Video, error) {
	r.getVideoCalls++
	// recheckErr simulates a transient read failure on the post-build re-check
	// (the 2nd GetVideo of a build), exercising the fail-safe path.
	if r.getVideoCalls >= 2 && r.recheckErr != nil {
		return nil, r.recheckErr
	}
	if r.video == nil {
		return nil, repository.ErrNotFound
	}
	return r.video, nil
}

func (r *fakeRepo) ListVideoParts(context.Context, int64) ([]repository.VideoPart, error) {
	return r.parts, nil
}

func (r *fakeRepo) GetVideoPlaybackAsset(context.Context, int64) (*repository.VideoPlaybackAsset, error) {
	if r.asset == nil {
		return nil, repository.ErrNotFound
	}
	return r.asset, nil
}

func (r *fakeRepo) UpsertVideoPlaybackAsset(_ context.Context, input *repository.VideoPlaybackAssetInput) (*repository.VideoPlaybackAsset, error) {
	if input.Status == repository.PlaybackAssetStatusReady && r.readyErr != nil {
		return nil, r.readyErr
	}
	r.events = append(r.events, input.Status)
	r.asset = &repository.VideoPlaybackAsset{
		VideoID:         input.VideoID,
		Status:          input.Status,
		Filename:        input.Filename,
		MimeType:        input.MimeType,
		DurationSeconds: input.DurationSeconds,
		SizeBytes:       input.SizeBytes,
		Error:           input.Error,
		GeneratedAt:     input.GeneratedAt,
		LastAccessedAt:  input.LastAccessedAt,
		UpdatedAt:       time.Now(),
	}
	return r.asset, nil
}

func (r *fakeRepo) ListReadyVideoPlaybackAssets(context.Context) ([]repository.VideoPlaybackAsset, error) {
	return append([]repository.VideoPlaybackAsset(nil), r.ready...), nil
}

func (r *fakeRepo) DeleteVideoPlaybackAsset(_ context.Context, videoID int64) error {
	if r.deleteAssetErr != nil && (r.deleteAssetErrForID == 0 || r.deleteAssetErrForID == videoID) {
		return r.deleteAssetErr
	}
	if r.asset != nil && r.asset.VideoID == videoID {
		r.asset = nil
	}
	filtered := r.ready[:0]
	for _, entry := range r.ready {
		if entry.VideoID != videoID {
			filtered = append(filtered, entry)
		}
	}
	r.ready = filtered
	return nil
}

type fakeRunner struct {
	calls int
	lists []string
	body  []byte
	// beforeWrite, if set, runs after the concat list is read but before the
	// output is written — a seam for simulating a concurrent event (e.g. a
	// retention sweep) landing mid-build.
	beforeWrite func()
}

func (r *fakeRunner) Concat(_ context.Context, listPath, outputPath string) error {
	r.calls++
	data, err := os.ReadFile(listPath)
	if err != nil {
		return err
	}
	r.lists = append(r.lists, string(data))
	if r.beforeWrite != nil {
		r.beforeWrite()
	}
	return os.WriteFile(outputPath, r.body, 0o644)
}

func TestBuildNowWritesReadyArtifactAfterDownload(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	for _, name := range []string{"vod-42-01.mp4", "vod-42-02.mp4"} {
		if err := store.Save(ctx, storagekeys.Video(name), bytes.NewReader([]byte("part"))); err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
	}
	fps := 60.0
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 100},
		video:    &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264, SegmentFormat: "mp4", DurationSeconds: 10, SizeBytes: 4},
			{PartIndex: 2, Filename: "vod-42-02.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264, SegmentFormat: "mp4", DurationSeconds: 12, SizeBytes: 4},
		},
	}
	runner := &fakeRunner{body: []byte("playback")}
	svc := New(repo, store, t.TempDir(), "", nil)
	svc.SetRunner(runner)

	if err := svc.BuildNow(ctx, 42); err != nil {
		t.Fatalf("BuildNow: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("concat calls = %d, want 1", runner.calls)
	}
	if got := strings.Join(repo.events, ","); got != "building,ready" {
		t.Fatalf("events = %s, want building,ready", got)
	}
	if repo.asset == nil || repo.asset.Filename == nil || *repo.asset.Filename != "vod-42-playback.mp4" {
		t.Fatalf("ready asset = %#v", repo.asset)
	}
	info, err := store.Stat(ctx, storagekeys.Video("vod-42-playback.mp4"))
	if err != nil {
		t.Fatalf("stat playback artifact: %v", err)
	}
	if info.Size != int64(len("playback")) {
		t.Fatalf("artifact size = %d", info.Size)
	}
	if len(runner.lists) != 1 || !strings.Contains(runner.lists[0], "vod-42-01.mp4") || !strings.Contains(runner.lists[0], "vod-42-02.mp4") {
		t.Fatalf("concat list = %#v", runner.lists)
	}
}

func TestBuildNowMarksIncompatiblePartsUnavailable(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 100},
		video:    &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4", Quality: "1080", Codec: repository.CodecH264, SegmentFormat: "mp4", SizeBytes: 4},
			{PartIndex: 2, Filename: "vod-42-02.mp4", Quality: "720", Codec: repository.CodecH264, SegmentFormat: "mp4", SizeBytes: 4},
		},
	}
	runner := &fakeRunner{body: []byte("playback")}
	svc := New(repo, store, t.TempDir(), "", nil)
	svc.SetRunner(runner)

	if err := svc.BuildNow(ctx, 42); err != nil {
		t.Fatalf("BuildNow: %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("concat calls = %d, want 0", runner.calls)
	}
	if repo.asset == nil || repo.asset.Status != repository.PlaybackAssetStatusUnavailable {
		t.Fatalf("asset = %#v, want unavailable", repo.asset)
	}
}

func TestBuildNowRemovesArtifactWhenReadyRowFails(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	for _, name := range []string{"vod-42-01.mp4", "vod-42-02.mp4"} {
		if err := store.Save(ctx, storagekeys.Video(name), bytes.NewReader([]byte("part"))); err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
	}
	fps := 60.0
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 100},
		video:    &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264, SegmentFormat: "mp4", DurationSeconds: 10, SizeBytes: 4},
			{PartIndex: 2, Filename: "vod-42-02.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264, SegmentFormat: "mp4", DurationSeconds: 12, SizeBytes: 4},
		},
		readyErr: errors.New("db unavailable"),
	}
	runner := &fakeRunner{body: []byte("playback")}
	svc := New(repo, store, t.TempDir(), "", nil)
	svc.SetRunner(runner)

	if err := svc.BuildNow(ctx, 42); err == nil {
		t.Fatal("BuildNow succeeded; want ready-row failure")
	}
	exists, err := store.Exists(ctx, storagekeys.Video("vod-42-playback.mp4"))
	if err != nil {
		t.Fatalf("exists playback artifact: %v", err)
	}
	if exists {
		t.Fatal("playback artifact still exists after ready-row failure")
	}
}

func TestPruneEvictsReadyArtifactsByLRU(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	oldName := "old-playback.mp4"
	newName := "new-playback.mp4"
	for _, name := range []string{oldName, newName} {
		if err := store.Save(ctx, storagekeys.Video(name), bytes.NewReader([]byte("artifact"))); err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
	}
	size := int64(len("artifact"))
	now := time.Now()
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheMaxPercent: 10},
		// ListReadyVideoPlaybackAssets returns oldest-accessed first.
		ready: []repository.VideoPlaybackAsset{
			{VideoID: 1, Status: repository.PlaybackAssetStatusReady, Filename: &oldName, SizeBytes: &size, LastAccessedAt: ptrTime(now.Add(-time.Hour))},
			{VideoID: 2, Status: repository.PlaybackAssetStatusReady, Filename: &newName, SizeBytes: &size, LastAccessedAt: ptrTime(now)},
		},
	}
	svc := New(repo, store, t.TempDir(), "", nil)
	// Budget fits exactly one artifact, forcing eviction of the LRU entry only.
	svc.capacityOverride = func(int64) (int64, bool) { return size, true }

	if err := svc.Prune(ctx); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(repo.ready) != 1 || repo.ready[0].VideoID != 2 {
		t.Fatalf("remaining ready entries = %#v, want only video 2", repo.ready)
	}
	if exists, _ := store.Exists(ctx, storagekeys.Video(oldName)); exists {
		t.Fatalf("%s still exists after prune", oldName)
	}
	if exists, _ := store.Exists(ctx, storagekeys.Video(newName)); !exists {
		t.Fatalf("%s evicted but should have been kept", newName)
	}
}

func TestPruneLeavesCacheWhenDisabled(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	name := "keep-playback.mp4"
	if err := store.Save(ctx, storagekeys.Video(name), bytes.NewReader([]byte("artifact"))); err != nil {
		t.Fatalf("save: %v", err)
	}
	size := int64(len("artifact"))
	repo := &fakeRepo{
		// maxPercent=0 means "off": disabling must not wipe the cache.
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheMaxPercent: 0},
		ready: []repository.VideoPlaybackAsset{
			{VideoID: 1, Status: repository.PlaybackAssetStatusReady, Filename: &name, SizeBytes: &size, LastAccessedAt: ptrTime(time.Now())},
		},
	}
	svc := New(repo, store, t.TempDir(), "", nil)
	svc.capacityOverride = func(int64) (int64, bool) { return 0, true }

	if err := svc.Prune(ctx); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(repo.ready) != 1 {
		t.Fatalf("ready entries = %#v, want cache untouched", repo.ready)
	}
	if exists, _ := store.Exists(ctx, storagekeys.Video(name)); !exists {
		t.Fatal("artifact wiped while cache disabled")
	}
}

func TestBuildNowDoesNothingWhenDisabled(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: false, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 100},
		video:    &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
	}
	runner := &fakeRunner{body: []byte("playback")}
	svc := New(repo, store, t.TempDir(), "", nil)
	svc.SetRunner(runner)

	if err := svc.BuildNow(ctx, 42); err != nil {
		t.Fatalf("BuildNow: %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("concat calls = %d, want 0", runner.calls)
	}
	if repo.asset != nil {
		t.Fatalf("asset = %#v, want nil", repo.asset)
	}
}

// Reconcile must NOT build artifacts: concatenation is lazy (kicked the first
// time a recording is watched), so the server never bulk-builds the library.
// Even a recording that would otherwise be eligible (done, multi-part,
// compatible) is left untouched — Reconcile only prunes.
func TestReconcileDoesNotBuildBacklog(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("local storage: %v", err)
	}
	for _, name := range []string{"vod-42-01.mp4", "vod-42-02.mp4"} {
		if err := store.Save(ctx, storagekeys.Video(name), bytes.NewReader([]byte("part"))); err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
	}
	fps := 60.0
	repo := &fakeRepo{
		settings: &repository.ServerSettings{PlaybackCacheEnabled: true, PlaybackCacheAutoGenerate: true, PlaybackCacheMaxPercent: 100},
		video:    &repository.Video{ID: 42, Status: repository.VideoStatusDone, Filename: "vod-42"},
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264, SegmentFormat: "mp4", DurationSeconds: 10, SizeBytes: 4},
			{PartIndex: 2, Filename: "vod-42-02.mp4", Quality: "1080", FPS: &fps, Codec: repository.CodecH264, SegmentFormat: "mp4", DurationSeconds: 12, SizeBytes: 4},
		},
	}
	runner := &fakeRunner{body: []byte("playback")}
	svc := New(repo, store, t.TempDir(), "", nil)
	svc.SetRunner(runner)

	if err := svc.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	svc.Wait() // join anything the reconciler might have started (it must not)

	if runner.calls != 0 {
		t.Fatalf("concat calls = %d, want 0 (Reconcile must not build)", runner.calls)
	}
	if repo.asset != nil {
		t.Fatalf("asset = %#v, want nil (no build on reconcile)", repo.asset)
	}
	if exists, _ := store.Exists(ctx, storagekeys.Video("vod-42-playback.mp4")); exists {
		t.Fatal("reconcile built a playback artifact; builds must be lazy/on-play")
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
