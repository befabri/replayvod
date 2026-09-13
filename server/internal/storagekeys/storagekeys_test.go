package storagekeys

import "testing"

// TestKeys preserves the layout of existing objects across changes to key helpers.
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
	if got, want := Waveform("rec"), "thumbnails/rec-waveform.json"; got != want {
		t.Errorf("Waveform = %q, want %q", got, want)
	}
}

func TestBase(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"rec-part01.mp4", "rec-part01"},
		{"rec-part01.aac", "rec-part01"},
		{"rec-part01", "rec-part01"},
		{"a.b.mp4", "a.b"},
	}
	for _, tc := range cases {
		if got := Base(tc.name); got != tc.want {
			t.Errorf("Base(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
	const name = "rec-part01.mp4"
	if Thumbnail(Base(name)) != "thumbnails/rec-part01.jpg" {
		t.Errorf("Thumbnail(Base(%q)) = %q, want thumbnails/rec-part01.jpg", name, Thumbnail(Base(name)))
	}
}
