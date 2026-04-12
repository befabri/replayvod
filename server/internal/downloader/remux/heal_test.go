package remux

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestHealArgs_Video(t *testing.T) {
	got := healArgs("/in/rec.mp4", "/out/rec.healed.mp4", KindVideo)
	want := []string{
		"-y",
		"-i", "/in/rec.mp4",
		"-vcodec", "copy",
		"-acodec", "copy",
		"/out/rec.healed.mp4",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args=%v\nwant=%v", got, want)
	}
}

func TestHealArgs_AudioDropsVideoFlag(t *testing.T) {
	// audio-only files have no video stream; -vcodec copy would
	// make ffmpeg complain.
	got := healArgs("/in/rec.m4a", "/out/rec.healed.m4a", KindAudio)
	want := []string{
		"-y",
		"-i", "/in/rec.m4a",
		"-acodec", "copy",
		"/out/rec.healed.m4a",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args=%v\nwant=%v", got, want)
	}
}

func TestRemuxer_Heal_Success(t *testing.T) {
	m := &mockRunner{}
	r := &Remuxer{Runner: m}
	err := r.Heal(context.Background(), "/in.mp4", "/out.mp4", KindVideo)
	if err != nil {
		t.Fatalf("Heal: %v", err)
	}
	if m.lastName != DefaultFFmpegPath {
		t.Errorf("binary=%q", m.lastName)
	}
	// Sanity: args match what healArgs would produce.
	want := healArgs("/in.mp4", "/out.mp4", KindVideo)
	if !reflect.DeepEqual(m.lastArgs, want) {
		t.Errorf("args=%v\nwant=%v", m.lastArgs, want)
	}
}

func TestRemuxer_Heal_FailureIncludesStderr(t *testing.T) {
	m := &mockRunner{
		returnErr: errors.New("exit status 1"),
		stderrOut: "Could not open encoder — heal-specific failure text",
	}
	r := &Remuxer{Runner: m}
	err := r.Heal(context.Background(), "/in.mp4", "/out.mp4", KindVideo)
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "heal-specific failure text") {
		t.Errorf("err=%v, want stderr excerpt", err)
	}
	if !strings.Contains(err.Error(), "exit status 1") {
		t.Errorf("err=%v, want underlying error", err)
	}
}

func TestRemuxer_Heal_CtxCancelPassesThrough(t *testing.T) {
	m := &mockRunner{returnErr: context.Canceled}
	r := &Remuxer{Runner: m}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := r.Heal(ctx, "/in.mp4", "/out.mp4", KindVideo)
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
