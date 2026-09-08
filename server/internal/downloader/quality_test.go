package downloader

import (
	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/repository"
	"testing"
)

func TestSavedQualitySelectsBoundedRendition(t *testing.T) {
	manifest := &twitch.Manifest{}
	for _, height := range []string{"480", "720", "1080", "1440", "2160"} {
		manifest.Variants = append(manifest.Variants, twitch.Variant{Quality: height, Codec: twitch.CodecH264, URL: height})
	}
	for quality, want := range map[string]string{repository.QualityLow: "480", repository.QualityMedium: "720", repository.QualityHigh: "1080", repository.Quality1440: "1440", repository.QualityBest: "2160"} {
		t.Run(quality, func(t *testing.T) {
			got, err := twitch.SelectVariant(manifest, twitch.SelectOptions{Quality: qualityToHeight(quality)})
			if err != nil || got.URL != want {
				t.Fatalf("selected %+v, %v; want %s", got, err, want)
			}
		})
	}
}
