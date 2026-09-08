package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVODPlaybackToken_SendsVODVariablesAndReadsVideoToken(t *testing.T) {
	var got map[string]any
	h := http.NewServeMux()
	h.HandleFunc("/gql", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			OperationName string         `json:"operationName"`
			Variables     map[string]any `json:"variables"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("decode gql body: %v", err)
		}
		got = req.Variables
		// The live token must not be mistaken for the VOD one.
		_, _ = w.Write([]byte(`{"data":{"streamPlaybackAccessToken":{"value":"LIVE","signature":"LIVESIG"},"videoPlaybackAccessToken":{"value":"VODTOKEN","signature":"VODSIG"}}}`))
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newRoutedClient(t, srv)

	tok, err := c.VODPlaybackToken(context.Background(), "123456789", "")
	if err != nil {
		t.Fatalf("VODPlaybackToken: %v", err)
	}
	if tok.Value != "VODTOKEN" || tok.Signature != "VODSIG" {
		t.Errorf("token = %+v, want the videoPlaybackAccessToken pair", tok)
	}
	if got["isVod"] != true || got["isLive"] != false {
		t.Errorf("variables isVod=%v isLive=%v, want true/false", got["isVod"], got["isLive"])
	}
	if got["vodID"] != "123456789" || got["login"] != "" {
		t.Errorf("variables vodID=%v login=%v, want the id and an empty login", got["vodID"], got["login"])
	}
}

func TestVODPlaybackToken_RejectsEmptyID(t *testing.T) {
	c := newRoutedClient(t, httptest.NewServer(http.NewServeMux()))
	if _, err := c.VODPlaybackToken(context.Background(), "", ""); err == nil {
		t.Fatalf("empty vod id accepted")
	}
}

func TestVODPlaybackToken_MissingVideoTokenIsEmpty(t *testing.T) {
	var calls int
	h := http.NewServeMux()
	h.HandleFunc("/gql", func(w http.ResponseWriter, r *http.Request) {
		calls++
		// Twitch answers a VOD query for an unknown or private id with data
		// present but no videoPlaybackAccessToken. That must read as an
		// empty token (integrity retry, then ErrPlaybackTokenEmpty), never
		// as the live token that may sit next to it.
		_, _ = w.Write([]byte(`{"data":{"streamPlaybackAccessToken":{"value":"LIVE","signature":"LIVESIG"}}}`))
	})
	h.HandleFunc("/integrity", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"token":"integrity-token","expiration":4102444800000}`))
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newRoutedClient(t, srv)

	_, err := c.VODPlaybackToken(context.Background(), "42", "")
	if err == nil {
		t.Fatalf("expected an error for a response without a VOD token")
	}
	if calls != 2 {
		t.Errorf("gql calls = %d, want 2 (anonymous then integrity retry)", calls)
	}
}

func TestFetchVODMasterPlaylist_UsesVODEndpointAndNauthParams(t *testing.T) {
	var gotPath string
	var gotQuery map[string][]string
	h := http.NewServeMux()
	h.HandleFunc("/vod/", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		_, _ = w.Write([]byte(strings.Join([]string{
			"#EXTM3U",
			`#EXT-X-STREAM-INF:BANDWIDTH=6000000,RESOLUTION=1920x1080,CODECS="avc1.64002A,mp4a.40.2",VIDEO="chunked",FRAME-RATE=60.000`,
			"https://cdn.example/vod/chunked/index-dvr.m3u8",
			`#EXT-X-STREAM-INF:BANDWIDTH=160000,CODECS="mp4a.40.2",VIDEO="audio_only"`,
			"https://cdn.example/vod/audio_only/index-dvr.m3u8",
		}, "\n") + "\n"))
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newRoutedClient(t, srv)

	m, err := c.FetchVODMasterPlaylist(context.Background(), "987", PlaybackToken{Value: "TOK", Signature: "SIG"}, SelectOptions{})
	if err != nil {
		t.Fatalf("FetchVODMasterPlaylist: %v", err)
	}
	if gotPath != "/vod/987.m3u8" {
		t.Errorf("path = %q, want /vod/987.m3u8", gotPath)
	}
	if got := gotQuery["nauth"]; len(got) != 1 || got[0] != "TOK" {
		t.Errorf("nauth = %v, want TOK", got)
	}
	if got := gotQuery["nauthsig"]; len(got) != 1 || got[0] != "SIG" {
		t.Errorf("nauthsig = %v, want SIG", got)
	}
	if _, live := gotQuery["token"]; live {
		t.Errorf("live token parameter sent on the VOD endpoint")
	}
	if got := gotQuery["allow_source"]; len(got) != 1 || got[0] != "true" {
		t.Errorf("allow_source = %v, want true", got)
	}
	if len(m.Variants) != 2 || m.Variants[0].Quality != "1080" || !m.Variants[1].IsAudioOnly() {
		t.Errorf("variants = %+v, want 1080 + audio_only", m.Variants)
	}
}

func TestFetchVODMasterPlaylist_Rejections(t *testing.T) {
	h := http.NewServeMux()
	h.HandleFunc("/vod/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`[{"error_code":"unauthorized_entitlements","error":"sub only"}]`))
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newRoutedClient(t, srv)
	tok := PlaybackToken{Value: "TOK", Signature: "SIG"}

	if _, err := c.FetchVODMasterPlaylist(context.Background(), "", tok, SelectOptions{}); err == nil {
		t.Errorf("empty vod id accepted")
	}
	if _, err := c.FetchVODMasterPlaylist(context.Background(), "1", PlaybackToken{}, SelectOptions{}); err == nil {
		t.Errorf("empty token accepted")
	}
	_, err := c.FetchVODMasterPlaylist(context.Background(), "1", tok, SelectOptions{})
	var ae *AuthError
	if !errors.As(err, &ae) || ae.Status != http.StatusForbidden {
		t.Fatalf("403 err = %v, want *AuthError with status 403", err)
	}
}
