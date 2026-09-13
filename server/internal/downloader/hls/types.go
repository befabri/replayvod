// Package hls parses media playlists and acquires TS or fMP4 fragments from CDN
// URLs; callers provide signed URLs and manage Twitch authentication separately.
package hls

import "time"

// SegmentKind identifies MPEG-TS (ts) or CMAF fragments with initialization (fmp4).
type SegmentKind string

const (
	SegmentKindTS   SegmentKind = "ts"
	SegmentKindFMP4 SegmentKind = "fmp4"
)

// SegmentOutcome classifies exact observations for durable recording accounting.
type SegmentOutcome string

const (
	// OutcomeCommitted identifies a durably published segment.
	OutcomeCommitted SegmentOutcome = "committed"

	// OutcomeGapAccepted identifies content loss permitted by gap policy.
	OutcomeGapAccepted SegmentOutcome = "gap_accepted"

	// OutcomeAdSkipped identifies an advertisement excluded from content-loss policy.
	OutcomeAdSkipped SegmentOutcome = "ad_skipped"

	// OutcomeAuth identifies a sequence whose authorization failure requires caller handling.
	OutcomeAuth SegmentOutcome = "auth"

	// OutcomeRefetchExpired resolves a requested retry that the playlist can no longer serve.
	OutcomeRefetchExpired SegmentOutcome = "refetch_expired"

	// OutcomeMalformedSkip identifies permitted loss from nonpositive EXTINF metadata.
	OutcomeMalformedSkip SegmentOutcome = "malformed_skip"
)

// SegmentEvent is a synchronous observation from Run, including accepted bootstrap loss.
// OnFirstPoll precedes these events; callbacks are sequential and must finish quickly.
type SegmentEvent struct {
	MediaSeq int64

	Outcome SegmentOutcome

	// BytesWritten counts published file bytes and is zero unless Outcome is OutcomeCommitted.
	BytesWritten int64

	// DurationSeconds is EXTINF duration for OutcomeCommitted and zero otherwise.
	DurationSeconds float64

	// Err carries the cause of authorization failure or accepted content loss.
	Err error
}

// SkipReason distinguishes policy-exempt advertisements from content lost before fetching.
type SkipReason string

const (
	// SkipReasonStitchedAd identifies Twitch advertisement metadata excluded from gap policy.
	SkipReasonStitchedAd SkipReason = "stitched-ad"

	// SkipReasonMalformed identifies nonpositive EXTINF, which silently shortens output
	// without creating a duration mismatch detectable after remuxing.
	SkipReasonMalformed SkipReason = "malformed"

	// SkipReasonWindowRolled covers an inclusive lost range [MediaSeq, EndMediaSeq]
	// after the initial playlist; it can recur during capture.
	SkipReasonWindowRolled SkipReason = "window-rolled"

	// SkipReasonRefetchExpired identifies an unresolved retry that can no longer arrive.
	SkipReasonRefetchExpired SkipReason = "refetch-expired"

	// SkipReasonResolved advances the cursor over durable outcomes without repeating accounting.
	SkipReasonResolved SkipReason = "resolved"
)

// SkipEvent reports a filtered sequence or lost range before acquisition.
type SkipEvent struct {
	MediaSeq int64
	// EndMediaSeq closes the lost range for SkipReasonWindowRolled.
	// Zero means this event covers only MediaSeq.
	EndMediaSeq int64
	Reason      SkipReason
}

// Segment represents one fragment with an absolute media sequence and EXTINF metadata.
type Segment struct {
	// MediaSeq is the absolute EXT-X-MEDIA-SEQUENCE position, stable across playlist refreshes.
	MediaSeq int64

	// URI preserves playlist spelling until the caller resolves relative references.
	URI string

	// Duration is EXTINF in seconds.
	Duration float64

	// Discontinuity records the preceding EXT-X-DISCONTINUITY tag.
	Discontinuity bool

	// IsAd identifies membership in a Twitch stitched-ad DateRange by program date-time.
	// Muted-DMCA segments remain content and must not be classified as advertisements.
	IsAd bool
}

// InitSegment is the EXT-X-MAP initialization required by fMP4 playlists.
type InitSegment struct {
	URI string
}

// MediaPlaylist is one parsed snapshot; live playlists usually expose a sliding window.
type MediaPlaylist struct {
	// Kind follows EXT-X-MAP presence, since URI suffixes do not reliably identify the container.
	Kind SegmentKind

	// Init is required for fMP4 and nil for TS.
	Init *InitSegment

	// TargetDuration is EXT-X-TARGETDURATION in whole seconds.
	TargetDuration time.Duration

	// MediaSequenceBase is EXT-X-MEDIA-SEQUENCE, including for empty playlists; zero is valid.
	MediaSequenceBase int64

	// Segments remains in playlist order across the sliding live window.
	Segments []Segment

	// EndList records EXT-X-ENDLIST, meaning no further segments can be appended.
	EndList bool
}

// Len returns the number of segments in this snapshot.
func (p *MediaPlaylist) Len() int { return len(p.Segments) }

// MaxMediaSeq returns the highest sequence, or MediaSequenceBase-1 when empty.
func (p *MediaPlaylist) MaxMediaSeq() int64 {
	if len(p.Segments) == 0 {
		return p.MediaSequenceBase - 1
	}
	return p.Segments[len(p.Segments)-1].MediaSeq
}
