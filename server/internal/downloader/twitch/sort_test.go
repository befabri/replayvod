package twitch

import (
	"strconv"
	"testing"
)

func TestSortByPreferenceOrdersTallestThenAsSelectVariantWould(t *testing.T) {
	pool := []Variant{
		{Quality: "720", Codec: CodecH264, FPS: 30},
		{Quality: "1080", Codec: CodecH264, FPS: 60},
		{Quality: "1080", Codec: CodecH265, FPS: 60},
		{Quality: "720", Codec: CodecH264, FPS: 60},
		{Quality: "1440", Codec: CodecH265, FPS: 60},
		{Quality: "1080", Codec: CodecH264, FPS: 30},
	}
	SortByPreference(pool)
	for i := 1; i < len(pool); i++ {
		prev, cur := pool[i-1], pool[i]
		hp, _ := strconv.Atoi(prev.Quality)
		hc, _ := strconv.Atoi(cur.Quality)
		if hp < hc {
			t.Fatalf("index %d: %s before %s, want tallest first", i, prev.Quality, cur.Quality)
		}
		if hp == hc && betterVariant(&cur, &prev) {
			t.Fatalf("index %d: %+v should precede %+v at the same height", i, cur, prev)
		}
	}
	// The first row of each height is exactly what a cap at that height picks.
	for _, height := range []string{"1440", "1080", "720"} {
		want, err := SelectVariant(&Manifest{Variants: pool}, SelectOptions{Quality: height})
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range pool {
			if v.Quality == height {
				if v.Codec != want.Codec || v.FPS != *want.FPS {
					t.Fatalf("first %sp row %+v, SelectVariant picks %+v", height, v, want)
				}
				break
			}
		}
	}
}
