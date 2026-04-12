// Package hls implements the media-playlist side of Stage 4 of the
// download pipeline: parsing the per-variant playlist Twitch's CDN
// serves, gating on unsupported features, and (in Phase 4b) the
// segment worker pool that fetches and writes .ts / .m4s files to
// disk.
//
// The master-playlist side (Stage 2) + variant selection (Stage 3)
// lives in internal/downloader/twitch. The orchestration that
// stitches them together (Phase 4c) lives in the parent downloader
// package.
//
// Why a separate package from twitch/: the media playlist talks to
// an arbitrary CDN edge URL — not a Twitch-branded host. The
// twitch/ package concentrates Twitch-specific auth (gql +
// integrity + playback token); once a playback token is in hand,
// segment fetching is generic HLS and doesn't need Twitch's client
// ID or integrity cache.
package hls

import "time"

// SegmentKind distinguishes the two HLS fragment containers we
// support. "ts" is MPEG-TS (the common case across anonymous
// captures); "fmp4" is CMAF / fMP4 with an EXT-X-MAP init segment.
// Spec Stage 4: these are the two supported paths; everything else
// (byterange, encryption, LL-HLS parts) gets rejected at the gate.
type SegmentKind string

const (
	SegmentKindTS   SegmentKind = "ts"
	SegmentKindFMP4 SegmentKind = "fmp4"
)

// Segment is one media fragment parsed out of the media playlist.
// Fields are minimal by design: the parser thins Eyevinn's rich
// MediaSegment down to what the fetch loop actually consults. All
// ad-stitching / muted-DMCA / gap-reason metadata gets added in
// Phase 4b when the fetch loop needs it.
type Segment struct {
	// MediaSeq is the segment's EXT-X-MEDIA-SEQUENCE position
	// (the playlist's base seqNo plus the segment's offset).
	// Stable across playlist refreshes: the fetch loop dedupes
	// by this value.
	MediaSeq int64

	// URI is the segment URL as it appeared in the playlist.
	// Relative URIs are resolved against the playlist URL by
	// the parser caller, not here.
	URI string

	// Duration is EXTINF in seconds. Used by the progress
	// reporter to estimate remaining time; the fetcher doesn't
	// care.
	Duration float64

	// Discontinuity is true when the preceding tag was
	// EXT-X-DISCONTINUITY. The orchestrator's ad-gap logic
	// (Phase 4b) walks these boundaries; the fetcher itself
	// just records the flag.
	Discontinuity bool
}

// InitSegment is the #EXT-X-MAP entry pointing at the fMP4
// initialization section (usually a small MP4 moov+ftyp). Only
// present when MediaPlaylist.Kind = SegmentKindFMP4.
type InitSegment struct {
	URI string
}

// MediaPlaylist is the parsed per-variant playlist. All fields
// except Segments are inputs to the fetch loop's control flow:
// Kind chooses the segment-file extension, TargetDuration drives
// the poll interval, EndList is the termination signal for VOD
// (or post-stream live), MediaSequenceBase anchors the seqNo
// math on the first refresh.
type MediaPlaylist struct {
	// Kind distinguishes TS from fMP4. Determined by the
	// presence of EXT-X-MAP (fMP4) or its absence (TS). Not
	// by URI suffix — some Twitch fMP4 playlists use .mp4 URIs
	// and a `.ts`-named variant can theoretically be fMP4.
	Kind SegmentKind

	// Init is the EXT-X-MAP init segment when Kind=fmp4, nil
	// for TS. A non-nil Init with Kind=ts is malformed and the
	// parser rejects it before returning.
	Init *InitSegment

	// TargetDuration is EXT-X-TARGETDURATION rounded to whole
	// seconds (the spec requires integer values ≥ max actual
	// segment duration). The poll loop uses this as its tick
	// interval.
	TargetDuration time.Duration

	// MediaSequenceBase is EXT-X-MEDIA-SEQUENCE — the
	// MediaSeq of Segments[0]. 0 is a valid base (a freshly
	// started stream before the window has slid).
	MediaSequenceBase int64

	// Segments is the ordered list of media fragments in this
	// playlist snapshot. Live playlists publish a sliding
	// window; VOD / post-stream publishes the whole thing.
	Segments []Segment

	// EndList is true when EXT-X-ENDLIST was present —
	// "playlist complete, no more segments will be appended."
	// Fetch loop's termination signal for VOD jobs and for
	// live jobs whose stream has ended.
	EndList bool
}

// Len reports the number of segments in the playlist. Convenience
// for callers that want the count without range-loop noise.
func (p *MediaPlaylist) Len() int { return len(p.Segments) }

// MaxMediaSeq returns the highest MediaSeq in the playlist, or
// MediaSequenceBase-1 when empty. Used by the poll loop to
// advance its "highest seen" cursor without re-walking Segments.
func (p *MediaPlaylist) MaxMediaSeq() int64 {
	if len(p.Segments) == 0 {
		return p.MediaSequenceBase - 1
	}
	return p.Segments[len(p.Segments)-1].MediaSeq
}
