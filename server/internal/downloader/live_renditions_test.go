package downloader

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/repository"
)

const liveRenditionsManifest = "#EXTM3U\n" +
	"#EXT-X-STREAM-INF:BANDWIDTH=160000,CODECS=\"mp4a.40.2\",VIDEO=\"audio_only\"\n%s/audio.m3u8\n" +
	"#EXT-X-STREAM-INF:BANDWIDTH=3000000,RESOLUTION=1280x720,FRAME-RATE=30,CODECS=\"avc1.4D401F,mp4a.40.2\"\n%s/720p30.m3u8\n" +
	"#EXT-X-STREAM-INF:BANDWIDTH=6000000,RESOLUTION=1920x1080,FRAME-RATE=60,CODECS=\"avc1.640028,mp4a.40.2\"\n%s/1080p60.m3u8\n" +
	"#EXT-X-STREAM-INF:BANDWIDTH=9000000,RESOLUTION=2560x1440,FRAME-RATE=60,CODECS=\"hev1.1.6.L150.90,mp4a.40.2\"\n%s/1440p60.m3u8\n" +
	"#EXT-X-STREAM-INF:BANDWIDTH=4000000,RESOLUTION=1280x720,FRAME-RATE=60,CODECS=\"avc1.4D401F,mp4a.40.2\"\n%s/720p60.m3u8\n"

func TestLiveRenditionsListsTheRecorderPoolTallestFirst(t *testing.T) {
	var base string
	var authorization []string
	edge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gql" {
			authorization = append(authorization, r.Header.Get("Authorization"))
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"streamPlaybackAccessToken": map[string]string{"value": "signed", "signature": "sig"}}})
			return
		}
		if r.URL.Query().Get("supported_codecs") != "h265,h264" {
			t.Errorf("supported_codecs = %q", r.URL.Query().Get("supported_codecs"))
		}
		_, _ = fmt.Fprintf(w, liveRenditionsManifest, base, base, base, base, base)
	}))
	defer edge.Close()
	base = edge.URL
	s := &Service{
		cfg:    &config.Config{},
		twitch: twitch.New(twitch.Config{GQLURL: base + "/gql", IntegrityURL: base + "/integrity", UsherBaseURL: base}, discardLog()),
	}

	got, err := s.LiveRenditions(t.Context(), "altair", false)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Anonymous {
		t.Fatal("no session connected, want anonymous")
	}
	want := []Rendition{
		{Height: 1440, FPS: 60, Codec: twitch.CodecH265},
		{Height: 1080, FPS: 60, Codec: twitch.CodecH264},
		{Height: 720, FPS: 60, Codec: twitch.CodecH264},
		{Height: 720, FPS: 30, Codec: twitch.CodecH264},
	}
	if len(got.Renditions) != len(want) {
		t.Fatalf("renditions = %+v, want %+v", got.Renditions, want)
	}
	for i := range want {
		if got.Renditions[i] != want[i] {
			t.Fatalf("rendition %d = %+v, want %+v", i, got.Renditions[i], want[i])
		}
	}

	s.SetPlaybackCredentials(&playbackCredentialStub{token: "private-website-session"})
	got, err = s.LiveRenditions(t.Context(), "altair", false)
	if err != nil || got.Anonymous {
		t.Fatalf("connected session: anonymous=%v err=%v", got.Anonymous, err)
	}
	if len(authorization) != 2 || authorization[0] != "" || authorization[1] != "OAuth private-website-session" {
		t.Fatalf("authorization = %v", authorization)
	}
}

func TestLiveRenditionsReportsAnOfflineChannel(t *testing.T) {
	edge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gql" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"streamPlaybackAccessToken": map[string]string{"value": "signed", "signature": "sig"}}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`[{"error_code":"transcode_does_not_exist","error":"transcode does not exist","type":"error"}]`))
	}))
	defer edge.Close()
	s := &Service{
		cfg:    &config.Config{},
		twitch: twitch.New(twitch.Config{GQLURL: edge.URL + "/gql", IntegrityURL: edge.URL + "/integrity", UsherBaseURL: edge.URL}, discardLog()),
	}
	_, err := s.LiveRenditions(t.Context(), "altair", false)
	var authErr *twitch.AuthError
	if !errors.As(err, &authErr) || authErr.Status != http.StatusNotFound {
		t.Fatalf("err = %v, want usher 404", err)
	}
}

func TestQualityCapPrefersThePinnedHeight(t *testing.T) {
	if got := (Params{Quality: "HIGH"}).qualityCap(); got != "1080" {
		t.Fatalf("tier cap = %s", got)
	}
	if got := (Params{Quality: "HIGH", MaxHeight: 936}).qualityCap(); got != "936" {
		t.Fatalf("pinned cap = %s", got)
	}
}

func TestLiveRenditionsAsksUsherForH264OnlyUnderForceH264(t *testing.T) {
	var base string
	var announced []string
	edge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gql" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"streamPlaybackAccessToken": map[string]string{"value": "signed", "signature": "sig"}}})
			return
		}
		announced = append(announced, r.URL.Query().Get("supported_codecs"))
		// Usher would not list HEVC after an h264-only announcement; serving
		// it anyway proves the pool filter drops it too.
		_, _ = fmt.Fprintf(w, liveRenditionsManifest, base, base, base, base, base)
	}))
	defer edge.Close()
	base = edge.URL
	s := &Service{
		cfg:    &config.Config{},
		twitch: twitch.New(twitch.Config{GQLURL: base + "/gql", IntegrityURL: base + "/integrity", UsherBaseURL: base}, discardLog()),
	}

	got, err := s.LiveRenditions(t.Context(), "altair", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(announced) != 1 || announced[0] != "h264" {
		t.Fatalf("supported_codecs = %v, want h264 only", announced)
	}
	for _, r := range got.Renditions {
		if r.Codec != twitch.CodecH264 {
			t.Fatalf("Force H.264 listed %+v", r)
		}
	}
	if len(got.Renditions) != 3 || got.Renditions[0].Height != 1080 {
		t.Fatalf("renditions = %+v, want the three H.264 rows", got.Renditions)
	}
}

// A crash between Start and the first stage boundary restarts the job from
// the row's checkpoint; the pinned height has to be in it from the insert.
func TestStartPersistsThePinnedHeightWithTheJobRow(t *testing.T) {
	f := newArchiveFixture(t, 1, 1)
	ctx := t.Context()
	jobID, err := f.svc.Start(ctx, Params{BroadcasterID: "bc-1", BroadcasterLogin: "bc-1", DisplayName: "bc-1", Quality: repository.QualityHigh, MaxHeight: 936})
	if err != nil {
		t.Fatal(err)
	}
	job, err := f.repo.GetJob(ctx, jobID)
	if err != nil {
		t.Fatal(err)
	}
	state, err := UnmarshalResumeState(job.ResumeState)
	if err != nil {
		t.Fatal(err)
	}
	if state.MaxHeight != 936 {
		t.Fatalf("checkpoint max_height = %d, want 936", state.MaxHeight)
	}
	if f.video(t, jobID).Quality != repository.QualityHigh {
		t.Fatalf("row quality = %q, want the tier", f.video(t, jobID).Quality)
	}
}
