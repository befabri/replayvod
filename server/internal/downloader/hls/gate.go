package hls

import (
	"errors"
	"fmt"
	"strings"

	m3u8 "github.com/Eyevinn/hls-m3u8/m3u8"
)

// ErrUnsupportedManifest is the base sentinel for every capability-
// gate rejection. Callers that want to distinguish cases use
// errors.As on the concrete *UnsupportedManifestError (or .Reason
// via UnsupportedReason accessor) rather than string-matching the
// message.
var ErrUnsupportedManifest = errors.New("hls: unsupported manifest feature")

// UnsupportedReason names the specific capability the gate
// rejected. Kept as an enum so the orchestrator can surface a
// readable reason on the job row instead of stringifying an error
// message.
type UnsupportedReason string

const (
	// ReasonByteRange fires when any media segment carries
	// EXT-X-BYTERANGE. Spec Stage 4: we never support byte-
	// range fetches. If Twitch's HEVC/AV1 path ever starts
	// emitting these, treat it as a blocker — don't paper over.
	ReasonByteRange UnsupportedReason = "byterange"

	// ReasonEncrypted fires on EXT-X-KEY with a non-NONE
	// METHOD. Covers AES-128 (which the spec explicitly
	// excludes from v1) and the DRM keyformats that share the
	// same tag. ReasonDRM is a narrower subcase but still
	// surfaces as ReasonEncrypted unless the key format is
	// recognized as a DRM provider.
	ReasonEncrypted UnsupportedReason = "encrypted"

	// ReasonDRM fires when the EXT-X-KEY carries a known DRM
	// scheme (FairPlay, PlayReady, Widevine via FAXS-CM). A
	// new-key refresh doesn't help; these are permanent
	// rejections for the stream.
	ReasonDRM UnsupportedReason = "drm"

	// ReasonLowLatency fires on EXT-X-PART or
	// EXT-X-PRELOAD-HINT. LL-HLS is out of scope for v1.
	ReasonLowLatency UnsupportedReason = "low_latency"

	// ReasonMalformed catches internally inconsistent
	// playlists — an fMP4 that says Kind=TS, a playlist with
	// zero segments and no ENDLIST, etc. Not a Twitch-side
	// failure; usually means our parser saw a shape it didn't
	// know how to normalize.
	ReasonMalformed UnsupportedReason = "malformed"
)

// UnsupportedManifestError is the rich-typed gate rejection.
// Implements errors.Is(ErrUnsupportedManifest) so callers can
// branch on the sentinel without caring about the reason.
type UnsupportedManifestError struct {
	Reason UnsupportedReason
	Detail string
}

func (e *UnsupportedManifestError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("hls: unsupported manifest: %s", e.Reason)
	}
	return fmt.Sprintf("hls: unsupported manifest: %s: %s", e.Reason, e.Detail)
}

// Is reports target == ErrUnsupportedManifest so errors.Is works
// on the sentinel. The Reason-level distinction is available via
// errors.As with *UnsupportedManifestError.
func (e *UnsupportedManifestError) Is(target error) bool {
	return target == ErrUnsupportedManifest
}

// drmKeyformats recognizes the three DRM providers most commonly
// embedded in HLS manifests. FairPlay ships as skd:// URIs or the
// apple keyformat; PlayReady uses the microsoft keyformat;
// Widevine shows up as FAXS-CM which Eyevinn doesn't model
// directly but which we'd see as a KEYFORMAT if ever present.
//
// Check against the lowercase form so a manifest that uppercases
// the value doesn't sneak past.
var drmKeyformats = map[string]struct{}{
	"com.apple.streamingkeydelivery": {},
	"com.microsoft.playready":        {},
	"urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed": {}, // Widevine system ID
}

