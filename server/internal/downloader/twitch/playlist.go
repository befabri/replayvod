package twitch

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	m3u8 "github.com/Eyevinn/hls-m3u8/m3u8"
)

// FetchMasterPlaylist hits usher.ttvnw.net with the full query-
// parameter set streamlink + yt-dlp settled on. Returns the parsed
// manifest. All codec/quality filtering happens in SelectVariant;
// this function is I/O only.
//
// Takes SelectOptions rather than a fetch-specific struct so the
// same per-job options drive both the `supported_codecs` query
// parameter here and the Stage 3 codec filter. Keeping them in
// one place prevents the drift where a caller opts AV1 into the
// fetch but not the select (or vice versa) and then can't explain
// why selection picks the "wrong" codec. Only EnableAV1 is read
// here; the rest of SelectOptions is ignored by the fetch.
//
// 4xx responses are wrapped in AuthError so the caller can run
// them through classifyAuthError — a 403 on usher usually means
// the playback token expired or the stream is geo/sub-restricted.
func (c *Client) FetchMasterPlaylist(ctx context.Context, login string, token PlaybackToken, opts SelectOptions) (*Manifest, error) {
	if login == "" {
		return nil, fmt.Errorf("twitch: empty login")
	}
	if token.Empty() {
		return nil, fmt.Errorf("twitch: empty playback token")
	}

	supported := "h265,h264"
	if opts.EnableAV1 {
		supported = "av1,h265,h264"
	}

	q := url.Values{}
	q.Set("platform", "web")
	q.Set("p", strconv.Itoa(randomCacheBuster()))
	q.Set("allow_source", "true")
	q.Set("allow_audio_only", "true")
	q.Set("playlist_include_framerate", "true")
	q.Set("supported_codecs", supported)
	q.Set("sig", token.Signature)
	q.Set("token", token.Value)

	u := fmt.Sprintf("%s/api/channel/hls/%s.m3u8?%s", c.usherBaseURL, strings.ToLower(login), q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build usher request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Origin", playerOrigin)
	req.Header.Set("Referer", playerReferer)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("usher request: %w", err)
	}
	defer drainAndClose(resp)

	if resp.StatusCode != http.StatusOK {
		// Usher 4xx bodies are JSON arrays like
		// [{"error_code":"unauthorized_entitlements",...}];
		// NewAuthError handles both array and object shapes.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return nil, NewAuthError(resp.StatusCode, body)
	}

	manifest, err := c.parseMasterPlaylist(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse master playlist: %w", err)
	}
	return manifest, nil
}

// randomCacheBuster returns a 0..999999 integer for the usher `p`
// parameter. Twitch uses this to prevent CDN-cached responses.
// crypto/rand for consistency with the rest of the package; math/
// rand would work fine but requires a global seed and is one more
// source of non-determinism in tests.
func randomCacheBuster() int {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return int(binary.BigEndian.Uint64(b[:]) % 1_000_000)
}

// parseMasterPlaylist decodes an M3U8 master playlist into our
// domain Manifest type. We thin out Eyevinn's rich Variant/VariantParams
// to the four fields Stage 3 actually consults (URL, Quality, FPS,
// Codec) + the GroupID so audio_only is identifiable.
//
// The Eyevinn parser handles the bulk of the work — EXT-X-STREAM-INF
// attribute parsing, EXT-X-MEDIA alternative attachment, EXT-X-INDEPENDENT-
// SEGMENTS, etc. What we do on top:
//   - normalize Quality from RESOLUTION ("1920x1080" → "1080") or
//     from the GROUP-ID for audio_only
//   - normalize Codec from the CODECS attribute (first video codec
//     wins; avc1 → h264, hvc1/hev1 → h265, av01 → av1, mp4a-only →
//     aac)
//   - carry Group-ID through so the selector can find audio_only.
//
// log may be nil; when provided, variants dropped for unparseable
// shape are surfaced at Debug so an unfamiliar manifest surfaces in
// logs instead of as "why did selection pick 720 instead of 1080".
func (c *Client) parseMasterPlaylist(r io.Reader) (*Manifest, error) {
	master := m3u8.NewMasterPlaylist()
	if err := master.DecodeFrom(r, false); err != nil {
		return nil, err
	}
	out := &Manifest{}
	for _, v := range master.Variants {
		if v == nil {
			continue
		}
		variant := Variant{
			URL:     v.URI,
			FPS:     v.FrameRate,
			GroupID: v.Video,
			Codec:   primaryCodec(v.Codecs),
		}
		variant.Quality = normalizeQuality(v.Resolution, v.Video)
		if variant.Quality == "" || variant.Codec == "" {
			// Parser keeps the variant; Stage 3 (SelectVariant)
			// is what actually filters it out. Log here so an
			// unfamiliar manifest shape is visible in
			// operator logs rather than only in the "why did
			// selection pick 720 instead of 1080" report.
			c.log.Debug("unusable variant in master playlist",
				"reason", "unrecognized quality or codec",
				"resolution", v.Resolution,
				"group_id", v.Video,
				"codecs", v.Codecs)
		}
		out.Variants = append(out.Variants, variant)
	}
	return out, nil
}

// parseMasterPlaylist (package function) is kept for tests that
// don't want to construct a Client. Logs nothing. When the Stage 4
// integration lands, delete this in favor of the method.
func parseMasterPlaylist(r io.Reader) (*Manifest, error) {
	master := m3u8.NewMasterPlaylist()
	if err := master.DecodeFrom(r, false); err != nil {
		return nil, err
	}
	out := &Manifest{}
	for _, v := range master.Variants {
		if v == nil {
			continue
		}
		variant := Variant{
			URL:     v.URI,
			FPS:     v.FrameRate,
			GroupID: v.Video,
			Codec:   primaryCodec(v.Codecs),
		}
		variant.Quality = normalizeQuality(v.Resolution, v.Video)
		out.Variants = append(out.Variants, variant)
	}
	return out, nil
}

// resolutionPattern parses "1920x1080" and extracts the height.
var resolutionPattern = regexp.MustCompile(`^\d+x(\d+)$`)

// normalizeQuality maps a variant's RESOLUTION + VIDEO group to our
// canonical quality string. audio_only variants → "audio_only".
// Source variants where Twitch doesn't declare a RESOLUTION (rare
// but observed on very old ingest paths) fall back to the group ID.
func normalizeQuality(resolution, groupID string) string {
	if groupID == "audio_only" {
		return "audio_only"
	}
	if m := resolutionPattern.FindStringSubmatch(resolution); m != nil {
		return m[1]
	}
	// Fall back to a height embedded in the group ID ("1080p60" → "1080").
	for i, ch := range groupID {
		if ch == 'p' && i > 0 {
			return groupID[:i]
		}
	}
	return ""
}

// primaryCodec picks the *video* codec from a CODECS attribute like
// "avc1.4D401F,mp4a.40.2" and normalizes to our domain values. An
// audio-only entry like "mp4a.40.2" (no video codec) resolves to
// CodecAAC so the variant picker can identify audio-only.
func primaryCodec(codecs string) string {
	if codecs == "" {
		return ""
	}
	parts := strings.Split(codecs, ",")
	hadAudio := false
	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch {
		case strings.HasPrefix(p, "avc1."):
			return CodecH264
		case strings.HasPrefix(p, "hvc1."), strings.HasPrefix(p, "hev1."):
			return CodecH265
		case strings.HasPrefix(p, "av01."):
			return CodecAV1
		case strings.HasPrefix(p, "mp4a."):
			hadAudio = true
		}
	}
	if hadAudio {
		return CodecAAC
	}
	return ""
}
