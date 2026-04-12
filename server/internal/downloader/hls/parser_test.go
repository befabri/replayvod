package hls

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func openFixture(t *testing.T, name string) *os.File {
	t.Helper()
	p := filepath.Join("testdata", name)
	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("open fixture %s: %v", p, err)
	}
	return f
}

func TestParseMediaPlaylist_TSLive(t *testing.T) {
	f := openFixture(t, "media-ts-live.m3u8")
	defer f.Close()

	pl, err := ParseMediaPlaylist(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if pl.Kind != SegmentKindTS {
		t.Errorf("Kind=%s, want ts", pl.Kind)
	}
	if pl.Init != nil {
		t.Errorf("Init=%+v, want nil for TS playlist", pl.Init)
	}
	if pl.TargetDuration != 2*time.Second {
		t.Errorf("TargetDuration=%v, want 2s", pl.TargetDuration)
	}
	if pl.MediaSequenceBase != 42 {
		t.Errorf("MediaSequenceBase=%d, want 42", pl.MediaSequenceBase)
	}
	if pl.EndList {
		t.Errorf("EndList=true, want false for live playlist")
	}
	if pl.Len() != 4 {
		t.Fatalf("len=%d, want 4", pl.Len())
	}
	// Sequential MediaSeq starting at the base.
	for i, want := range []int64{42, 43, 44, 45} {
		if pl.Segments[i].MediaSeq != want {
			t.Errorf("Segments[%d].MediaSeq=%d, want %d", i, pl.Segments[i].MediaSeq, want)
		}
	}
	// Discontinuity is flagged on the segment that follows the tag.
	if !pl.Segments[3].Discontinuity {
		t.Error("Segments[3].Discontinuity=false, want true (after EXT-X-DISCONTINUITY)")
	}
	if pl.Segments[2].Discontinuity {
		t.Error("Segments[2].Discontinuity=true, want false")
	}
	if pl.MaxMediaSeq() != 45 {
		t.Errorf("MaxMediaSeq=%d, want 45", pl.MaxMediaSeq())
	}
}

func TestParseMediaPlaylist_FMP4Live(t *testing.T) {
	f := openFixture(t, "media-fmp4-live.m3u8")
	defer f.Close()

	pl, err := ParseMediaPlaylist(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if pl.Kind != SegmentKindFMP4 {
		t.Errorf("Kind=%s, want fmp4", pl.Kind)
	}
	if pl.Init == nil {
		t.Fatal("Init=nil, want non-nil for fMP4 playlist")
	}
	if pl.Init.URI != "https://edge.example.com/init.mp4" {
		t.Errorf("Init.URI=%q", pl.Init.URI)
	}
	if pl.TargetDuration != 6*time.Second {
		t.Errorf("TargetDuration=%v, want 6s", pl.TargetDuration)
	}
	if pl.MediaSequenceBase != 100 {
		t.Errorf("MediaSequenceBase=%d, want 100", pl.MediaSequenceBase)
	}
	if pl.Len() != 3 {
		t.Errorf("len=%d, want 3", pl.Len())
	}
}

func TestParseMediaPlaylist_TSVodWithEndList(t *testing.T) {
	f := openFixture(t, "media-ts-vod.m3u8")
	defer f.Close()

	pl, err := ParseMediaPlaylist(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !pl.EndList {
		t.Error("EndList=false, want true for VOD with EXT-X-ENDLIST")
	}
	if pl.Len() != 3 {
		t.Errorf("len=%d, want 3", pl.Len())
	}
}

func TestParseMediaPlaylist_RejectByterange(t *testing.T) {
	f := openFixture(t, "reject-byterange.m3u8")
	defer f.Close()

	_, err := ParseMediaPlaylist(f)
	if !errors.Is(err, ErrUnsupportedManifest) {
		t.Fatalf("err=%v, want ErrUnsupportedManifest", err)
	}
	var ue *UnsupportedManifestError
	if !errors.As(err, &ue) {
		t.Fatalf("err=%T, want *UnsupportedManifestError", err)
	}
	if ue.Reason != ReasonByteRange {
		t.Errorf("Reason=%s, want %s", ue.Reason, ReasonByteRange)
	}
}

func TestParseMediaPlaylist_RejectAES128(t *testing.T) {
	f := openFixture(t, "reject-aes128.m3u8")
	defer f.Close()

	_, err := ParseMediaPlaylist(f)
	var ue *UnsupportedManifestError
	if !errors.As(err, &ue) {
		t.Fatalf("err=%v, want *UnsupportedManifestError", err)
	}
	// AES-128 has no drmKeyformat → classified ReasonEncrypted,
	// not ReasonDRM. Important distinction for operator logs.
	if ue.Reason != ReasonEncrypted {
		t.Errorf("Reason=%s, want %s (AES-128 is encrypted but not DRM)", ue.Reason, ReasonEncrypted)
	}
}

func TestParseMediaPlaylist_RejectFairPlay(t *testing.T) {
	f := openFixture(t, "reject-fairplay.m3u8")
	defer f.Close()

	_, err := ParseMediaPlaylist(f)
	var ue *UnsupportedManifestError
	if !errors.As(err, &ue) {
		t.Fatalf("err=%v, want *UnsupportedManifestError", err)
	}
	if ue.Reason != ReasonDRM {
		t.Errorf("Reason=%s, want %s", ue.Reason, ReasonDRM)
	}
}

func TestParseMediaPlaylist_RejectPlayReady(t *testing.T) {
	f := openFixture(t, "reject-playready.m3u8")
	defer f.Close()

	_, err := ParseMediaPlaylist(f)
	var ue *UnsupportedManifestError
	if !errors.As(err, &ue) {
		t.Fatalf("err=%v, want *UnsupportedManifestError", err)
	}
	if ue.Reason != ReasonDRM {
		t.Errorf("Reason=%s, want %s", ue.Reason, ReasonDRM)
	}
}

func TestParseMediaPlaylist_RejectLowLatency(t *testing.T) {
	f := openFixture(t, "reject-llhls.m3u8")
	defer f.Close()

	_, err := ParseMediaPlaylist(f)
	var ue *UnsupportedManifestError
	if !errors.As(err, &ue) {
		t.Fatalf("err=%v, want *UnsupportedManifestError", err)
	}
	if ue.Reason != ReasonLowLatency {
		t.Errorf("Reason=%s, want %s", ue.Reason, ReasonLowLatency)
	}
}

func TestMaxMediaSeq_EmptyPlaylist(t *testing.T) {
	pl := &MediaPlaylist{MediaSequenceBase: 50}
	if got := pl.MaxMediaSeq(); got != 49 {
		t.Errorf("MaxMediaSeq(empty, base=50)=%d, want 49", got)
	}
}
