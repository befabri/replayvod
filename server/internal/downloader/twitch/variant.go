package twitch

import (
	"errors"
)

// ErrNoAudioRendition is returned when SelectVariant is called
// with RecordingType="audio" against a manifest that has no
// audio_only rendition. Unobserved in any of the spec's 10
// captures, but defensible — Twitch could drop it for a channel
// without us noticing until a job fails.
var ErrNoAudioRendition = errors.New("twitch: no audio_only rendition in master playlist")

// ErrNoAcceptableVariant is returned when Stage 3 filters leave
// zero variants matching the caller's codec + quality constraints.
// Seen in practice when ForceH264=true against a hypothetical HEVC-
// only channel (none observed), or EnableAV1=false + an AV1-only
// manifest.
var ErrNoAcceptableVariant = errors.New("twitch: no variant matches codec + quality constraints")

// qualityFallbackChain mirrors the v1 fallback matrix
// (.docs/spec/download-pipeline.md Stage 3). Requested quality
// resolves by trying itself first, then each fallback in order.
var qualityFallbackChain = map[string][]string{
	"1080": {"1080", "720", "480", "360"},
	"720":  {"720", "480", "360"},
	"480":  {"480", "360", "160"},
	"360":  {"360", "160"},
	"160":  {"160"},
}

// SelectOptions carries everything the Stage 3 selector needs.
// None of the boolean flags are mutually exclusive; all filters
// combine with AND semantics.
type SelectOptions struct {
	// RecordingType: "video" or "audio". "audio" short-circuits
	// all codec/quality logic and picks the audio_only rendition.
	RecordingType string

	// Quality is the requested numeric-string height ("1080" ...
	// "160"). Empty defaults to "1080" — same default as the
	// v1 downloader. Ignored when RecordingType="audio".
	Quality string

	// EnableAV1 opts into AV1 variants at Stage 3. Matches
	// cfg.Download.EnableAV1.
	EnableAV1 bool

	// DisableHEVC drops hvc1/hev1 variants even when the channel
	// serves them. Escape hatch for operators whose ffmpeg build
	// or downstream player can't decode HEVC.
	DisableHEVC bool

	// ForceH264 is the per-job override (videos.force_h264).
	// Drops both HEVC and AV1 before the quality chain runs.
	// When true, effectively restricts the pool to H.264 variants.
	ForceH264 bool
}

// SelectVariant is Stage 3: given a parsed master playlist and the
// operator's preferences, return exactly the variant to record.
// Ordering inside SelectOptions doesn't matter — the filter is
// commutative.
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
	for _, want := range fallbackChain(opts.Quality) {
		if best := pickByCodecPreference(pool, want); best != nil {
			return selectedFrom(best), nil
		}
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

// acceptableVariants drops the renditions a video job can never pick: audio_only
// (a manifest-shape fixture that keeps the response matching what Twitch ships
// the web player), codecs the options exclude, and any whose height is unknown
// and so has no place on the fallback chain.
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

// fallbackChain resolves a requested quality to its highest-to-lowest search
// order. An empty request defaults to 1080; a non-standard one (e.g. "1440")
// borrows the 1080 chain, the search order Twitch actually supports.
func fallbackChain(requested string) []string {
	if requested == "" {
		requested = "1080"
	}
	if chain, ok := qualityFallbackChain[requested]; ok {
		return chain
	}
	return qualityFallbackChain["1080"]
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

// pickByCodecPreference finds the best variant for a target
// quality. If multiple variants match the same quality, prefer
// HEVC over H.264 and AV1 over HEVC (matching the spec's "prefer
// hvc1 over avc1 at equal quality" rule). The preference order
// lives in codecRank below.
//
// Returns nil when no variant matches.
func pickByCodecPreference(pool []Variant, quality string) *Variant {
	var best *Variant
	bestRank := -1
	for i := range pool {
		v := &pool[i]
		if v.Quality != quality {
			continue
		}
		rank := codecRank(v.Codec)
		if rank > bestRank {
			best = v
			bestRank = rank
		}
	}
	return best
}

// codecRank orders codecs by "prefer when equal quality." Higher is
// better. H.264 is the baseline; HEVC is the spec's preferred codec
// when Twitch offers it; AV1 is an optional third tier that only
// wins over H.264, not over HEVC. Rationale: the spec's Overview
// flags HEVC as "supported and preferred whenever Twitch offers
// it" and AV1 as "optional behind config" — HEVC is the mature
// codec on this pipeline, AV1 is experimental. Unknown codecs
// rank -1 and never win.
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
