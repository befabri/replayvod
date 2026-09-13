package twitch

import (
	"log/slog"
	"strings"
	"testing"
)

func TestSelectVariantNumericLimits(t *testing.T) {
	m := &Manifest{Variants: []Variant{
		{Quality: "1440", Codec: CodecH265, URL: "1440", FPS: 60},
		{Quality: "720", Codec: CodecH264, URL: "720", FPS: 60},
		{Quality: "160", Codec: CodecH264, URL: "160", FPS: 30},
	}}
	for _, tc := range []struct {
		quality string
		h264    bool
		want    string
	}{
		{"1440", false, "1440"}, {"best", false, "1440"}, {"1080", false, "720"},
		{"", false, "720"}, {"invalid", false, "720"}, {"1440", true, "720"}, {"best", true, "720"}, {"480", false, "160"},
	} {
		t.Run(tc.quality, func(t *testing.T) {
			got, err := SelectVariant(m, SelectOptions{Quality: tc.quality, ForceH264: tc.h264})
			if err != nil || got.Quality != tc.want {
				t.Fatalf("quality = %s, %v; want %s", got.Quality, err, tc.want)
			}
		})
	}
	m.Variants = append(m.Variants, Variant{Quality: "936", Codec: CodecH264, URL: "936-30", FPS: 30}, Variant{Quality: "936", Codec: CodecH264, URL: "936-60", FPS: 60})
	got, err := SelectVariant(m, SelectOptions{Quality: "1080"})
	if err != nil || got.URL != "936-60" {
		t.Fatalf("nonstandard height/FPS selection = %+v %v", got, err)
	}
	m.Variants = append(m.Variants, Variant{Quality: "2160", Codec: CodecH265, URL: "2160"})
	got, err = SelectVariant(m, SelectOptions{Quality: "best"})
	if err != nil || got.Quality != "2160" {
		t.Fatalf("best = %+v %v", got, err)
	}
}

func TestSelectVariantSparseMasterPlaylist(t *testing.T) {
	const playlist = `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=12000000,CODECS="hvc1.2.4.L153.B0,mp4a.40.2",RESOLUTION=2560x1440,FRAME-RATE=60,VIDEO="chunked"
source.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=3000000,CODECS="avc1.4D4020,mp4a.40.2",RESOLUTION=1280x720,FRAME-RATE=60,VIDEO="720p60"
720.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=128000,CODECS="mp4a.40.2",VIDEO="audio_only"
audio.m3u8
`
	client := &Client{log: slog.New(slog.DiscardHandler)}
	manifest, err := client.parseMasterPlaylist(strings.NewReader(playlist))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		opts SelectOptions
		url  string
	}{
		{"1440 source", SelectOptions{Quality: "1440"}, "source.m3u8"},
		{"best source", SelectOptions{Quality: "BEST"}, "source.m3u8"},
		{"high cap", SelectOptions{Quality: "1080"}, "720.m3u8"},
		{"force h264", SelectOptions{Quality: "best", ForceH264: true}, "720.m3u8"},
		{"hevc disabled", SelectOptions{Quality: "1440", DisableHEVC: true}, "720.m3u8"},
		{"audio ignores video limits", SelectOptions{RecordingType: RecordingTypeAudio, Quality: "160", ForceH264: true, DisableHEVC: true}, "audio.m3u8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SelectVariant(manifest, tc.opts)
			if err != nil || got.URL != tc.url {
				t.Fatalf("selected %+v, %v; want %s", got, err, tc.url)
			}
		})
	}
}
