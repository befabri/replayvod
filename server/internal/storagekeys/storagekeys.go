// Package storagekeys defines storage-relative object keys with forward slashes.
// Readers and cleanup use persisted media references; callers choose unique
// names for immutable generations.
//
// Layout:
//
//	videos/<name>                       media, including its container extension
//	thumbnails/<base>.jpg                part thumbnail, excluding the container extension
//	thumbnails/<base>-strip.jpg          part sprite strip
//	thumbnails/<filename>-snapNN.jpg     recording snapshot position
//	thumbnails/<name>-waveform.json      waveform generation
package storagekeys

import (
	"fmt"
	"path"
	"strings"

	"github.com/befabri/replayvod/server/internal/repository"
)

const (
	videoDir = "videos"
	thumbDir = "thumbnails"
)

// Video returns the media key for a stored filename including its extension.
func Video(name string) string {
	return videoDir + "/" + name
}

// Thumbnail returns a part thumbnail key from its name without a container extension.
func Thumbnail(base string) string {
	return thumbDir + "/" + base + ".jpg"
}

// Strip returns a part sprite key from its name without a container extension.
func Strip(base string) string {
	return thumbDir + "/" + base + "-strip.jpg"
}

// Snapshot returns the key for a recording snapshot position.
// The publication journal retains failed positions for discovery across gaps.
func Snapshot(filename string, index int) string {
	return fmt.Sprintf("%s/%s-snap%02d.jpg", thumbDir, filename, index)
}

// Waveform returns the artifact key for a waveform generation name.
func Waveform(filename string) string {
	return thumbDir + "/" + filename + "-waveform.json"
}

// Base strips the container extension to derive a part thumbnail or strip key.
func Base(name string) string {
	return strings.TrimSuffix(name, path.Ext(name))
}

// MediaPaths reads the authoritative part references, including migrated media.
func MediaPaths(parts []repository.VideoPart) []string {
	paths := make([]string, len(parts))
	for i, p := range parts {
		paths[i] = Video(p.Filename)
	}
	return paths
}
