package twitch

import (
	"errors"
	"testing"
)

// makeManifest builds a manifest shaped like the synthetic HEVC
// fixture: both HEVC and H.264 at 1080, H.264-only everywhere else,
// plus audio_only. Useful for exercising the codec-preference and
// fallback-chain paths without re-parsing fixtures per test.
func makeManifest() *Manifest {
	return &Manifest{
		Variants: []Variant{
			{URL: "src-hevc", Quality: "1440", Codec: CodecH265, GroupID: "chunked"},
			{URL: "1080-h264", Quality: "1080", Codec: CodecH264, GroupID: "1080p60"},
			{URL: "1080-hevc", Quality: "1080", Codec: CodecH265, GroupID: "1080p60-h265"},
			{URL: "720-h264", Quality: "720", Codec: CodecH264, GroupID: "720p60"},
			{URL: "audio", Quality: "audio_only", Codec: CodecAAC, GroupID: "audio_only"},
		},
	}
}

func TestSelectVariant_VideoPrefersHEVCAt1080(t *testing.T) {
	got, err := SelectVariant(makeManifest(), SelectOptions{
		RecordingType: RecordingTypeVideo,
		Quality:       "1080",
	})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got.URL != "1080-hevc" {
		t.Errorf("got URL %q, want 1080-hevc (HEVC preferred at equal quality)", got.URL)
	}
	if got.Codec != CodecH265 {
		t.Errorf("codec=%s, want %s", got.Codec, CodecH265)
	}
}

func TestSelectVariant_ForceH264FallsBackToH264(t *testing.T) {
	got, err := SelectVariant(makeManifest(), SelectOptions{
		RecordingType: RecordingTypeVideo,
		Quality:       "1080",
		ForceH264:     true,
	})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got.Codec != CodecH264 {
		t.Errorf("codec=%s, want %s with ForceH264=true", got.Codec, CodecH264)
	}
	if got.URL != "1080-h264" {
		t.Errorf("URL=%q, want 1080-h264", got.URL)
	}
}

func TestSelectVariant_DisableHEVC(t *testing.T) {
	got, err := SelectVariant(makeManifest(), SelectOptions{
		RecordingType: RecordingTypeVideo,
		Quality:       "1080",
		DisableHEVC:   true,
	})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got.Codec != CodecH264 {
		t.Errorf("codec=%s, want %s with DisableHEVC=true", got.Codec, CodecH264)
	}
}

func TestSelectVariant_QualityFallback(t *testing.T) {
	// Request 480; manifest has 720 + 1080 + audio only. The
	// fallback chain for 480 is [480, 360, 160] — none match —
	// so this should fail cleanly. NOT fall up to 720.
	got, err := SelectVariant(makeManifest(), SelectOptions{
		RecordingType: RecordingTypeVideo,
		Quality:       "480",
	})
	if !errors.Is(err, ErrNoAcceptableVariant) {
		t.Errorf("err=%v, want ErrNoAcceptableVariant", err)
	}
	if got != (SelectedVariant{}) {
		t.Errorf("got %+v, want zero", got)
	}
}

func TestSelectVariant_QualityFallsDown(t *testing.T) {
	// Request 1080 against a manifest that only has 360. Should
	// walk the chain 1080 → 720 → 480 → 360 and pick 360.
	m := &Manifest{
		Variants: []Variant{
			{URL: "360-only", Quality: "360", Codec: CodecH264, GroupID: "360p30"},
			{URL: "audio", Quality: "audio_only", Codec: CodecAAC, GroupID: "audio_only"},
		},
	}
	got, err := SelectVariant(m, SelectOptions{
		RecordingType: RecordingTypeVideo,
		Quality:       "1080",
	})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got.Quality != "360" {
		t.Errorf("quality=%s, want 360 (fallback)", got.Quality)
	}
}

