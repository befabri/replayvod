package twitch

import (
	"errors"
	"sort"
	"strconv"
	"strings"
)

// ErrNoAudioRendition is returned when SelectVariant is called
// with RecordingType="audio" against a manifest that has no
// audio_only rendition.
var ErrNoAudioRendition = errors.New("twitch: no audio_only rendition in master playlist")

// ErrNoAcceptableVariant is returned when Stage 3 filters leave
// zero variants matching the caller's codec + quality constraints,
// for instance ForceH264 against an HEVC-only channel.
var ErrNoAcceptableVariant = errors.New("twitch: no variant matches codec + quality constraints")

// SelectOptions carries everything the Stage 3 selector needs.
// None of the boolean flags are mutually exclusive; all filters
// combine with AND semantics.
type SelectOptions struct {
	// RecordingType: "video" or "audio". "audio" short-circuits
	// all codec/quality logic and picks the audio_only rendition.
	RecordingType string

	// Quality is a maximum numeric height (including nonstandard heights),
	// or "best" for no cap. Empty/invalid defaults to 1080. Ignored for audio.
	Quality string

	// EnableAV1 opts into AV1 variants at Stage 3. Matches
	// cfg.Download.EnableAV1.
	EnableAV1 bool

	// DisableHEVC drops hvc1/hev1 variants even when the channel
	// serves them. Escape hatch for operators whose ffmpeg build
	// or downstream player can't decode HEVC.
	DisableHEVC bool

	// ForceH264 is the per-job override (videos.force_h264).
	// Drops both HEVC and AV1, leaving an H.264-only pool.
	ForceH264 bool
}

// SelectVariant is Stage 3: given a parsed master playlist and the
// operator's preferences, return exactly the variant to record.
func SelectVariant(m *Manifest, opts SelectOptions) (SelectedVariant, error) {
	if m.isEmpty() {
		return SelectedVariant{}, ErrNoAcceptableVariant
	}
	if opts.RecordingType == RecordingTypeAudio {
		return selectAudioVariant(m)
	}
	pool := AcceptableVariants(m, opts)
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

// selectAudioVariant returns the audio_only rendition, or ErrNoAudioRendition
// when the manifest carries none.
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

// AcceptableVariants is the pool SelectVariant chooses from: the manifest's
// renditions minus audio_only, codecs the options exclude, and any whose
// height the manifest does not state. Callers that list what a recording
// could pick from use it directly.
func AcceptableVariants(m *Manifest, opts SelectOptions) []Variant {
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

// SortByPreference orders a pool tallest first and, within a height, the way
// SelectVariant decides between renditions: preferred codec, then frame rate.
// The first entry of each height is the one a cap at that height records.
func SortByPreference(pool []Variant) {
	sort.SliceStable(pool, func(i, j int) bool {
		hi, _ := strconv.Atoi(pool[i].Quality)
		hj, _ := strconv.Atoi(pool[j].Quality)
		if hi != hj {
			return hi > hj
		}
		return betterVariant(&pool[i], &pool[j])
	})
}

// betterVariant reports whether candidate should displace current at the same
// height: codec preference decides first, then frame rate rather than playlist
// order.
func betterVariant(candidate, current *Variant) bool {
	if codecRank(candidate.Codec) != codecRank(current.Codec) {
		return codecRank(candidate.Codec) > codecRank(current.Codec)
	}
	return candidate.FPS > current.FPS
}

// selectedFrom builds the result for a chosen variant, reporting frame rate by
// pointer only when it is known (positive).
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

// codecAllowed applies the three codec filters in one place. HEVC
// is dropped when either DisableHEVC or ForceH264 is on; AV1 is
// dropped unless EnableAV1 is explicitly on AND ForceH264 is off.
func codecAllowed(codec string, opts SelectOptions) bool {
	switch codec {
	case CodecH264:
		return true
	case CodecH265:
		return !opts.DisableHEVC && !opts.ForceH264
	case CodecAV1:
		return opts.EnableAV1 && !opts.ForceH264
	}
	// Unknown codec → drop. The manifest capability gate in Stage 4 does the
	// same thing for unsupported containers.
	return false
}

// codecRank orders codecs by preference at equal quality; higher wins. H.264
// is the baseline, HEVC is preferred when Twitch offers it, and AV1 only beats
// H.264. Unknown codecs rank -1 and never win.
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
