// Package remux prepares local HLS fragments and stream-copies them into MP4 or M4A files.
package remux

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Mode selects the HLS fragment format and its ffmpeg input description.
type Mode string

const (
	// ModeTS uses the concat demuxer with a segments.txt input list.
	ModeTS Mode = "ts"

	// ModeFMP4 uses a media.m3u8 playlist with the required init segment.
	ModeFMP4 Mode = "fmp4"
)

// Kind selects a video MP4 or audio-only M4A output container.
type Kind string

const (
	KindVideo Kind = "video"
	KindAudio Kind = "audio"
)

// OutputExt returns the container extension, including its leading dot.
// Unknown kinds use .mp4.
func (k Kind) OutputExt() string {
	if k == KindAudio {
		return ".m4a"
	}
	return ".mp4"
}

// PrepareInput replaces the input description in workDir and returns its absolute path.
// The directory must contain committed numeric .ts fragments, or .m4s fragments with init.mp4.
// Re-running preparation safely replaces an existing description through files.
func PrepareInput(ctx context.Context, workDir string, mode Mode, files FileOperations) (string, error) {
	absDir, err := filepath.Abs(workDir)
	if err != nil {
		return "", fmt.Errorf("remux: resolve work dir %q: %w", workDir, err)
	}
	info, err := os.Stat(absDir)
	if err != nil {
		return "", fmt.Errorf("remux: stat work dir %q: %w", absDir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("remux: work dir %q is not a directory", absDir)
	}

	switch mode {
	case ModeTS:
		return prepareTSConcat(ctx, absDir, files)
	case ModeFMP4:
		return prepareFMP4Playlist(ctx, absDir, files)
	default:
		return "", fmt.Errorf("remux: unknown mode %q", mode)
	}
}

func prepareTSConcat(ctx context.Context, absDir string, files FileOperations) (string, error) {
	segs, err := scanSegments(absDir, ".ts")
	if err != nil {
		return "", err
	}
	if len(segs) == 0 {
		return "", fmt.Errorf("remux: no .ts segments in %q", absDir)
	}

	paths := make([]string, len(segs))
	for i, s := range segs {
		paths[i] = filepath.Join(absDir, s.filename)
	}

	outPath := filepath.Join(absDir, "segments.txt")
	if err := WriteConcatListFile(ctx, outPath, paths, files); err != nil {
		return "", err
	}
	return outPath, nil
}

// ConcatList renders paths in their given order and escapes embedded single quotes.
// The ffmpeg concat demuxer reads files in that order.
func ConcatList(paths []string) string {
	var b strings.Builder
	for _, p := range paths {
		fmt.Fprintf(&b, "file '%s'\n", strings.ReplaceAll(p, "'", "'\\''"))
	}
	return b.String()
}

// WriteConcatListFile replaces path with a concat-demuxer input list through files.
func WriteConcatListFile(ctx context.Context, path string, paths []string, files FileOperations) error {
	if err := writeInputFile(ctx, path, []byte(ConcatList(paths)), files); err != nil {
		return fmt.Errorf("remux: write concat list %s: %w", path, err)
	}
	return nil
}

// prepareFMP4Playlist writes local init and fragment references with ENDLIST.
// During stream copy, ffmpeg reads durations from fragment headers, so EXTINF can be zero.
func prepareFMP4Playlist(ctx context.Context, absDir string, files FileOperations) (string, error) {
	initPath := filepath.Join(absDir, "init.mp4")
	if _, err := os.Stat(initPath); err != nil {
		return "", fmt.Errorf("remux: init.mp4 missing in %q: %w", absDir, err)
	}
	segs, err := scanSegments(absDir, ".m4s")
	if err != nil {
		return "", err
	}
	if len(segs) == 0 {
		return "", fmt.Errorf("remux: no .m4s fragments in %q", absDir)
	}

	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:6\n")
	b.WriteString("#EXT-X-TARGETDURATION:10\n")
	b.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
	fmt.Fprintf(&b, "#EXT-X-MAP:URI=\"%s\"\n", initPath)
	for _, s := range segs {
		b.WriteString("#EXTINF:0,\n")
		fmt.Fprintf(&b, "%s\n", filepath.Join(absDir, s.filename))
	}
	b.WriteString("#EXT-X-ENDLIST\n")

	outPath := filepath.Join(absDir, "media.m3u8")
	if err := writeInputFile(ctx, outPath, []byte(b.String()), files); err != nil {
		return "", fmt.Errorf("remux: write media.m3u8: %w", err)
	}
	return outPath, nil
}

type segmentEntry struct {
	seq      int64
	filename string
}

// scanSegments returns matching fragments in numeric media-sequence order.
// Lexicographic filenames would put segment 10 before segment 2.
func scanSegments(absDir, ext string) ([]segmentEntry, error) {
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil, fmt.Errorf("remux: read work dir %q: %w", absDir, err)
	}
	var out []segmentEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ext) {
			continue
		}
		stem := strings.TrimSuffix(name, ext)
		seq, err := strconv.ParseInt(stem, 10, 64)
		if err != nil {
			continue
		}
		out = append(out, segmentEntry{seq: seq, filename: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seq < out[j].seq })
	return out, nil
}