// checkCapability runs the capability gate against a decoded
// Eyevinn MediaPlaylist. It returns the Kind the parser should
// assign when successful (ts or fmp4) and a non-nil error wrapping
// UnsupportedManifestError when any rejection criteria fires.
//
// The gate is deliberately per-playlist, not per-segment: we
// check once after decode, before any segment work begins. That
// means we pay the full playlist scan once per poll, which is
// cheap next to the network I/O.
func checkCapability(p *m3u8.MediaPlaylist) (SegmentKind, error) {
	if p == nil {
		return "", &UnsupportedManifestError{Reason: ReasonMalformed, Detail: "nil playlist"}
	}

	// LL-HLS rejection runs first because it's the cheapest
	// scan (two fields) and a positive match short-circuits
	// the rest.
	if len(p.PartialSegments) > 0 {
		return "", &UnsupportedManifestError{Reason: ReasonLowLatency, Detail: "EXT-X-PART"}
	}
	if p.PreloadHints != nil {
		return "", &UnsupportedManifestError{Reason: ReasonLowLatency, Detail: "EXT-X-PRELOAD-HINT"}
	}

	// Encryption / DRM: examine every key both at playlist
	// level and on individual segments. Some manifests only
	// declare the key mid-stream (EXT-X-KEY between segments),
	// which Eyevinn attaches to the segments that follow.
	for i := range p.Keys {
		if err := checkKey(&p.Keys[i]); err != nil {
			return "", err
		}
	}
	for _, seg := range p.Segments {
		if seg == nil {
			continue
		}
		for i := range seg.Keys {
			if err := checkKey(&seg.Keys[i]); err != nil {
				return "", err
			}
		}
	}

	// Byterange: reject any segment with Limit > 0 (Eyevinn
	// uses Limit=0 to mean "no byterange"). Offset alone
	// (without Limit) isn't meaningful in HLS syntax — EXTINF
	// byterange always carries a length — so we check Limit
	// as the primary signal.
	for _, seg := range p.Segments {
		if seg == nil {
			continue
		}
		if seg.Limit > 0 {
			return "", &UnsupportedManifestError{
				Reason: ReasonByteRange,
				Detail: fmt.Sprintf("segment seq=%d carries EXT-X-BYTERANGE", seg.SeqId),
			}
		}
	}

	// Determine kind from the presence of EXT-X-MAP. fMP4 is
	// identified by the map, not by URI suffix — the spec's
	// empirical altair capture has .mp4 URIs in the master but
	// .m4s-like behavior at the segment level.
	kind := SegmentKindTS
	if p.Map != nil && p.Map.URI != "" {
		kind = SegmentKindFMP4
	}
	return kind, nil
}

// checkKey rejects any EXT-X-KEY that would require decryption
// support we don't have. NONE and empty method values pass through
// — HLS uses NONE to explicitly clear a previous key, and an
// unpopulated Method is Eyevinn's zero value for "no key tag".
func checkKey(k *m3u8.Key) error {
	if k == nil {
		return nil
	}
	method := strings.ToUpper(strings.TrimSpace(k.Method))
	if method == "" || method == "NONE" {
		return nil
	}

	// DRM first — the classifier cares about the reason so
	// operator logs can distinguish "we could add AES-128
	// support if we wanted to" from "this stream will never
	// be downloadable."
	keyformat := strings.ToLower(strings.TrimSpace(k.Keyformat))
	if _, ok := drmKeyformats[keyformat]; ok {
		return &UnsupportedManifestError{
			Reason: ReasonDRM,
			Detail: fmt.Sprintf("KEYFORMAT=%q", k.Keyformat),
		}
	}
	// skd:// URIs are FairPlay even when KEYFORMAT is omitted.
	if strings.HasPrefix(strings.ToLower(k.URI), "skd://") {
		return &UnsupportedManifestError{
			Reason: ReasonDRM,
			Detail: "FairPlay skd:// URI",
		}
	}

	// Anything else (AES-128, SAMPLE-AES, an unknown method)
	// is encrypted but not DRM. Still rejected in v1.
	return &UnsupportedManifestError{
		Reason: ReasonEncrypted,
		Detail: fmt.Sprintf("METHOD=%s", method),
	}
}
