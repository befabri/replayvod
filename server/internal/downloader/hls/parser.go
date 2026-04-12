package hls

import (
	"fmt"
	"io"
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
		})
	}
	return out, nil
}
