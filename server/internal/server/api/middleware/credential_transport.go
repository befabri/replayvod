package middleware

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/befabri/trpcgo"
)

type transportPeerKey struct{}

// CaptureTransportPeer must precede proxy-address rewriting middleware. The
// original peer is used only for the local HTTP exception, never forwarded IPs.
func CaptureTransportPeer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), transportPeerKey{}, r.RemoteAddr)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// TRPCCredentialTransport returns middleware that rejects a credential import
// unless the request arrives over HTTPS or on loopback, so an insecure
// submission is rejected before validation or persistence. Behind a TLS terminator the
// HTTPS contract comes from publicOrigin, never from caller-controlled
// Forwarded or X-Forwarded-Proto headers; the proxy must redirect HTTP and keep
// the backend private.
func TRPCCredentialTransport(publicOrigin string) trpcgo.Middleware {
	return func(next trpcgo.HandlerFunc) trpcgo.HandlerFunc {
		return func(ctx context.Context, input any) (any, error) {
			if !credentialRequestAllowed(getHTTPRequest(ctx), publicOrigin) {
				return nil, trpcgo.NewError(trpcgo.CodeBadRequest, "Twitch session import requires HTTPS for the dashboard and public API, or a local loopback connection")
			}
			return next(ctx, input)
		}
	}
}

func credentialRequestAllowed(r *http.Request, publicOrigin string) bool {
	if r == nil {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && !(u.Scheme == "http" && loopbackHost(u.Hostname()))) {
			return false
		}
	}
	if r.TLS != nil {
		return true
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	remote := r.RemoteAddr
	if original, ok := r.Context().Value(transportPeerKey{}).(string); ok {
		remote = original
	}
	peer, _, err := net.SplitHostPort(remote)
	if err == nil && loopbackHost(host) && net.ParseIP(peer).IsLoopback() {
		return true
	}
	u, err := url.Parse(publicOrigin)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && strings.EqualFold(u.Host, r.Host)
}

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	return net.ParseIP(strings.Trim(host, "[]")).IsLoopback()
}
