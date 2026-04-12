package remux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestHealArgs_Video(t *testing.T) {
	got := healArgs("/in/rec.mp4", "/out/rec.healed.mp4.part", KindVideo)
	want := []string{
		"-y",
		"-i", "/in/rec.mp4",
		"-c", "copy",
		"/out/rec.healed.mp4.part",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args=%v\nwant=%v", got, want)
	}
}

func TestHealArgs_AudioDropsVideoFlag(t *testing.T) {
	// audio-only files have no video stream; -c copy is fine
	// in principle but -c:a copy is explicit and matches what
	// the spec calls for on the audio heal path.
	got := healArgs("/in/rec.m4a", "/out/rec.healed.m4a.part", KindAudio)
	want := []string{
		"-y",
		"-i", "/in/rec.m4a",
		"-c:a", "copy",
		"/out/rec.healed.m4a.part",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args=%v\nwant=%v", got, want)
	}
}

func TestRemuxer_Heal_SuccessCommitsAtomicRename(t *testing.T) {
	dir := t.TempDir()
	finalPath := filepath.Join(dir, "healed.mp4")
	partPath := finalPath + partSuffix

	m := &mockRunner{emulateSuccess: true}
	r := &Remuxer{Runner: m}
	err := r.Heal(context.Background(), "/in.mp4", finalPath, KindVideo)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if _, err := os.Stat(finalPath); err != nil {
		t.Errorf("final file missing: %v", err)
	}
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Errorf(".part should be gone after commit, got err=%v", err)
	}
	// Runner was invoked with the .part path.
	if m.lastArgs[len(m.lastArgs)-1] != partPath {
		t.Errorf("last arg=%q, want %q", m.lastArgs[len(m.lastArgs)-1], partPath)
	}
}

func TestRemuxer_Heal_FailureCleansPartFile(t *testing.T) {
	dir := t.TempDir()
	finalPath := filepath.Join(dir, "healed.mp4")
	partPath := finalPath + partSuffix
	// Seed a stale .part from a prior crash.
	if err := os.WriteFile(partPath, []byte("junk"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	m := &mockRunner{
		returnErr: errors.New("exit status 1"),
		stderrOut: "heal-specific failure",
	}
	r := &Remuxer{Runner: m}
	err := r.Heal(context.Background(), "/in.mp4", finalPath, KindVideo)
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "heal-specific failure") {
		t.Errorf("err=%v, want stderr excerpt", err)
	}
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Errorf(".part not cleaned after failure: %v", err)
	}
	if _, err := os.Stat(finalPath); !os.IsNotExist(err) {
		t.Errorf("final should not exist on failure: %v", err)
	}
}

func TestRemuxer_Heal_CtxCancelPassesThrough(t *testing.T) {
	dir := t.TempDir()
	finalPath := filepath.Join(dir, "healed.mp4")

	m := &mockRunner{returnErr: context.Canceled}
	r := &Remuxer{Runner: m}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := r.Heal(ctx, "/in.mp4", finalPath, KindVideo)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err=%v, want context.Canceled", err)
	}
	if strings.Contains(err.Error(), "stderr") {
		t.Errorf("ctx cancel dressed with stderr: %q", err)
	}
}

func TestCorruptionThresholdValue(t *testing.T) {
	// The 50s threshold is a spec-pinned value; pinning it in a
	// test surfaces a spec change as a test failure rather than
	// silently drifting tolerance.
	if CorruptionThreshold != 50.0 {
		t.Errorf("CorruptionThreshold=%v, want 50.0 (spec Stage 9)", CorruptionThreshold)
	}
}
