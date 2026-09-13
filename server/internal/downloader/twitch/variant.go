package twitch

import (
	"errors"
	"strconv"
	"strings"
)

// ErrNoAudioRendition indicates that a nonempty manifest has no audio_only rendition.
var ErrNoAudioRendition = errors.New("twitch: no audio_only rendition in master playlist")

// ErrNoAcceptableVariant indicates an empty manifest or no video rendition
// matching the requested codec and height limits.
var ErrNoAcceptableVariant = errors.New("twitch: no variant matches codec + quality constraints")

// SelectOptions constrains the recording rendition; codec filters combine.
type SelectOptions struct {
	// RecordingType is "video" or "audio"; audio ignores codec and quality limits.
	RecordingType string

	// Quality is a positive height in pixels, or "best" for no cap.
	// Empty or invalid values default to 1080; audio ignores this value.
	Quality string

	EnableAV1 bool

	// DisableHEVC excludes hvc1/hev1 variants for players that cannot decode HEVC.
	DisableHEVC bool

	// ForceH264 excludes HEVC and AV1, even when EnableAV1 is set.
	ForceH264 bool
}

// SelectVariant returns the highest eligible rendition within the height limit.
// At equal height it prefers HEVC, then AV1, then H.264, followed by frame rate.
// Audio requests select the audio_only rendition regardless of video settings.
func SelectVariant(m *Manifest, opts SelectOptions) (SelectedVariant, error) {
	if m.isEmpty() {
		return SelectedVariant{}, ErrNoAcceptableVariant
	}
	if opts.RecordingType == RecordingTypeAudio {
		return selectAudioVariant(m)
	}
	pool := acceptableVariants(m, opts)
	if len(pool) == 0 {
		return SelectedVariant{}, ErrNoAcceptableVariant
	}
	limit := qualityLimit(opts.Quality)
	var best *Variant
	bestHeight := 0
	for i := range pool {
		v := &pool[i]
		height, err := strconv.Atoi(v.Quality)
		if err != nil || height <= 0 || (limit > 0 && height > limit) {
			continue
		}
		if best == nil || height > bestHeight || (height == bestHeight && betterVariant(v, best)) {
			best, bestHeight = v, height
		}
	}
	if best != nil {
		return selectedFrom(best), nil
	}
	return SelectedVariant{}, ErrNoAcceptableVariant
}

func (m *Manifest) isEmpty() bool {
	return m == nil || len(m.Variants) == 0
}

func selectAudioVariant(m *Manifest) (SelectedVariant, error) {
	for _, v := range m.Variants {
		if v.IsAudioOnly() {
			return SelectedVariant{
				URL:     v.URL,
				Quality: "audio_only",
				FPS:     nil,
				Codec:   CodecAAC,
			}, nil
		}
	}
	return SelectedVariant{}, ErrNoAudioRendition
}

func acceptableVariants(m *Manifest, opts SelectOptions) []Variant {
	pool := make([]Variant, 0, len(m.Variants))
	for _, v := range m.Variants {
		if v.IsAudioOnly() {
			continue
		}
		if !codecAllowed(v.Codec, opts) {
			continue
		}
		if v.Quality == "" {
			continue
		}
		pool = append(pool, v)
	}
	return pool
}

// qualityLimit returns the maximum height a request allows, or 0 for no cap.
func qualityLimit(requested string) int {
	if strings.EqualFold(requested, "best") {
		return 0
	}
	height, err := strconv.Atoi(requested)
	if err != nil || height <= 0 {
		return 1080
	}
	return height
}

func betterVariant(candidate, current *Variant) bool {
	if codecRank(candidate.Codec) != codecRank(current.Codec) {
		return codecRank(candidate.Codec) > codecRank(current.Codec)
	}
	return candidate.FPS > current.FPS
}

func selectedFrom(best *Variant) SelectedVariant {
	var fps *float64
	if best.FPS > 0 {
		v := best.FPS
		fps = &v
	}
	return SelectedVariant{
		URL:     best.URL,
		Quality: best.Quality,
		FPS:     fps,
		Codec:   best.Codec,
	}
}

func codecAllowed(codec string, opts SelectOptions) bool {
	switch codec {
	case CodecH264:
		return true
	case CodecH265:
		return !opts.DisableHEVC && !opts.ForceH264
	case CodecAV1:
		return opts.EnableAV1 && !opts.ForceH264
	}
	return false
}

func codecRank(codec string) int {
	switch codec {
	case CodecH265:
		return 3
	case CodecAV1:
		return 2
	case CodecH264:
		return 1
	default:
		return -1
	}
}
