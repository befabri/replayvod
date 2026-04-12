package twitch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMasterPlaylist_Altair(t *testing.T) {
	f := openFixture(t, "master-altair.m3u8")
	defer f.Close()

	m, err := parseMasterPlaylist(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// altair capture (anonymous): 1080p60, 720p60, 360p30, audio_only.
	// All H.264, all from the enhanced_broadcast transcode stack.
	if len(m.Variants) < 4 {
		t.Fatalf("expected at least 4 variants, got %d", len(m.Variants))
	}

	want := map[string]struct {
		codec    string
		groupIDs []string
	}{
		"1080":       {CodecH264, []string{"1080p60"}},
		"720":        {CodecH264, []string{"720p60"}},
		"360":        {CodecH264, []string{"360p30"}},
		"audio_only": {CodecAAC, []string{"audio_only"}},
	}
	seen := map[string]bool{}
	for _, v := range m.Variants {
		t.Logf("variant: quality=%s codec=%s group=%s", v.Quality, v.Codec, v.GroupID)
		if w, ok := want[v.Quality]; ok {
			if v.Codec != w.codec {
				t.Errorf("quality %s: codec=%s, want %s", v.Quality, v.Codec, w.codec)
			}
			seen[v.Quality] = true
		}
	}
	for q := range want {
		if !seen[q] {
			t.Errorf("missing expected quality %q", q)
		}
	}
}

func TestParseMasterPlaylist_Kato(t *testing.T) {
	// The "kato" fixture is actually another H.264/TS anonymous
	// capture — we re-use the playlist shape to sanity-check the
	// parser against a different transcode ladder (chunked + 720 +
	// 480 + 360 + 160 + audio_only).
	f := openFixture(t, "master-kato.m3u8")
	defer f.Close()

	m, err := parseMasterPlaylist(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(m.Variants) == 0 {
		t.Fatal("expected variants, got 0")
	}
	// Every variant except audio_only should be H.264.
	for _, v := range m.Variants {
		if v.IsAudioOnly() {
			if v.Codec != CodecAAC {
				t.Errorf("audio_only codec=%s, want %s", v.Codec, CodecAAC)
			}
			continue
		}
		if v.Codec != CodecH264 {
			t.Errorf("variant %s codec=%s, want %s", v.Quality, v.Codec, CodecH264)
		}
	}
}

func TestParseMasterPlaylist_HEVCSynthetic(t *testing.T) {
	// Synthetic manifest because we don't have a real authenticated
	// HEVC capture. Mirrors what the spec says altair serves to a
	// Turbo + subscriber viewer: a 1440 HEVC source, a 1080 H.264
	// fallback, a 1080 HEVC alternative (rarely offered in practice
	// but useful for the codec-preference test), 720 H.264, and
	// audio_only.
	f := openFixture(t, "master-hevc-synthetic.m3u8")
	defer f.Close()

	m, err := parseMasterPlaylist(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var foundHEVC, foundH264 bool
	for _, v := range m.Variants {
		switch v.Codec {
		case CodecH265:
			foundHEVC = true
		case CodecH264:
			foundH264 = true
		}
	}
	if !foundHEVC {
		t.Error("expected at least one HEVC variant in synthetic fixture")
	}
	if !foundH264 {
		t.Error("expected at least one H.264 variant in synthetic fixture")
	}
}

func TestNormalizeQuality(t *testing.T) {
	cases := []struct {
		resolution string
		groupID    string
		want       string
	}{
		{"1920x1080", "1080p60", "1080"},
		{"1280x720", "720p60", "720"},
		{"640x360", "360p30", "360"},
		{"284x160", "160p30", "160"},
		{"2560x1440", "chunked", "1440"},
		{"", "audio_only", "audio_only"},
		// Source variant without a declared resolution — fall back
		// to parsing the group ID.
		{"", "1080p60", "1080"},
		// Unknown shape — empty result, selector drops it.
		{"", "", ""},
		{"garbage", "garbage", ""},
	}
	for _, c := range cases {
		got := normalizeQuality(c.resolution, c.groupID)
		if got != c.want {
			t.Errorf("normalizeQuality(%q, %q) = %q, want %q",
				c.resolution, c.groupID, got, c.want)
		}
	}
}

func TestPrimaryCodec(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"avc1.4D401F,mp4a.40.2", CodecH264},
		{"avc1.64002A,mp4a.40.2", CodecH264},
		{"hvc1.2.4.L150.B0,mp4a.40.2", CodecH265},
		{"hev1.1.6.L150.90.0.0.0.0.0,mp4a.40.2", CodecH265},
		{"av01.0.08M.08,mp4a.40.2", CodecAV1},
		{"mp4a.40.2", CodecAAC},
		{"", ""},
		{"unknown", ""},
	}
	for _, c := range cases {
		got := primaryCodec(c.in)
		if got != c.want {
			t.Errorf("primaryCodec(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func openFixture(t *testing.T, name string) *os.File {
	t.Helper()
	p := filepath.Join("testdata", name)
	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("open fixture %s: %v", p, err)
	}
	return f
}
