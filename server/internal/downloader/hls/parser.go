package hls

import (
	"fmt"
	"io"
	"strings"
	"time"

	m3u8 "github.com/Eyevinn/hls-m3u8/m3u8"
)

// Capacity sized to a 16-hour VOD at 6s target duration. Live
// windows are always far smaller (~10 segments); oversizing costs
// a few KB of slice header. Eyevinn requires capacity >= winsize
// and we don't use the sliding window behavior.
const decodeCapacity = 10000

// maxPlaylistBytes is the upper bound applied to the reader before
// decoding. Real Twitch media playlists cap out at ~20 KB even
// for long VODs; 1 MiB is ~50× that and guards against a hostile
// or misconfigured origin streaming unbounded bytes into Eyevinn's
// buffered parser.
const maxPlaylistBytes = 1 << 20

// ParseMediaPlaylist decodes an M3U8 media playlist, runs the
// capability gate, and returns the domain MediaPlaylist. Returns
// *UnsupportedManifestError wrapped in ErrUnsupportedManifest if
// the playlist uses a feature we don't support; any other error is
// a parse-level failure (malformed M3U8, I/O error) worth logging
// and retrying at the transport layer.
//
// The input reader is bounded by maxPlaylistBytes to prevent a
// pathological server from feeding the parser an unbounded body.
//
// This function is pure — no network, no filesystem, no logger.
// The caller (Phase 4b+ segment fetch loop) handles everything
// stateful around it.
func ParseMediaPlaylist(r io.Reader) (*MediaPlaylist, error) {
	pl, err := m3u8.NewMediaPlaylist(0, decodeCapacity)
	if err != nil {
		return nil, fmt.Errorf("hls new playlist: %w", err)
	}
	if err := pl.DecodeFrom(io.LimitReader(r, maxPlaylistBytes), false); err != nil {
		return nil, fmt.Errorf("hls decode: %w", err)
	}
	kind, init, err := checkCapability(pl)
	if err != nil {
		return nil, err
	}

	out := &MediaPlaylist{
		Kind:              kind,
		Init:              init,
		TargetDuration:    time.Duration(pl.TargetDuration) * time.Second,
		MediaSequenceBase: int64(pl.SeqNo),
		EndList:           pl.Closed,
	}

	// Collect stitched-ad windows before walking segments so each
	// segment can consult the full list in one pass. An ad window
	// is a [StartDate, StartDate+Duration) interval; we match
	// segments whose ProgramDateTime falls inside any window.
	adWindows := collectStitchedAdWindows(pl.DateRanges)

	// Walk segments in playlist order. Eyevinn's MediaPlaylist
	// stores segments in a ring buffer; iterating pl.Segments
	// directly is the library's documented access pattern.
	// The SeqId on each segment is the absolute MediaSeq (base +
	// offset), not a local index, so we use it as-is.
	for _, seg := range pl.Segments {
		if seg == nil {
			continue
		}
		out.Segments = append(out.Segments, Segment{
			MediaSeq:      int64(seg.SeqId),
			URI:           seg.URI,
			Duration:      seg.Duration,
			Discontinuity: seg.Discontinuity,
			IsAd:          isAdSegment(seg.ProgramDateTime, adWindows),
		})
	}
	return out, nil
}

// adWindow is one stitched-ad DateRange pinned to the time axis.
// Half-open [Start, End) so a segment whose ProgramDateTime is
// exactly at End belongs to the post-ad content, not the ad.
type adWindow struct {
	Start time.Time
	End   time.Time
}

// collectStitchedAdWindows filters a playlist's DateRanges down to
// the ones Twitch uses to mark stitched ads. Two identifiers both
// work in practice — checking either is belt-and-suspenders
// against minor schema drift:
//
//   - CLASS="twitch-stitched-ad" (canonical, seen in every ad-pod
//     capture)
//   - ID prefix "stitched-ad-" (e.g. "stitched-ad-<timestamp>-<id>";
//     survives even if Twitch renames the class)
//
// Windows with zero or missing Duration are skipped — without a
// duration bound we can't attribute segments to the ad pod. Spec
// allows a fallback via EXT-X-DISCONTINUITY boundaries; not
// implemented yet because every observed capture carries duration.
func collectStitchedAdWindows(drs []*m3u8.DateRange) []adWindow {
	var out []adWindow
	for _, dr := range drs {
		if dr == nil {
			continue
		}
		if !isStitchedAdClass(dr) {
			continue
		}
		if dr.Duration == nil || *dr.Duration <= 0 {
			continue
		}
		dur := time.Duration(*dr.Duration * float64(time.Second))
		out = append(out, adWindow{
			Start: dr.StartDate,
			End:   dr.StartDate.Add(dur),
		})
	}
	return out
}

// isStitchedAdClass reports whether a DateRange marks a Twitch
// stitched ad pod. Both identifiers are treated as authoritative —
// either alone is enough.
func isStitchedAdClass(dr *m3u8.DateRange) bool {
	if dr.Class == "twitch-stitched-ad" {
		return true
	}
	if strings.HasPrefix(dr.ID, "stitched-ad-") {
		return true
	}
	return false
}

// isAdSegment reports whether a segment's ProgramDateTime falls
// within any ad window. Zero-valued ProgramDateTime (segment
// lacks EXT-X-PROGRAM-DATE-TIME) never matches — without a
// timestamp we can't attribute, and silently marking is-ad on
// timestamp-less segments would skip legitimate content.
func isAdSegment(pdt time.Time, windows []adWindow) bool {
	if pdt.IsZero() || len(windows) == 0 {
		return false
	}
	for _, w := range windows {
		// Half-open interval: segments exactly at w.End belong
		// to the post-ad content, not the ad.
		if !pdt.Before(w.Start) && pdt.Before(w.End) {
			return true
		}
	}
	return false
}
