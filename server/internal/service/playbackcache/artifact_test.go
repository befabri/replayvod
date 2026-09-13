package playbackcache

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/downloader/remux"
	"github.com/befabri/replayvod/server/internal/mediastore"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/storage"
)

func TestPlaybackBuildPassesItsWorkspaceToConcat(t *testing.T) {
	s, repo, _, _, video := publicationFixture(t)
	parts, err := repo.ListVideoParts(t.Context(), video.ID)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{body: []byte("playback")}
	s.SetRunner(runner)
	artifact, err := s.buildArtifact(t.Context(), parts)
	if err != nil {
		t.Fatal(err)
	}
	defer artifact.cleanup()
	if runner.files != artifact.workspace {
		t.Fatal("concat did not receive the artifact's scratch owner")
	}
}

type cacheWorkspaceFiles struct {
	*mediastore.Workspace
	removes, renames int
}

func (f *cacheWorkspaceFiles) Remove(path string) error {
	f.removes++
	return f.Workspace.Remove(path)
}

func (f *cacheWorkspaceFiles) Rename(from, to string) error {
	f.renames++
	return f.Workspace.Rename(from, to)
}

type cacheCommand func(context.Context, string, []string, io.Writer) error

func (f cacheCommand) Run(ctx context.Context, binary string, args []string, stderr io.Writer) error {
	return f(ctx, binary, args, stderr)
}

func TestPlaybackRemuxCommitsThroughItsWorkspace(t *testing.T) {
	w, err := mediastore.NewScratch(t.TempDir()).New("playback", 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close(true)
	files := &cacheWorkspaceFiles{Workspace: w}
	output := filepath.Join(w.Dir, "playback.mp4")
	runner := remuxRunner{remuxer: &remux.Remuxer{Runner: cacheCommand(func(_ context.Context, _ string, args []string, _ io.Writer) error {
		return os.WriteFile(args[len(args)-1], []byte("playback"), 0600)
	})}}
	if err := runner.Concat(t.Context(), filepath.Join(w.Dir, "parts.txt"), output, files); err != nil {
		t.Fatal(err)
	}
	if files.removes != 1 || files.renames != 1 {
		t.Fatalf("remux bypassed workspace: removes=%d renames=%d", files.removes, files.renames)
	}
	if data, err := os.ReadFile(output); err != nil || string(data) != "playback" {
		t.Fatalf("playback output=%q err=%v", data, err)
	}
}

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
