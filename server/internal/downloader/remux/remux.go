// Package remux implements Stage 5-6 of the download pipeline:
// prepare an ffmpeg input description from a directory of HLS
// fragments and run ffmpeg with -c copy to produce a playable
// MP4 (video) or M4A (audio).
//
// Why a separate package from hls/: the hls fetcher's job is
// HTTP → disk; remux is disk → disk. They share no state. Keeping
// them separate means the remux step can be re-run standalone on
// an orphan .part directory (debug path) without pulling in the
// HTTP transport + worker pool. It also lets the orchestrator
// split the work for progress reporting — "fetching" vs
// "remuxing" are two stages the UI renders separately.
//
// This package shells out to ffmpeg because implementing a
// container muxer in Go is out of scope per spec. ffmpeg with
// -c copy is a bitstream-copy operation: no transcoding, no A/V
// re-sync, no quality degradation. Fast even on modest hardware.
package remux

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Mode distinguishes the two HLS fragment containers. Determines
// which ffmpeg input format we prepare and how we invoke ffmpeg.
// Mirrors hls.SegmentKind but duplicated here so this package
// stays free of hls/ imports — the downloader orchestrator maps
// between them.
type Mode string

const (
	// ModeTS uses ffmpeg's concat demuxer against a segments.txt
	// listing each fragment's absolute path. Works for raw
	// MPEG-TS streams, which is what Twitch serves to most
	// channels.
	ModeTS Mode = "ts"

	// ModeFMP4 re-synthesizes a local media playlist
	// (media.m3u8) that references the on-disk fragments plus
	// the EXT-X-MAP init segment, and hands that playlist to
	// ffmpeg. fMP4 fragments can't be concat-demuxed without
	// their init header.
	ModeFMP4 Mode = "fmp4"
)

// Kind decides the output extension and is driven by the
// recording-type config (video vs audio). `.mp4` is an MP4
// container with audio+video; `.m4a` is an MP4 container with
// audio only. ffmpeg treats them identically for our -c copy
// case; the extension is only a hint for file managers + MIME
// sniffers.
type Kind string

const (
	KindVideo Kind = "video"
	KindAudio Kind = "audio"
)

// OutputExt returns the file extension for the given kind,
// including the leading dot. Unknown kinds default to .mp4 —
// safer than an empty extension that would leave the file
// un-detectable by players and MIME sniffers.
func (k Kind) OutputExt() string {
	if k == KindAudio {
		return ".m4a"
	}
	return ".mp4"
}

// PrepareInput writes ffmpeg's input description into workDir
// and returns the absolute path to it. The work directory must
// contain the HLS fragments already committed by the fetcher
// (`<mediaSeq>.ts` files for TS mode, `<mediaSeq>.m4s` files +
// `init.mp4` for fMP4 mode).
//
// Re-running PrepareInput against the same workDir is safe and
// idempotent — the generated file is overwritten. Spec Stage 5
// promises this so the restart path doesn't have to think about
// leftover input files.
//
// The function does NOT call ffmpeg; it only produces the input
// description. Separating preparation from execution lets the
// caller inspect the generated input (useful for debugging) or
// pass it to a mocked exec in tests.
//
// Returned paths are absolute so ffmpeg's working directory
// doesn't affect resolution. The concat demuxer's `-safe 0`
// handles absolute paths; we still normalize via filepath.Abs
// so relative workDir arguments work.
func PrepareInput(workDir string, mode Mode) (string, error) {
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
		return prepareTSConcat(absDir)
	case ModeFMP4:
		return prepareFMP4Playlist(absDir)
	default:
		return "", fmt.Errorf("remux: unknown mode %q", mode)
	}
}

// prepareTSConcat builds segments.txt for ffmpeg's concat demuxer.
// Format: one `file '<path>'` line per segment, sorted by
// numeric media-sequence ascending.
//
// Sort order is load-bearing: ffmpeg's concat demuxer walks the
// file in listed order and does NOT re-sort. Lexicographic sort
// on "<seq>.ts" would put "10.ts" before "2.ts" — producing a
// garbled output. The parse-and-sort-numerically step here is
// the fix.
func prepareTSConcat(absDir string) (string, error) {
	segs, err := scanSegments(absDir, ".ts")
	if err != nil {
		return "", err
	}
	if len(segs) == 0 {
		return "", fmt.Errorf("remux: no .ts segments in %q", absDir)
	}

	var b strings.Builder
	// ffmpeg's concat demuxer escapes single-quote-in-path by
	// doubling; none of our paths contain quotes (we control
	// the scratch dir), but wrap conservatively anyway.
	for _, s := range segs {
		p := filepath.Join(absDir, s.filename)
		fmt.Fprintf(&b, "file '%s'\n", strings.ReplaceAll(p, "'", "'\\''"))
	}

	outPath := filepath.Join(absDir, "segments.txt")
	if err := os.WriteFile(outPath, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("remux: write segments.txt: %w", err)
	}
	return outPath, nil
}

// prepareFMP4Playlist writes media.m3u8 referencing the local
// init segment + sorted m4s fragments. ffmpeg reads this as a
// standalone HLS input.
//
// Uses EXTINF:0 as the segment duration — correct duration values
// aren't required for -c copy (ffmpeg trusts the fragment headers)
// and we'd have to re-parse fragment timestamps to get accurate
// values, which is extra work for no gain. EXT-X-ENDLIST closes
// the playlist so ffmpeg knows it's not live.
func prepareFMP4Playlist(absDir string) (string, error) {
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
	if err := os.WriteFile(outPath, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("remux: write media.m3u8: %w", err)
	}
	return outPath, nil
}

// segmentEntry pairs a sortable media-sequence integer with the
// raw filename. Using an int for sort lets us ascend correctly
// even when the HLS seq numbers aren't zero-padded.
type segmentEntry struct {
	seq      int64
	filename string
}

// scanSegments walks the work directory, picks files matching
// the given extension, and sorts them by numeric media-sequence
// ascending. Rejects files whose basename isn't a parseable
// integer — partial files from a crashed run (if any survived
// the startup sweep), temporary editor swap files, etc., stay
// out of the ffmpeg input.
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
			// Non-numeric filename; skip silently so
			// init.mp4 / segments.txt / media.m3u8 don't
			// end up in their own inputs.
			continue
		}
		out = append(out, segmentEntry{seq: seq, filename: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].seq < out[j].seq })
	return out, nil
}
