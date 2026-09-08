package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/downloader/twitch"
	"github.com/befabri/replayvod/server/internal/playbackauth"
	"github.com/befabri/replayvod/server/internal/repository"
)

type playbackCredentialStub struct {
	token     string
	err       error
	rechecked bool
}

func (p *playbackCredentialStub) Token(context.Context) (string, error) { return p.token, p.err }
func (p *playbackCredentialStub) RecheckRejected(context.Context, string) error {
	p.rechecked = true
	return playbackauth.ErrRejected
}

func TestPlaybackConnectionSuppliesWebsiteTokenOnlyToGQL(t *testing.T) {
	credentials := &playbackCredentialStub{token: "private-website-session"}
	var base string
	var authorization []string
	reject := false
	edge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gql" {
			authorization = append(authorization, r.Header.Get("Authorization"))
			if reject && r.Header.Get("Authorization") != "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"streamPlaybackAccessToken": map[string]string{"value": "signed-playback", "signature": "signature"}}})
			return
		}
		if r.URL.Path == "/integrity" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("website session leaked to CDN")
		}
		if r.URL.Query().Get("supported_codecs") != "h265,h264" {
			t.Error("incorrect codec announcement")
		}
		if _, err := fmt.Fprintf(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=9805284,RESOLUTION=1920x1080,FRAME-RATE=60,CODECS=\"hev1.1.6.L150.90,mp4a.40.2\"\n%s/1080.m3u8\n", base); err != nil {
			t.Error(err)
		}
	}))
	defer edge.Close()
	base = edge.URL
	s := &Service{twitch: twitch.New(twitch.Config{GQLURL: base + "/gql", IntegrityURL: base + "/integrity", UsherBaseURL: base}, discardLog())}
	s.SetPlaybackCredentials(credentials)
	resolve := func() {
		t.Helper()
		got, err := s.resolveVariantURL(t.Context(), Params{BroadcasterLogin: "altair"}, twitch.SelectOptions{Quality: qualityToHeight(repository.QualityHigh)})
		if err != nil || got.Quality != "1080" || got.Codec != twitch.CodecH265 {
			t.Fatalf("selection: %+v %v", got, err)
		}
	}
	resolve()
	if len(authorization) != 1 || authorization[0] != "OAuth "+credentials.token {
		t.Fatalf("authorization = %v", authorization)
	}
	credentials.err = playbackauth.ErrRejected
	resolve()
	if len(authorization) != 2 || authorization[1] != "" {
		t.Fatalf("revoked session did not fall back anonymously: %v", authorization)
	}
	credentials.err = nil
	reject = true
	resolve()
	if !credentials.rechecked || len(authorization) != 4 || authorization[2] != "OAuth "+credentials.token || authorization[3] != "" {
		t.Fatalf("GQL revocation recovery = %v, rechecked=%v", authorization, credentials.rechecked)
	}
}

type playbackValidatorFunc func(context.Context, string) (playbackauth.Identity, error)

func (f playbackValidatorFunc) Validate(ctx context.Context, token string) (playbackauth.Identity, error) {
	return f(ctx, token)
}

func TestPlaybackResolutionReloadsSessionAfterStaleGQLRejection(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		name := "replace"
		if disconnect {
			name = "disconnect"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := newTestService(t, t.TempDir())
			credentials := playbackauth.New(s.repo, strings.Repeat("s", 32), playbackValidatorFunc(func(context.Context, string) (playbackauth.Identity, error) {
				return playbackauth.Identity{UserID: "123", Login: "viewer"}, nil
			}))
			const old = "old-session-0123456789abcdef"
			const replacement = "new-session-0123456789abcdef"
			if _, err := credentials.Connect(ctx, old); err != nil {
				t.Fatal(err)
			}
			s.SetPlaybackCredentials(credentials)
			gqlCalls := 0
			var base string
			edge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/gql" {
					gqlCalls++
					if gqlCalls == 1 {
						if r.Header.Get("Authorization") != "OAuth "+old {
							t.Error("initial request did not use old session")
						}
						var err error
						if disconnect {
							_, err = credentials.Disconnect(ctx)
						} else {
							_, err = credentials.Connect(ctx, replacement)
						}
						if err != nil {
							t.Error(err)
						}
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					expected := "OAuth " + replacement
					if disconnect {
						expected = ""
					}
					if r.Header.Get("Authorization") != expected {
						t.Error("retry reused replaced/disconnected session")
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"streamPlaybackAccessToken": map[string]string{"value": "signed", "signature": "sig"}}})
					return
				}
				if r.URL.Path == "/integrity" {
					w.WriteHeader(503)
					return
				}
				if r.Header.Get("Authorization") != "" {
					t.Error("credential sent to CDN")
				}
				if _, err := fmt.Fprintf(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=200000,RESOLUTION=1280x720,CODECS=\"avc1.4d401f,mp4a.40.2\"\n%s/media.m3u8\n", base); err != nil {
					t.Error(err)
				}
			}))
			defer edge.Close()
			base = edge.URL
			s.twitch = twitch.New(twitch.Config{GQLURL: base + "/gql", IntegrityURL: base + "/integrity", UsherBaseURL: base}, discardLog())
			got, err := retryPlaybackResolution(ctx, func(ctx context.Context) (twitch.SelectedVariant, error) {
				return s.resolveVariantURL(ctx, Params{BroadcasterLogin: "viewer"}, twitch.SelectOptions{Quality: "1080"})
			})
			if err != nil || got.Quality != "720" || gqlCalls != 2 {
				t.Fatalf("stale rejection recovery: quality=%s GQL calls=%d err=%v", got.Quality, gqlCalls, err)
			}
		})
	}
}
