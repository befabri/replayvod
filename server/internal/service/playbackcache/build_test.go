package playbackcache

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/replayvod/server/internal/storagekeys"
)

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
	svc := New(repo, cacheMedia(t, repo, store, nil, nil), "", nil)
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
	runner := &fakeRunner{body: []byte("playback")}
	svc := New(repo, cacheMedia(t, repo, store, nil, nil), "", nil)
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

// TestBuildNowDropsArtifactWhenVideoDeletedMidBuild checks that retention during
// concat cannot leave a ready row or published output.
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
		repo.video.DeletedAt = &deletedAt
	}}
	svc := New(repo, cacheMedia(t, repo, store, nil, nil), "", nil)
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
	assertPlaybackFiles(t, svc, store)
}

// TestBuildNowFailsSafeOnRecheckError preserves retryable building state when
// publication cannot confirm the recording still exists.
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
	svc := New(repo, cacheMedia(t, repo, store, nil, nil), "", nil)
	svc.SetRunner(runner)
	svc.capacityOverride = func(int64) (int64, bool) { return 1 << 40, true }

	if err := svc.BuildNow(ctx, 42); err == nil {
		t.Fatal("BuildNow succeeded; want error from the transient re-check")
	}
	if repo.asset == nil || repo.asset.Status != repository.PlaybackAssetStatusBuilding {
		t.Fatalf("asset = %#v, want building left for the next play (not committed ready)", repo.asset)
	}
	assertPlaybackFiles(t, svc, store)
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

// TestBuildNowSkipsRebuildOnExistsError preserves ready artifacts after failed
// existence probes.
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
	svc := New(repo, cacheMedia(t, repo, existsErrStore{LocalStorage: local, err: errors.New("HeadObject 500")}, nil, nil), "", nil)
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

// TestBuildNowDeletedMidBuildBeatsOversizeVerdict checks that deletion prevents
// an unavailable row from being recreated after concat.
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
	runner := &fakeRunner{body: make([]byte, 100), beforeWrite: func() { repo.video.DeletedAt = &deletedAt }}
	svc := New(repo, cacheMedia(t, repo, store, nil, nil), "", nil)
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
	assertPlaybackFiles(t, svc, store)
}

// TestBuildNowInterruptDropsRowForPromptRetry checks that cancellation permits
// retry without recording a build failure.
func TestBuildNowInterruptDropsRowForPromptRetry(t *testing.T) {
	ctx := t.Context()
	svc, repo, store, runner := cacheFixture(t, nil)
	runner.err = context.Canceled

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
	assertPlaybackFiles(t, svc, store)
}

func TestBuildNowWritesReadyArtifactAfterDownload(t *testing.T) {
	ctx := t.Context()
	svc, repo, store, runner := cacheFixture(t, nil)

	if err := svc.BuildNow(ctx, 42); err != nil {
		t.Fatalf("BuildNow: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("concat calls = %d, want 1", runner.calls)
	}
	if got := strings.Join(repo.events, ","); got != "building,ready" {
		t.Fatalf("events = %s, want building,ready", got)
	}
	if repo.asset == nil || repo.asset.Filename == nil || !strings.HasPrefix(*repo.asset.Filename, "vod-42-playback-") {
		t.Fatalf("ready asset = %#v", repo.asset)
	}
	info, err := store.Stat(ctx, storagekeys.Video(*repo.asset.Filename))
	if err != nil {
		t.Fatalf("stat playback artifact: %v", err)
	}
	if info.Size != int64(len("playback")) {
		t.Fatalf("artifact size = %d", info.Size)
	}
	if len(runner.lists) != 1 || !strings.Contains(runner.lists[0], "part001.mp4") || !strings.Contains(runner.lists[0], "part002.mp4") {
		t.Fatalf("concat list = %#v", runner.lists)
	}
}

func TestBuildNowMarksIncompatiblePartsUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func([]repository.VideoPart)
	}{
		{"quality mismatch", func(p []repository.VideoPart) { p[1].Quality = "720" }},
		{"codec mismatch", func(p []repository.VideoPart) { p[1].Codec = "other" }},
		{"segment format mismatch", func(p []repository.VideoPart) { p[1].SegmentFormat = "ts" }},
		{"frame rate mismatch", func(p []repository.VideoPart) { fps := 30.0; p[1].FPS = &fps }},
		{"missing frame rate", func(p []repository.VideoPart) { p[1].FPS = nil }},
		{"unsupported container", func(p []repository.VideoPart) { p[0].Filename = "part.ts" }},
		{"mixed containers", func(p []repository.VideoPart) { p[1].Filename = "part.m4a" }},
		{"missing part index", func(p []repository.VideoPart) { p[1].PartIndex = 3 }},
		{"empty part", func(p []repository.VideoPart) { p[1].SizeBytes = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, store, runner := cacheFixture(t, nil)
			tc.change(repo.parts)
			if err := svc.BuildNow(t.Context(), 42); err != nil {
				t.Fatal(err)
			}
			if runner.calls != 0 {
				t.Fatalf("incompatible parts reached concat: %d calls", runner.calls)
			}
			if repo.asset == nil || repo.asset.Status != repository.PlaybackAssetStatusUnavailable || repo.asset.Error == nil || *repo.asset.Error == "" {
				t.Fatalf("asset = %+v, want unavailable with a reason", repo.asset)
			}
			assertPlaybackFiles(t, svc, store)
		})
	}
}

