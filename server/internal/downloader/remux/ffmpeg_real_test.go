//go:build ffmpeg

// Real-ffmpeg tests. Excluded from the default `go test ./...`
// run — enable via:
//
//	go test -tags ffmpeg -count=1 ./internal/downloader/remux/...
//	# or: task test-ffmpeg
//
// The existing argv tests (ffmpeg_test.go, heal_test.go) compare
// argv slices produced by our builder against argv slices baked
// into the test. Both sides come from us — if the slice is wrong,
// both sides are wrong together. They caught zero bugs against
// ffmpeg 8.1's stricter extension auto-detection (the `-f mp4`
// regression).
//
// These tests actually invoke ffmpeg against synthetic fragments
// generated at test time. Any future argv drift (ffmpeg tightens
// a flag, a muxer default changes) shows up here the moment the
// test runs, because the assertion is "ffmpeg accepted the argv
// and produced a playable output" — not "the argv is the one we
// thought we were producing."
//
// Cost: ~200 ms per test. ffmpeg is required in PATH; the test
// Skips if it isn't.

package remux

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// requireFFmpeg resolves ffmpeg + ffprobe from PATH or skips the
// test. Both binaries are needed — generating fixtures and probing
// output.
func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not in PATH")
	}
}

// genTSFragment generates a synthetic MPEG-TS fragment at path with
// H.264 video + AAC audio, `dur` seconds long. Used as the input to
// Remuxer.Run's ModeTS path.
//
// `ultrafast` keeps test runs sub-second; `testsrc` is lavfi's
// built-in animated pattern and `sine` is its tone generator, so
// no external fixture files are needed.
func genTSFragment(t *testing.T, path string, dur float64) {
	t.Helper()
	durStr := strconv.FormatFloat(dur, 'f', 2, 64)
	cmd := exec.Command("ffmpeg",
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=15:duration="+durStr,
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+durStr,
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-f", "mpegts",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gen TS fragment: %v\n%s", err, out)
	}
}

// genTSAudioFragment is the audio-only counterpart. AAC in an
// MPEG-TS container — matches the `audio_only` rendition Twitch
// exposes on TS-path channels.
func genTSAudioFragment(t *testing.T, path string, dur float64) {
	t.Helper()
	durStr := strconv.FormatFloat(dur, 'f', 2, 64)
	cmd := exec.Command("ffmpeg",
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+durStr,
		"-c:a", "aac",
		"-f", "mpegts",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gen TS audio fragment: %v\n%s", err, out)
	}
}

// genFMP4HLS produces an fMP4 HLS output in dir: an init.mp4,
// fragment .m4s files, and a media.m3u8 that references them. The
// playlist written by ffmpeg is the exact shape our production
// code hands to the ModeFMP4 ffmpeg invocation, so Remuxer.Run can
// consume it directly.
//
// Returns the path to the media.m3u8.
func genFMP4HLS(t *testing.T, dir string, dur float64) string {
	t.Helper()
	durStr := strconv.FormatFloat(dur, 'f', 2, 64)
	playlist := filepath.Join(dir, "media.m3u8")
	cmd := exec.Command("ffmpeg",
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=15:duration="+durStr,
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+durStr,
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-f", "hls",
		"-hls_time", "1",
		"-hls_segment_type", "fmp4",
		"-hls_playlist_type", "vod",
		"-hls_segment_filename", filepath.Join(dir, "seg%d.m4s"),
		"-hls_fmp4_init_filename", "init.mp4",
		playlist,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gen fMP4 HLS: %v\n%s", err, out)
	}
	return playlist
}

// probeResult is just the subset we assert against. Anything
// unneeded stays off the struct so one Twitch-side schema change
// doesn't flake a bunch of unrelated tests.
type probeResult struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
	} `json:"streams"`
}

// probeOutput runs ffprobe with `-v error`, returning the decoded
// JSON on success. `-v error` is load-bearing — without it ffprobe
// tolerates repairable damage and returns 0 on files a strict
// player would reject.
func probeOutput(t *testing.T, path string) probeResult {
	t.Helper()
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v\nstderr: %s", path, err, stderr.String())
	}
	var r probeResult
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatalf("probe decode: %v\n%s", err, out)
	}
	return r
}

// hasStream reports whether any stream in the probe matches the
// given codec_type. Small helper to keep assertions readable.
func hasStream(p probeResult, codecType string) bool {
	for _, s := range p.Streams {
		if s.CodecType == codecType {
			return true
		}
	}
	return false
}

// TestReal_Run_TS_Video is the test that would have caught the
// ffmpeg 8.1 `-f mp4` regression. Generates a 2s TS fragment,
// writes a one-line segments.txt, runs Remuxer.Run, and verifies
// the output actually parses as MP4 with both streams present.
func TestReal_Run_TS_Video(t *testing.T) {
	requireFFmpeg(t)

	workDir := t.TempDir()
	outputDir := t.TempDir()

	segPath := filepath.Join(workDir, "0.ts")
	genTSFragment(t, segPath, 2.0)

	// Matches what remux.PrepareInput writes for the TS path.
	segList := filepath.Join(workDir, "segments.txt")
	if err := os.WriteFile(segList, []byte(fmt.Sprintf("file '%s'\n", segPath)), 0o644); err != nil {
		t.Fatalf("write segments.txt: %v", err)
	}

	r := &Remuxer{}
	if err := r.Run(context.Background(), RunInput{
		Mode:           ModeTS,
		Kind:           KindVideo,
		InputPath:      segList,
		OutputDir:      outputDir,
		OutputBasename: "rec",
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := filepath.Join(outputDir, "rec.mp4")
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("final file missing: %v", err)
	}
	// .part should be gone after commit rename.
	if _, err := os.Stat(out + partSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".part file still present after success: %v", err)
	}

	p := probeOutput(t, out)
	if !strings.Contains(p.Format.FormatName, "mp4") {
		t.Errorf("format_name=%q, want mp4 variant", p.Format.FormatName)
	}
	if !hasStream(p, "video") {
		t.Error("output missing video stream")
	}
	if !hasStream(p, "audio") {
		t.Error("output missing audio stream")
	}
}

