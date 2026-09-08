package twitch

import (
	"testing"
)

func TestSelectVariantNumericLimits(t *testing.T) {
	// The reported 1440/720 ladder must not require a fictitious 1080 tier.
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
	// Best remains useful when Twitch adds higher tiers.
	m.Variants = append(m.Variants, Variant{Quality: "2160", Codec: CodecH265, URL: "2160"})
	got, err = SelectVariant(m, SelectOptions{Quality: "best"})
	if err != nil || got.Quality != "2160" {
		t.Fatalf("best = %+v %v", got, err)
	}
}