func TestBuildNowReconcilesReadyRowFailureWithoutRebuilding(t *testing.T) {
	ctx := t.Context()
	svc, repo, store, runner := cacheFixture(t, nil)
	repo.readyErr = errors.New("db unavailable")

	if err := svc.BuildNow(ctx, 42); err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 || repo.asset == nil || repo.asset.Status != repository.PlaybackAssetStatusReady {
		t.Fatalf("persistence retry repeated concat: calls=%d asset=%+v", runner.calls, repo.asset)
	}
	if exists, err := store.Exists(ctx, storagekeys.Video(*repo.asset.Filename)); err != nil || !exists {
		t.Fatalf("confirmed output lost: exists=%v err=%v", exists, err)
	}
}

func TestBuildNowDoesNothingWhenDisabled(t *testing.T) {
	for _, tc := range []struct {
		name    string
		disable func(*repository.ServerSettings)
	}{
		{"feature off", func(s *repository.ServerSettings) { s.PlaybackCacheEnabled = false }},
		{"generation off", func(s *repository.ServerSettings) { s.PlaybackCacheAutoGenerate = false }},
		{"zero budget", func(s *repository.ServerSettings) { s.PlaybackCacheMaxPercent = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, store, runner := cacheFixture(t, nil)
			tc.disable(repo.settings)
			if err := svc.BuildNow(t.Context(), 42); err != nil {
				t.Fatal(err)
			}
			if runner.calls != 0 || repo.asset != nil {
				t.Fatalf("disabled build ran: calls=%d asset=%+v", runner.calls, repo.asset)
			}
			assertPlaybackFiles(t, svc, store)
		})
	}
}

func TestBuildNowRecordsConcatFailureAndRetries(t *testing.T) {
	svc, repo, store, runner := cacheFixture(t, nil)
	failure := errors.New("ffmpeg could not concatenate parts")
	runner.err = failure // Write partial output before returning the error.
	if err := svc.BuildNow(t.Context(), 42); !errors.Is(err, failure) {
		t.Fatalf("BuildNow error = %v, want concat failure", err)
	}
	if got := strings.Join(repo.events, ","); got != "building,failed" {
		t.Fatalf("events = %s, want building,failed", got)
	}
	if repo.asset == nil || repo.asset.Status != repository.PlaybackAssetStatusFailed || repo.asset.Error == nil || *repo.asset.Error != failure.Error() {
		t.Fatalf("concat failure was not persisted: %+v", repo.asset)
	}
	assertPlaybackFiles(t, svc, store)

	runner.err = nil
	if err := svc.BuildNow(t.Context(), 42); err != nil {
		t.Fatal(err)
	}
	if runner.calls != 2 || repo.asset == nil || repo.asset.Status != repository.PlaybackAssetStatusReady || repo.asset.Filename == nil {
		t.Fatalf("retry did not produce ready output: calls=%d asset=%+v", runner.calls, repo.asset)
	}
	assertPlaybackFiles(t, svc, store, *repo.asset.Filename)
}