// TestReal_Run_TS_Audio covers the audio-only path: same TS mode,
// Kind=KindAudio, output extension .m4a. The `-f mp4` flag has to
// work for both kinds — m4a is just an MP4 container holding only
// audio. If we ever split to -f ipod for audio this test catches
// any regression.
func TestReal_Run_TS_Audio(t *testing.T) {
	requireFFmpeg(t)

	workDir := t.TempDir()
	outputDir := t.TempDir()

	segPath := filepath.Join(workDir, "0.ts")
	genTSAudioFragment(t, segPath, 2.0)

	segList := filepath.Join(workDir, "segments.txt")
	if err := os.WriteFile(segList, []byte(fmt.Sprintf("file '%s'\n", segPath)), 0o644); err != nil {
		t.Fatalf("write segments.txt: %v", err)
	}

	r := &Remuxer{}
	if err := r.Run(context.Background(), RunInput{
		Mode:           ModeTS,
		Kind:           KindAudio,
		InputPath:      segList,
		OutputDir:      outputDir,
		OutputBasename: "rec",
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := filepath.Join(outputDir, "rec.m4a")
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("final .m4a file missing: %v", err)
	}

	p := probeOutput(t, out)
	if !strings.Contains(p.Format.FormatName, "mp4") {
		t.Errorf("format_name=%q, want mp4 variant", p.Format.FormatName)
	}
	if hasStream(p, "video") {
		t.Error("audio job produced a video stream")
	}
	if !hasStream(p, "audio") {
		t.Error("audio job missing audio stream")
	}
}

// TestReal_Run_FMP4_Video covers the fMP4 path. Generates an
// fMP4-HLS output with ffmpeg (init.mp4 + fragments + playlist),
// hands the playlist to Remuxer.Run, and verifies the remuxed
// MP4 parses. Distinct argv path from ModeTS — any divergence
// between the two branches shows up here.
func TestReal_Run_FMP4_Video(t *testing.T) {
	requireFFmpeg(t)

	workDir := t.TempDir()
	outputDir := t.TempDir()

	playlist := genFMP4HLS(t, workDir, 2.0)

	r := &Remuxer{}
	if err := r.Run(context.Background(), RunInput{
		Mode:           ModeFMP4,
		Kind:           KindVideo,
		InputPath:      playlist,
		OutputDir:      outputDir,
		OutputBasename: "rec",
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := filepath.Join(outputDir, "rec.mp4")
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("final file missing: %v", err)
	}

	p := probeOutput(t, out)
	if !strings.Contains(p.Format.FormatName, "mp4") {
		t.Errorf("format_name=%q, want mp4 variant", p.Format.FormatName)
	}
	if !hasStream(p, "video") {
		t.Error("output missing video stream")
	}
	if !hasStream(p, "audio") {
		t.Error("output missing audio stream")
	}
}

// TestReal_Heal_Video runs the Stage 9 heal pass against a real
// MP4. The heal argv shares the `-f mp4` trap with Run, and the
// `-c copy` vs `-c:a copy` split matters once a real ffmpeg sees
// the argument list.
func TestReal_Heal_Video(t *testing.T) {
	requireFFmpeg(t)

	workDir := t.TempDir()

	// Start with a real remuxed MP4 — the heal input. Easiest to
	// produce by running a fresh Run (the path we just validated
	// above) rather than teaching the test about yet another
	// ffmpeg invocation.
	segPath := filepath.Join(workDir, "0.ts")
	genTSFragment(t, segPath, 2.0)
	segList := filepath.Join(workDir, "segments.txt")
	if err := os.WriteFile(segList, []byte(fmt.Sprintf("file '%s'\n", segPath)), 0o644); err != nil {
		t.Fatalf("write segments.txt: %v", err)
	}

	r := &Remuxer{}
	in := RunInput{
		Mode:           ModeTS,
		Kind:           KindVideo,
		InputPath:      segList,
		OutputDir:      workDir,
		OutputBasename: "rec",
	}
	if err := r.Run(context.Background(), in); err != nil {
		t.Fatalf("seed Run: %v", err)
	}
	healed := filepath.Join(workDir, "healed.mp4")
	if err := r.Heal(context.Background(), in.OutputPath(), healed, KindVideo); err != nil {
		t.Fatalf("Heal: %v", err)
	}

	p := probeOutput(t, healed)
	if !strings.Contains(p.Format.FormatName, "mp4") {
		t.Errorf("healed format_name=%q", p.Format.FormatName)
	}
	if !hasStream(p, "video") {
		t.Error("healed output missing video stream")
	}
	// .part should be gone after heal commit.
	if _, err := os.Stat(healed + partSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".part file still present after heal success: %v", err)
	}
}
