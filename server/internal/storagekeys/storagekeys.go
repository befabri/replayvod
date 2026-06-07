// Package storagekeys is the single source of truth for the object-store keys a
// recording owns: the per-part video file, its hero thumbnail and sprite strip,
// the live-snapshot JPEGs, and the audio waveform artifact. The downloader
// writes objects at these keys, the video API serves and lists them, and
// retention deletes them — packages that must agree exactly or retention
// silently leaks orphaned files. Centralize the layout here so a change to the
// naming is a compile-time concern at every site instead of a drift no test
// would catch.
//
// Layout (all keys are forward-slash storage-relative):
//
//	videos/<name>                     the recorded file, name includes its extension
//	thumbnails/<base>.jpg             the part hero thumbnail (base = name without extension)
//	thumbnails/<base>-strip.jpg       the part sprite strip
//	thumbnails/<filename>-snapNN.jpg  the NN-th live snapshot (filename = video base name)
//	thumbnails/<filename>-waveform.json the audio waveform artifact
package storagekeys

import (
	"fmt"
	"path"
	"strings"
)

const (
	videoDir = "videos"
	thumbDir = "thumbnails"
)

func Video(name string) string {
	return videoDir + "/" + name
}

func Thumbnail(base string) string {
	return thumbDir + "/" + base + ".jpg"
}

func Strip(base string) string {
	return thumbDir + "/" + base + "-strip.jpg"
}

// Snapshot returns the key for the index-th live snapshot of a recording.
// filename is the video's base name (no extension). The snapshot writer advances
// index only on a successful write, so a recording's snapshots form a contiguous
// 0..k run — which is what lets the lister and retention probe until the first
// gap.
func Snapshot(filename string, index int) string {
	return fmt.Sprintf("%s/%s-snap%02d.jpg", thumbDir, filename, index)
}

func Waveform(filename string) string {
	return thumbDir + "/" + filename + "-waveform.json"
}

// Base strips a stored filename's container extension, yielding the base that
// Thumbnail and Strip expect. A part stores "videos/<base><ext>" and its
// thumbnails as "thumbnails/<base>.jpg", so retention derives base from the
// part's stored filename to find the thumbnails.
func Base(name string) string {
	return strings.TrimSuffix(name, path.Ext(name))
}

// PlaybackName returns the stored filename of a recording's playback-cache
// artifact: the video's base name plus a "-playback" suffix and the container
// extension of its first part. It is the value written to
// video_playback_assets.filename and passed to Video(). The playback-cache
// builder and retention must agree on this exactly or retention silently leaks
// the artifact, so the naming lives here with the rest of a recording's layout.
// partFilename is the first part's stored filename (e.g. "rec-part01.mp4").
func PlaybackName(videoFilename, partFilename string) string {
	return videoFilename + "-playback" + strings.ToLower(path.Ext(partFilename))
}