func TestSelectVariant_AudioOnly(t *testing.T) {
	got, err := SelectVariant(makeManifest(), SelectOptions{
		RecordingType: RecordingTypeAudio,
	})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got.Codec != CodecAAC {
		t.Errorf("codec=%s, want %s", got.Codec, CodecAAC)
	}
	if got.Quality != "audio_only" {
		t.Errorf("quality=%s, want audio_only", got.Quality)
	}
	if got.URL != "audio" {
		t.Errorf("URL=%q, want audio", got.URL)
	}
}

func TestSelectVariant_AudioMissing(t *testing.T) {
	m := &Manifest{
		Variants: []Variant{
			{URL: "h264-only", Quality: "720", Codec: CodecH264, GroupID: "720p60"},
		},
	}
	_, err := SelectVariant(m, SelectOptions{RecordingType: RecordingTypeAudio})
	if !errors.Is(err, ErrNoAudioRendition) {
		t.Errorf("err=%v, want ErrNoAudioRendition", err)
	}
}

// TestSelectVariant_PrefersHEVCOverAV1AtEqualQuality pins the spec's
// codec preference order: HEVC is the mature supported codec; AV1 is
// optional behind a flag but does NOT outrank HEVC. Regression for
// the earlier codecRank that had AV1=3 > HEVC=2.
func TestSelectVariant_PrefersHEVCOverAV1AtEqualQuality(t *testing.T) {
	m := &Manifest{
		Variants: []Variant{
			{URL: "av1-1080", Quality: "1080", Codec: CodecAV1, GroupID: "1080p60-av1"},
			{URL: "hevc-1080", Quality: "1080", Codec: CodecH265, GroupID: "1080p60-h265"},
			{URL: "h264-1080", Quality: "1080", Codec: CodecH264, GroupID: "1080p60"},
		},
	}
	got, err := SelectVariant(m, SelectOptions{
		RecordingType: RecordingTypeVideo,
		Quality:       "1080",
		EnableAV1:     true,
	})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got.Codec != CodecH265 {
		t.Errorf("codec=%s, want %s (HEVC must outrank AV1 even with EnableAV1=true)",
			got.Codec, CodecH265)
	}
}

func TestSelectVariant_AV1GatedByFlag(t *testing.T) {
	m := &Manifest{
		Variants: []Variant{
			{URL: "av1-1080", Quality: "1080", Codec: CodecAV1, GroupID: "1080p60-av1"},
			{URL: "h264-1080", Quality: "1080", Codec: CodecH264, GroupID: "1080p60"},
		},
	}

	// AV1 off → picks H.264.
	got, err := SelectVariant(m, SelectOptions{
		RecordingType: RecordingTypeVideo,
		Quality:       "1080",
	})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got.Codec != CodecH264 {
		t.Errorf("AV1-off: codec=%s, want %s", got.Codec, CodecH264)
	}

	// AV1 on → picks AV1 (highest rank).
	got, err = SelectVariant(m, SelectOptions{
		RecordingType: RecordingTypeVideo,
		Quality:       "1080",
		EnableAV1:     true,
	})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got.Codec != CodecAV1 {
		t.Errorf("AV1-on: codec=%s, want %s", got.Codec, CodecAV1)
	}

	// ForceH264 wins even with EnableAV1.
	got, err = SelectVariant(m, SelectOptions{
		RecordingType: RecordingTypeVideo,
		Quality:       "1080",
		EnableAV1:     true,
		ForceH264:     true,
	})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got.Codec != CodecH264 {
		t.Errorf("ForceH264+EnableAV1: codec=%s, want %s", got.Codec, CodecH264)
	}
}

func TestSelectVariant_NilOrEmptyManifest(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    *Manifest
	}{
		{name: "nil manifest", m: nil},
		{name: "no variants", m: &Manifest{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := SelectVariant(tc.m, SelectOptions{RecordingType: RecordingTypeVideo, Quality: "1080"})
			if !errors.Is(err, ErrNoAcceptableVariant) {
				t.Fatalf("err=%v, want ErrNoAcceptableVariant", err)
			}
		})
	}
}

