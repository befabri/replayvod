package twitch

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// rewriteTransport swaps the scheme+host of outbound requests to
// point at the test server, preserving the path so the handler can
// dispatch on it. Needed because Client embeds the real gql/usher
// URLs as constants.
type rewriteTransport struct {
	base  string
	inner http.RoundTripper
}

func (r *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newURL := r.base + req.URL.Path
	if req.URL.RawQuery != "" {
		newURL += "?" + req.URL.RawQuery
	}
	r2 := req.Clone(req.Context())
	r2.URL, _ = req.URL.Parse(newURL)
	r2.Host = r2.URL.Host
	return r.inner.RoundTrip(r2)
}

func newRoutedClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	httpClient := &http.Client{Transport: &rewriteTransport{
		base:  srv.URL,
		inner: srv.Client().Transport,
	}}
	return New(Config{
		HTTPClient: httpClient,
		ClientID:   "test-client-id",
		UserAgent:  "test-ua",
		DeviceID:   "test-device",
	}, slog.New(slog.DiscardHandler))
}

func TestPlaybackToken_HappyPath(t *testing.T) {
	var gqlCalls int
	h := http.NewServeMux()
	h.HandleFunc("/gql", func(w http.ResponseWriter, r *http.Request) {
		gqlCalls++
		// First attempt must not carry Client-Integrity header.
		if r.Header.Get("Client-Integrity") != "" {
			t.Errorf("happy path sent Client-Integrity on first call")
		}
		if r.Header.Get("Client-ID") != "test-client-id" {
			t.Errorf("Client-ID header not set")
		}
		if r.Header.Get("Device-Id") != "test-device" {
			t.Errorf("Device-Id header not set")
		}
		_, _ = w.Write([]byte(`{"data":{"streamPlaybackAccessToken":{"value":"TOKEN","signature":"SIG"}}}`))
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newRoutedClient(t, srv)

	tok, err := c.PlaybackToken(context.Background(), "altair", "")
	if err != nil {
		t.Fatalf("PlaybackToken: %v", err)
	}
	if tok.Value != "TOKEN" || tok.Signature != "SIG" {
		t.Errorf("got %+v, want TOKEN/SIG", tok)
	}
	if gqlCalls != 1 {
		t.Errorf("gqlCalls=%d, want 1", gqlCalls)
	}
}

func TestPlaybackToken_IntegrityFallback(t *testing.T) {
	var gqlCalls, integrityCalls int
	h := http.NewServeMux()
	h.HandleFunc("/gql", func(w http.ResponseWriter, r *http.Request) {
		gqlCalls++
		if r.Header.Get("Client-Integrity") == "" {
			// First attempt → empty value triggers the fallback.
			_, _ = w.Write([]byte(`{"data":{"streamPlaybackAccessToken":{"value":"","signature":""}}}`))
			return
		}
		// Second attempt with integrity → real token.
		if got := r.Header.Get("Client-Integrity"); got != "INTEGRITY-TOKEN" {
			t.Errorf("second attempt Client-Integrity=%q, want INTEGRITY-TOKEN", got)
		}
		_, _ = w.Write([]byte(`{"data":{"streamPlaybackAccessToken":{"value":"TOKEN","signature":"SIG"}}}`))
	})
	h.HandleFunc("/integrity", func(w http.ResponseWriter, r *http.Request) {
		integrityCalls++
		if r.Method != http.MethodPost {
			t.Errorf("integrity method=%s, want POST", r.Method)
		}
		resp := map[string]any{
			"token":      "INTEGRITY-TOKEN",
			"expiration": 9_999_999_999_999, // far in the future
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newRoutedClient(t, srv)

	tok, err := c.PlaybackToken(context.Background(), "altair", "")
	if err != nil {
		t.Fatalf("PlaybackToken: %v", err)
	}
	if tok.Value != "TOKEN" {
		t.Errorf("got Value=%q, want TOKEN", tok.Value)
	}
	if gqlCalls != 2 {
		t.Errorf("gqlCalls=%d, want 2 (first empty, second with integrity)", gqlCalls)
	}
	if integrityCalls != 1 {
		t.Errorf("integrityCalls=%d, want 1", integrityCalls)
	}
}

func TestPlaybackToken_PermanentEntitlement(t *testing.T) {
	var gqlCalls, integrityCalls int
	h := http.NewServeMux()
	h.HandleFunc("/gql", func(w http.ResponseWriter, r *http.Request) {
		gqlCalls++
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"Forbidden","error_code":"unauthorized_entitlements","message":"subscribe-only"}`))
	})
	h.HandleFunc("/integrity", func(w http.ResponseWriter, r *http.Request) {
		integrityCalls++
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newRoutedClient(t, srv)

	_, err := c.PlaybackToken(context.Background(), "sub_only_channel", "")
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !IsPermanent(err) {
		t.Errorf("err not classified permanent: %v", err)
	}
	if gqlCalls != 1 {
		t.Errorf("gqlCalls=%d, want 1 (no integrity fallback for permanent)", gqlCalls)
	}
	if integrityCalls != 0 {
		t.Errorf("integrityCalls=%d, want 0", integrityCalls)
	}
}

func TestPlaybackToken_AuthedPlayback(t *testing.T) {
	var sawOAuth bool
	h := http.NewServeMux()
	h.HandleFunc("/gql", func(w http.ResponseWriter, r *http.Request) {
		sawOAuth = r.Header.Get("Authorization") == "OAuth user-access-token"
		_, _ = w.Write([]byte(`{"data":{"streamPlaybackAccessToken":{"value":"TOKEN","signature":"SIG"}}}`))
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newRoutedClient(t, srv)

	_, err := c.PlaybackToken(context.Background(), "altair", "user-access-token")
	if err != nil {
		t.Fatalf("PlaybackToken: %v", err)
	}
	if !sawOAuth {
		t.Error("Authorization: OAuth <token> header not sent when accessToken provided")
	}
}

func TestPlaybackToken_GQLPayloadShape(t *testing.T) {
	// Pin the outbound GQL body shape — regressions here break
	// Twitch-side parsing silently. The persisted-query hash in
	// particular is the thing most likely to drift.
	h := http.NewServeMux()
	h.HandleFunc("/gql", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed gqlPersistedQuery
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatalf("server side decode: %v", err)
		}
		if parsed.OperationName != "PlaybackAccessToken_Template" {
			t.Errorf("OperationName=%q", parsed.OperationName)
		}
		if parsed.Extensions.PersistedQuery.SHA256Hash != playbackAccessTokenSHA256 {
			t.Errorf("hash drifted: %q", parsed.Extensions.PersistedQuery.SHA256Hash)
		}
		if parsed.Variables["login"] != "altair" {
			t.Errorf("login=%v", parsed.Variables["login"])
		}
		if parsed.Variables["isLive"] != true {
			t.Errorf("isLive=%v", parsed.Variables["isLive"])
		}
		_, _ = io.Copy(w, strings.NewReader(`{"data":{"streamPlaybackAccessToken":{"value":"T","signature":"S"}}}`))
	})
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newRoutedClient(t, srv)

	if _, err := c.PlaybackToken(context.Background(), "altair", ""); err != nil {
		t.Fatalf("PlaybackToken: %v", err)
	}
}

