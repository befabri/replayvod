package middleware

import (
	"context"
	"crypto/tls"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCredentialRequestTransport(t *testing.T) {
	for _, tc := range []struct {
		name, host, peer, origin, public, forwarded string
		tls, want                                   bool
	}{
		{name: "TLS", host: "replay.example", tls: true, want: true},
		{name: "HTTPS proxy", host: "replay.example", public: "https://replay.example", want: true},
		{name: "HTTP remote", host: "replay.example", public: "http://replay.example"},
		{name: "spoofed proto", host: "replay.example", forwarded: "https"},
		{name: "proxy host mismatch", host: "other.example", public: "https://replay.example", forwarded: "https"},
		{name: "insecure dashboard", host: "replay.example", public: "https://replay.example", origin: "http://dashboard.example"},
		{name: "opaque origin", host: "replay.example", public: "https://replay.example", origin: "null"},
		{name: "local", host: "localhost:8080", peer: "127.0.0.1:1234", origin: "http://localhost:3000", want: true},
		{name: "IPv6", host: "[::1]:8080", peer: "[::1]:1234", origin: "http://[::1]:3000", want: true},
		{name: "forged local host", host: "localhost:8080", peer: "192.0.2.10:1234", forwarded: "https"},
		{name: "localhost lookalike", host: "localhost.evil:8080", peer: "127.0.0.1:1234"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/trpc/twitchPlayback.connect", nil)
			req.RemoteAddr = tc.peer
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("X-Forwarded-Proto", tc.forwarded)
			if tc.tls {
				req.TLS = &tls.ConnectionState{}
			}
			called := false
			handler := TRPCCredentialTransport(tc.public)(func(context.Context, any) (any, error) { called = true; return nil, nil })
			_, err := handler(WithContextCreator(context.Background(), req), nil)
			if called != tc.want || (err == nil) != tc.want {
				t.Fatalf("called=%v err=%v", called, err)
			}
		})
	}
	if credentialRequestAllowed(nil, "https://replay.example") {
		t.Fatal("missing request allowed")
	}
}

func TestCredentialTransportUsesSocketPeerBeforeRealIP(t *testing.T) {
	for _, tc := range []struct {
		name, peer, forwarded string
		want                  bool
	}{
		{"local proxy", "127.0.0.1:1234", "198.51.100.9", true},
		{"spoofed loopback", "198.51.100.9:1234", "127.0.0.1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var allowed bool
			handler := CaptureTransportPeer(chimiddleware.RealIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				allowed = credentialRequestAllowed(r, "")
			})))
			req := httptest.NewRequest(http.MethodPost, "http://localhost:8080/trpc/twitchPlayback.connect", nil)
			req.RemoteAddr = tc.peer
			req.Header.Set("X-Forwarded-For", tc.forwarded)
			handler.ServeHTTP(httptest.NewRecorder(), req)
			if allowed != tc.want {
				t.Fatalf("allowed = %v, want %v", allowed, tc.want)
			}
		})
	}
}
