// Package twitch implements Stage 1-3 of the download pipeline: the
// GQL playback-token handshake, the usher master-playlist fetch, and
// the quality/codec variant selection that picks the media playlist
// we'll actually record.
//
// This package talks only to Twitch's public streaming endpoints
// (gql.twitch.tv, usher.ttvnw.net). It does NOT talk to the Helix
// REST API — that lives in internal/twitch. The two are kept
// separate because they have different auth models, different client
// IDs, and very different lifecycles (Helix app-token vs. per-job
// playback-token).
//
// Spec: .docs/spec/download-pipeline.md stages 1-3. References:
//   - streamlink src/streamlink/plugins/twitch.py (closest mirror)
//   - yt-dlp yt_dlp/extractor/twitch.py (edge-case bug list)
package twitch

// Codec values returned by the variant selector. Match the
// video_parts.codec CHECK constraint in the database schema.
const (
	CodecH264 = "h264"
	CodecH265 = "h265"
	CodecAV1  = "av1"
	CodecAAC  = "aac" // audio-only recordings
)

// Segment format values. Derived from the media-playlist features at
// Stage 4 (presence of EXT-X-MAP → fmp4); kept here because the Stage
// 3 codec selector already knows enough about the master manifest to
// hint at which container most channels serve.
const (
	SegmentFormatTS   = "ts"
	SegmentFormatFMP4 = "fmp4"
)

// RecordingType mirrors repository.RecordingType but is duplicated
// here to keep the twitch package free of repository imports (the
// downloader package will map between them).
const (
	RecordingTypeVideo = "video"
	RecordingTypeAudio = "audio"
)

// PlaybackToken is the signed JWT + signature returned by Twitch's
// GQL PlaybackAccessToken endpoint. Both fields are opaque strings
// that we forward verbatim on every usher + media-playlist + segment
// request. They expire after ~24h in practice; refresh on 401/403.
type PlaybackToken struct {
	// Value is the signed JWT body. Passed as the `token` query
	// parameter to usher and media playlists.
	Value string

	// Signature is the HMAC over Value. Passed as the `sig` query
	// parameter. Without a matching signature, usher returns 403.
	Signature string
}

// Empty reports whether the token is zero-valued. A non-empty Value
// alone isn't enough — both fields are required for a functional
// request, and Twitch sometimes returns {value: "", signature: ""}
// on transient errors.
func (t PlaybackToken) Empty() bool {
	return t.Value == "" || t.Signature == ""
}

// Manifest is the parsed master playlist: a list of variants plus
// metadata the selector needs to filter them. Twitch encodes audio-
// only renditions as a regular EXT-X-STREAM-INF entry with a VIDEO
// group of "audio_only" (not as an EXT-X-MEDIA AUDIO alternative),
// so we don't need to track those here.
type Manifest struct {
	// Variants is the ordered list of variants from the master
	// playlist, preserving Twitch's declared order. The selector
	// filters and ranks this list; no one downstream should
	// depend on a specific ordering.
	Variants []Variant
}

// Variant is one media-playlist entry from the master. URL points
// at the media playlist Twitch's CDN will serve; the selector picks
// exactly one variant per part.
type Variant struct {
	// URL is the absolute media-playlist URL from the master.
	URL string

	// Quality is the normalized height string ("1080", "720",
	// "480", "360", "160"). For the audio-only rendition, Quality
	// is the literal string "audio_only". Source variants
	// ("chunked") are normalized to their actual height so the
	// fallback chain can compare them.
	Quality string

	// FPS is the declared FRAME-RATE. 0 means "not declared" —
	// not a useful signal on its own, and we don't filter on it.
	FPS float64

	// Codec is the first video codec from the manifest's CODECS
	// attribute, normalized to one of CodecH264/H265/AV1/AAC.
	// Empty string means "unknown codec" — such variants are
	// dropped by the selector.
	Codec string

	// GroupID is the VIDEO="..." reference from the STREAM-INF.
	// Used to identify the audio_only rendition and to pair
	// variants with their EXT-X-MEDIA alternatives if we ever
	// need them.
	GroupID string
}

// IsAudioOnly reports whether the variant is Twitch's audio_only
// rendition. Twitch always tags it with GROUP-ID="audio_only" on the
// STREAM-INF's VIDEO attribute.
func (v Variant) IsAudioOnly() bool {
	return v.GroupID == "audio_only"
}

// SelectedVariant is what Stage 3 hands back: the variant to record
// plus the normalized metadata the job row stores on video_parts.
type SelectedVariant struct {
	// URL is the media-playlist URL to hand to the Stage 4 fetch
	// loop. Always absolute.
	URL string

	// Quality is the normalized height that went into the
	// fallback chain. For audio jobs, literally "audio_only".
	Quality string

	// FPS is the selected variant's declared frame rate when the
	// master playlist exposed it. Nil means "not declared" or
	// audio_only.
	FPS *float64

	// Codec is the chosen codec: h264, h265, av1, or aac.
	Codec string
}
