package storagekeys

import "testing"

// TestKeys pins the exact object-store layout every consumer depends on. These
// strings are a contract: the downloader writer, the video API, and retention
// all route through these functions, so a change here that didn't update every
// site is impossible — but a change that silently alters the layout (and thus
// orphans every previously written object) is caught here.
func TestKeys(t *testing.T) {
	if got, want := Video("rec-part01.mp4"), "videos/rec-part01.mp4"; got != want {
		t.Errorf("Video = %q, want %q", got, want)
	}
	if got, want := Thumbnail("rec-part01"), "thumbnails/rec-part01.jpg"; got != want {
		t.Errorf("Thumbnail = %q, want %q", got, want)
	}
	if got, want := Strip("rec-part01"), "thumbnails/rec-part01-strip.jpg"; got != want {
		t.Errorf("Strip = %q, want %q", got, want)
	}
	if got, want := Snapshot("rec", 0), "thumbnails/rec-snap00.jpg"; got != want {
		t.Errorf("Snapshot(0) = %q, want %q", got, want)
	}
	if got, want := Snapshot("rec", 7), "thumbnails/rec-snap07.jpg"; got != want {
		t.Errorf("Snapshot(7) = %q, want %q", got, want)
	}
	if got, want := Snapshot("rec", 42), "thumbnails/rec-snap42.jpg"; got != want {
		t.Errorf("Snapshot(42) = %q, want %q", got, want)
	}
}

// TestBase covers the extension strip that derives a part's thumbnail base from
// its stored video filename, and the round-trip Video(name) / Thumbnail(Base(name))
// the writer and retention rely on.
func TestBase(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"rec-part01.mp4", "rec-part01"},
		{"rec-part01.aac", "rec-part01"},
		{"rec-part01", "rec-part01"}, // no extension: unchanged
		{"a.b.mp4", "a.b"},           // only the final extension is stripped
	}
	for _, tc := range cases {
		if got := Base(tc.name); got != tc.want {
			t.Errorf("Base(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
	// The writer stores videos/<name> and thumbnails/<base>.jpg; retention must
	// reconstruct the thumbnail from the stored video name. Pin that they line up.
	const name = "rec-part01.mp4"
	if Thumbnail(Base(name)) != "thumbnails/rec-part01.jpg" {
		t.Errorf("Thumbnail(Base(%q)) = %q, want thumbnails/rec-part01.jpg", name, Thumbnail(Base(name)))
	}
}

func TestPlaybackName(t *testing.T) {
	cases := []struct {
		videoFilename string
		partFilename  string
		want          string
	}{
		{"rec", "rec-part01.mp4", "rec-playback.mp4"},
		{"rec", "rec-part01.m4a", "rec-playback.m4a"},
		{"rec", "rec-part01.MP4", "rec-playback.mp4"}, // extension lowercased
	}
	for _, tc := range cases {
		if got := PlaybackName(tc.videoFilename, tc.partFilename); got != tc.want {
			t.Errorf("PlaybackName(%q, %q) = %q, want %q", tc.videoFilename, tc.partFilename, got, tc.want)
		}
	}
	// playbackcache builds the artifact and retention deletes it; both must agree
	// on the storage key. Pin that PlaybackName feeds Video() to the expected key.
	if Video(PlaybackName("rec", "rec-part01.mp4")) != "videos/rec-playback.mp4" {
		t.Errorf("Video(PlaybackName(...)) = %q, want videos/rec-playback.mp4", Video(PlaybackName("rec", "rec-part01.mp4")))
	}
}