// TestSelectVariant_FPS pins the FPS pointer rule: a positive frame rate is
// reported by pointer, while a zero/absent one stays nil rather than a pointer
// to 0. The boundary matters because the field distinguishes "unknown" (nil)
// from a real value.
func TestSelectVariant_FPS(t *testing.T) {
	pick := func(fps float64) *float64 {
		m := &Manifest{Variants: []Variant{{URL: "v", Quality: "1080", Codec: CodecH264, GroupID: "1080p", FPS: fps}}}
		got, err := SelectVariant(m, SelectOptions{RecordingType: RecordingTypeVideo, Quality: "1080"})
		if err != nil {
			t.Fatalf("select(fps=%v): %v", fps, err)
		}
		return got.FPS
	}
	if got := pick(0); got != nil {
		t.Errorf("FPS=0 reported as %v, want nil", *got)
	}
	if got := pick(0.5); got == nil || *got != 0.5 {
		t.Errorf("FPS=0.5 reported as %v, want pointer to 0.5", got)
	}
	if got := pick(60); got == nil || *got != 60 {
		t.Errorf("FPS=60 reported as %v, want pointer to 60", got)
	}
}

// TestSelectVariant_FirstWinsAtEqualCodecAndQuality pins that, among variants
// tied on both quality and codec, the first in manifest order is kept, so the
// pick is stable rather than order-of-last.
func TestSelectVariant_FirstWinsAtEqualCodecAndQuality(t *testing.T) {
	m := &Manifest{Variants: []Variant{
		{URL: "first", Quality: "1080", Codec: CodecH264, GroupID: "1080p60"},
		{URL: "second", Quality: "1080", Codec: CodecH264, GroupID: "1080p60-alt"},
	}}
	got, err := SelectVariant(m, SelectOptions{RecordingType: RecordingTypeVideo, Quality: "1080"})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got.URL != "first" {
		t.Errorf("URL=%q, want first (manifest order wins on a tie)", got.URL)
	}
}

func TestCodecRank(t *testing.T) {
	cases := map[string]int{CodecH265: 3, CodecAV1: 2, CodecH264: 1, "vp9": -1, "": -1}
	for codec, want := range cases {
		if got := codecRank(codec); got != want {
			t.Errorf("codecRank(%q) = %d, want %d", codec, got, want)
		}
	}
}

func TestCodecAllowed(t *testing.T) {
	cases := []struct {
		name  string
		codec string
		opts  SelectOptions
		want  bool
	}{
		{name: "h264 always allowed", codec: CodecH264, opts: SelectOptions{}, want: true},
		{name: "h264 allowed even when forced", codec: CodecH264, opts: SelectOptions{ForceH264: true}, want: true},
		{name: "hevc allowed by default", codec: CodecH265, opts: SelectOptions{}, want: true},
		{name: "hevc dropped when disabled", codec: CodecH265, opts: SelectOptions{DisableHEVC: true}, want: false},
		{name: "hevc dropped when forced h264", codec: CodecH265, opts: SelectOptions{ForceH264: true}, want: false},
		{name: "av1 gated off by default", codec: CodecAV1, opts: SelectOptions{}, want: false},
		{name: "av1 allowed when enabled", codec: CodecAV1, opts: SelectOptions{EnableAV1: true}, want: true},
		{name: "av1 dropped when forced h264", codec: CodecAV1, opts: SelectOptions{EnableAV1: true, ForceH264: true}, want: false},
		{name: "unknown codec dropped", codec: "vp9", opts: SelectOptions{}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := codecAllowed(tc.codec, tc.opts); got != tc.want {
				t.Errorf("codecAllowed(%q, %+v) = %v, want %v", tc.codec, tc.opts, got, tc.want)
			}
		})
	}
}

func TestSelectVariant_EmptyQualityDefaults1080(t *testing.T) {
	got, err := SelectVariant(makeManifest(), SelectOptions{
		RecordingType: RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got.Quality != "1080" {
		t.Errorf("quality=%s, want 1080 (empty request defaults to 1080)", got.Quality)
	}
}
