//go:build ffmpeg

// Run these tests with -tags ffmpeg; ffmpeg and ffprobe must be available in PATH.

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

// requireFFmpeg skips unless both fixture generation and probing binaries are available.
func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not in PATH")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not in PATH")
	}
}

// genTSFragment writes dur seconds of H.264 video and AAC audio in MPEG-TS.
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

// genTSAudioFragment writes dur seconds of AAC audio in MPEG-TS.
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

// genFMP4HLS returns an ffmpeg-generated playlist with local init and fragment files.
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

// probeOutput returns the container and stream metadata reported by ffprobe.
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

func hasStream(p probeResult, codecType string) bool {
	for _, s := range p.Streams {
		if s.CodecType == codecType {
			return true
		}
	}
	return false
}

// TestReal_Run_TS_Video verifies that .part output receives an explicit MP4 muxer.
func TestReal_Run_TS_Video(t *testing.T) {
	requireFFmpeg(t)

	workDir := t.TempDir()
	outputDir := t.TempDir()

	segPath := filepath.Join(workDir, "0.ts")
	genTSFragment(t, segPath, 2.0)

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

// TestReal_Run_TS_Audio verifies that the MP4 muxer also accepts audio-only M4A output.
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

// genMP4Part writes dur seconds of H.264 and AAC in a self-contained MP4.
func genMP4Part(t *testing.T, path string, dur float64) {
	t.Helper()
	durStr := strconv.FormatFloat(dur, 'f', 2, 64)
	cmd := exec.Command("ffmpeg",
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=30:duration="+durStr,
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+durStr,
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		// Enable B-frames to expose timestamp discontinuities; ultrafast disables them.
		"-bf", "2", "-g", "15",
		"-c:a", "aac",
		"-movflags", "+faststart",
		"-f", "mp4",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gen mp4 part: %v\n%s", err, out)
	}
}

// genM4APart writes dur seconds of AAC in an M4A container.
func genM4APart(t *testing.T, path string, dur float64) {
	t.Helper()
	durStr := strconv.FormatFloat(dur, 'f', 2, 64)
	cmd := exec.Command("ffmpeg",
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+durStr,
		"-c:a", "aac",
		"-movflags", "+faststart",
		"-f", "mp4",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gen m4a part: %v\n%s", err, out)
	}
}

// decodeClean fails if full decoding emits errors, including timestamp anomalies at joins.
func decodeClean(t *testing.T, path string) {
	t.Helper()
	cmd := exec.Command("ffmpeg", "-hide_banner", "-v", "error", "-i", path, "-f", "null", "-")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("decode %s: %v\nstderr: %s", path, err, stderr.String())
	}
	if s := strings.TrimSpace(stderr.String()); s != "" {
		t.Fatalf("decoding %s produced warnings/errors (timestamp/A-V anomaly at a join?):\n%s", path, s)
	}
}

// concatParts returns a faststart artifact built with the playback-cache concat path.
func concatParts(t *testing.T, kind Kind, parts []string) string {
	t.Helper()
	workDir := t.TempDir()
	outputDir := t.TempDir()
	listPath := filepath.Join(workDir, "parts.txt")
	if err := WriteConcatListFile(t.Context(), listPath, parts, nil); err != nil {
		t.Fatalf("WriteConcatListFile: %v", err)
	}
	r := &Remuxer{}
	if err := r.Run(context.Background(), RunInput{
		Mode:           ModeTS,
		Kind:           kind,
		Faststart:      true,
		InputPath:      listPath,
		OutputDir:      outputDir,
		OutputBasename: "playback",
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return filepath.Join(outputDir, "playback"+kind.OutputExt())
}

func probeDurationSeconds(t *testing.T, p probeResult) float64 {
	t.Helper()
	d, err := strconv.ParseFloat(p.Format.Duration, 64)
	if err != nil {
		t.Fatalf("parse probed duration %q: %v", p.Format.Duration, err)
	}
	return d
}

// TestReal_Concat_MP4Parts checks stream preservation, duration, and timestamp continuity at joins.
func TestReal_Concat_MP4Parts(t *testing.T) {
	requireFFmpeg(t)

	dir := t.TempDir()
	const partDur = 1.0
	const nParts = 3
	parts := make([]string, nParts)
	for i := range parts {
		parts[i] = filepath.Join(dir, fmt.Sprintf("part%02d.mp4", i+1))
		genMP4Part(t, parts[i], partDur)
	}

	out := concatParts(t, KindVideo, parts)

	decodeClean(t, out)
	p := probeOutput(t, out)
	if !strings.Contains(p.Format.FormatName, "mp4") {
		t.Errorf("format_name=%q, want mp4 variant", p.Format.FormatName)
	}
	if !hasStream(p, "video") || !hasStream(p, "audio") {
		t.Errorf("concatenated artifact missing a stream: %+v", p.Streams)
	}
	if got, want := probeDurationSeconds(t, p), partDur*nParts; got < want-0.5 || got > want+0.5 {
		t.Errorf("duration = %.2fs, want ~%.2fs (sum of parts)", got, want)
	}
}

// TestReal_Concat_M4AParts checks audio duration and timestamp continuity at joins.
func TestReal_Concat_M4AParts(t *testing.T) {
	requireFFmpeg(t)

	dir := t.TempDir()
	const partDur = 1.0
	const nParts = 3
	parts := make([]string, nParts)
	for i := range parts {
		parts[i] = filepath.Join(dir, fmt.Sprintf("part%02d.m4a", i+1))
		genM4APart(t, parts[i], partDur)
	}

	out := concatParts(t, KindAudio, parts)

	decodeClean(t, out)
	p := probeOutput(t, out)
	if !strings.Contains(p.Format.FormatName, "mp4") {
		t.Errorf("format_name=%q, want mp4 variant", p.Format.FormatName)
	}
	if hasStream(p, "video") {
		t.Error("audio-only concat produced a video stream")
	}
	if !hasStream(p, "audio") {
		t.Error("audio-only concat missing audio stream")
	}
	if got, want := probeDurationSeconds(t, p), partDur*nParts; got < want-0.5 || got > want+0.5 {
		t.Errorf("duration = %.2fs, want ~%.2fs (sum of parts)", got, want)
	}
}

// TestReal_Heal_Video verifies that repair produces an MP4 through its temporary .part path.
func TestReal_Heal_Video(t *testing.T) {
	requireFFmpeg(t)

	workDir := t.TempDir()

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
	if err := r.Heal(context.Background(), in.OutputPath(), healed, KindVideo, nil); err != nil {
		t.Fatalf("Heal: %v", err)
	}

	p := probeOutput(t, healed)
	if !strings.Contains(p.Format.FormatName, "mp4") {
		t.Errorf("healed format_name=%q", p.Format.FormatName)
	}
	if !hasStream(p, "video") {
		t.Error("healed output missing video stream")
	}
	if _, err := os.Stat(healed + partSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".part file still present after heal success: %v", err)
	}
}
