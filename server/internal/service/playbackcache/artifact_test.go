package playbackcache

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
)

func TestLocalPlaybackInputsArePinnedInManagedScratch(t *testing.T) {
	s, _, raw, _, _ := publicationFixture(t)
	ctx := t.Context()
	w, err := s.store.Scratch().New("local-copy", 100)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close(true)
	parts := []repository.VideoPart{{Filename: "pin.mp4", SizeBytes: 8}}
	if err := raw.Save(ctx, "videos/pin.mp4", strings.NewReader("original")); err != nil {
		t.Fatal(err)
	}
	paths, err := s.localPartPaths(ctx, w, parts)
	if err != nil || len(paths) != 1 || filepath.Dir(paths[0]) != w.Dir {
		t.Fatalf("inputs did not use managed scratch: %v %v", paths, err)
	}
	if err := raw.Save(ctx, "videos/pin.mp4", strings.NewReader("replacement")); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(paths[0]); err != nil || string(data) != "original" {
		t.Fatalf("source replacement changed pinned input: %q %v", data, err)
	}
}

func TestLocalPlaybackReservesInputOutputAndMuxingMarginBeforeCopy(t *testing.T) {
	s, _, raw, _, _ := publicationFixture(t)
	ctx := t.Context()
	if err := raw.Save(ctx, "videos/budget.mp4", strings.NewReader("source")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.store.Scratch().Root(), 0755); err != nil {
		t.Fatal(err)
	}
	total, avail, err := statfsBytes(s.store.Scratch().Root())
	if err != nil {
		t.Fatal(err)
	}
	// Two copies fit with 0.5% left; the muxing margin does not. Only metadata
	// advertises this size, so a broken reservation cannot actually fill disk.
	estimate := (avail - total/20) * 100 / 201
	runner := &fakeRunner{body: []byte("output")}
	s.SetRunner(runner)
	artifact, err := s.buildArtifact(ctx, []repository.VideoPart{{Filename: "budget.mp4", SizeBytes: estimate}})
	if artifact != nil {
		artifact.cleanup()
	}
	if !errors.Is(err, storage.ErrFull) || runner.calls != 0 {
		t.Fatalf("build failed to reserve copies plus margin: err=%v concat=%d", err, runner.calls)
	}
	files, err := os.ReadDir(s.store.Scratch().Root())
	if err != nil || len(files) != 0 {
		t.Fatalf("rejected reservation left scratch: %v %v", files, err)
	}
}

func TestBackgroundBuildPanicReleasesScratchAndAllowsRetry(t *testing.T) {
	svc, repo, _, _, video := publicationFixture(t)
	svc.capacityOverride = func(int64) (int64, bool) { return 1 << 40, true }
	svc.SetRunner(&fakeRunner{beforeWrite: func() { panic("concat failed") }})
	if err := svc.StartBuild(t.Context(), video.ID); err != nil {
		t.Fatal(err)
	}
	svc.Wait()
	entries, err := os.ReadDir(svc.store.Scratch().Root())
	if err != nil || len(entries) != 0 {
		t.Fatalf("recovered build panic left copied inputs in scratch: %v, %v", entries, err)
	}
	svc.SetRunner(&fakeRunner{body: []byte("playback")})
	if err := svc.StartBuild(t.Context(), video.ID); err != nil {
		t.Fatal(err)
	}
	svc.Wait()
	asset, err := repo.GetVideoPlaybackAsset(t.Context(), video.ID)
	if err != nil || asset.Status != repository.PlaybackAssetStatusReady {
		t.Fatalf("build could not retry after panic: %+v, %v", asset, err)
	}
}
