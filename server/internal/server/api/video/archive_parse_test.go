package video

import (
	"testing"

	"github.com/befabri/replayvod/server/internal/twitch"
)

func TestParseVODID(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"123456789", "123456789", true},
		{"  123456789\n", "123456789", true},
		{"https://www.twitch.tv/videos/2233445566", "2233445566", true},
		{"http://twitch.tv/videos/2233445566/", "2233445566", true},
		{"twitch.tv/videos/2233445566", "2233445566", true},
		{"https://m.twitch.tv/videos/2233445566?t=1h2m3s", "2233445566", true},
		{"https://www.twitch.tv/videos/2233445566#comment", "2233445566", true},
		{"https://www.twitch.tv/somestreamer/video/2233445566", "2233445566", true},
		{"https://www.twitch.tv/somestreamer/v/2233445566", "2233445566", true},
		{"https://WWW.TWITCH.TV/VIDEOS/99", "99", true},
		// Not VODs.
		{"", "", false},
		{"abc", "", false},
		{"12a34", "", false},
		{"https://www.twitch.tv/somestreamer", "", false},
		{"https://www.twitch.tv/somestreamer/clip/FunnyClip", "", false},
		{"https://clips.twitch.tv/FunnyClip", "", false},
		{"https://www.youtube.com/watch?v=123", "", false},
		{"https://example.com/videos/123", "", false},
		{"https://evil-twitch.tv/videos/123", "", false},
		{"https://www.twitch.tv/videos/", "", false},
		{"https://www.twitch.tv/videos/123/extra", "", false},
	}
	for _, tc := range cases {
		got, ok := ParseVODID(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseVODID(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseChannelLogin(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"somestreamer", "somestreamer", true},
		{"SomeStreamer", "somestreamer", true},
		{"@somestreamer", "somestreamer", true},
		{"  some_streamer_1 ", "some_streamer_1", true},
		{"https://www.twitch.tv/somestreamer", "somestreamer", true},
		{"https://www.twitch.tv/SomeStreamer/videos?filter=archives&sort=time", "somestreamer", true},
		{"twitch.tv/somestreamer/", "somestreamer", true},
		{"https://m.twitch.tv/somestreamer/about", "somestreamer", true},
		// Not channels.
		{"", "", false},
		{"@", "", false},
		{"a", "", false},
		{"has space", "", false},
		{"https://www.twitch.tv/videos/123", "", false},
		{"https://www.twitch.tv/somestreamer/video/123", "", false},
		{"https://www.twitch.tv/somestreamer/v/123", "", false},
		{"https://www.twitch.tv/directory/category/x", "", false},
		{"https://www.twitch.tv/", "", false},
		{"https://example.com/somestreamer", "", false},
		{"this-login-is-way-too-long-for-twitch", "", false},
	}
	for _, tc := range cases {
		got, ok := ParseChannelLogin(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseChannelLogin(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseTwitchDuration(t *testing.T) {
	cases := map[string]int{
		"3h20m5s": 3*3600 + 20*60 + 5,
		"58m1s":   58*60 + 1,
		"45s":     45,
		"2h":      7200,
		"1h30m":   5400,
		"":        0,
		"weird":   0,
		"10":      0,
	}
	for in, want := range cases {
		if got := ParseTwitchDuration(in); got != want {
			t.Errorf("ParseTwitchDuration(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestVODThumbnailURL(t *testing.T) {
	got := twitch.VideoThumbnailURL("https://static-cdn.jtvnw.net/cf_vods/x/thumb/thumb0-%{width}x%{height}.jpg", 320, 180)
	if got != "https://static-cdn.jtvnw.net/cf_vods/x/thumb/thumb0-320x180.jpg" {
		t.Errorf("thumbnail url = %q", got)
	}
	if got := twitch.VideoThumbnailURL("", 640, 360); got != "" {
		t.Errorf("empty template = %q", got)
	}
}
