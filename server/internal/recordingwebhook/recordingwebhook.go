// Package recordingwebhook implements the generic outbound webhook ReplayVOD
// fires when a recording reaches a terminal state (recording.completed /
// recording.failed). It is the project's "react to something" primitive: a
// self-hoster points it at any receiver — a media-server refresh, a notifier, a
// post-processing or upload script — with zero integration-specific code in the
// app. The server never knows or cares what is on the other end.
//
// The feature has three parts, kept in separate files:
//
//   - Service (config.go): owner-managed configuration persisted in
//     server_settings — enabled, target URL, signing secret, and which events
//     to fire. The signing secret is auto-generated when blank, exactly like the
//     EventSub HMAC secret.
//   - buildPayload (payload.go): assembles the JSON body from the video, its
//     parts, and its metadata at delivery time.
//   - Dispatcher (dispatcher.go): drains the durable delivery outbox, signs the
//     body, and POSTs it with persisted retry/backoff state.
//
// Deliveries are signed HMAC-SHA256 over id‖timestamp‖body exactly like the
// inbound Twitch EventSub convention (see twitch.VerifyEventSubSignature), so a
// receiver verifies an outbound delivery with the same computation it would use
// for Twitch.
package recordingwebhook

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/befabri/replayvod/server/internal/eventbus"
)

// Event type identifiers. These are the canonical strings stored in the events
// allowlist, sent in the Replayvod-Webhook-Event header, and echoed in the
// payload's `event` field.
const (
	EventCompleted = "recording.completed"
	EventFailed    = "recording.failed"
	// EventTest is the event identifier of a dashboard "send test" delivery. It
	// is deliberately NOT in knownEvents: it can never be stored in the
	// allowlist or fired by a recording, only minted by SendTest so an owner can
	// verify their receiver (and signature) before a real recording happens.
	EventTest = "recording.test"
)

// knownEvents is the closed set of event identifiers a recording can fire, in
// canonical order. EventTest is intentionally excluded (see its doc).
var knownEvents = []string{EventCompleted, EventFailed}

// ErrInvalid marks a configuration the owner cannot save (bad URL, unknown
// event). The tRPC handler maps it to a 400 so the dashboard shows the message.
var ErrInvalid = errors.New("recording webhook config: invalid")

// invalidError carries a human-readable validation message while still
// matching ErrInvalid via errors.Is, mirroring eventsubconfig's pattern.
type invalidError struct{ message string }

func (e invalidError) Error() string        { return e.message }
func (e invalidError) Is(target error) bool { return target == ErrInvalid }

// eventForKind maps the eventbus terminal kind to its event identifier.
func eventForKind(kind eventbus.RecordingTerminalKind) string {
	switch kind {
	case eventbus.RecordingCompleted:
		return EventCompleted
	case eventbus.RecordingFailed:
		return EventFailed
	default:
		return ""
	}
}

// generateSecret returns 32 random bytes hex-encoded (64 characters). Same
// shape and entropy as the EventSub HMAC secret so the two are interchangeable
// to a receiver that already verifies Twitch signatures.
func generateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate webhook secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// validateURL enforces that an enabled webhook points somewhere safe and
// well-formed.
//
// Scheme rule: https is required for a public host, but http is allowed for the
// local machine OR a private/LAN address. A self-hosted recorder's most common
// receiver is another box on the same LAN (a media server, notifier, or NAS)
// that has no TLS, so requiring https there would block the feature's primary
// use; the delivery is HMAC-signed regardless, so cleartext on the operator's
// own network is their call. Public targets still must use https.
//
// SSRF posture: this is an owner-only setting on a single-owner deployment, so a
// blanket private-range denylist would be theater that breaks the legitimate LAN
// case (see the four-way investigation in the PR). The one target with no
// legitimate use and a real, asymmetric downside is the link-local /
// cloud-metadata range (169.254.0.0/16, fe80::/10 — 169.254.169.254 hands out
// instance credentials on a cloud VM), so that is rejected for any scheme.
// Note this is a parse-time check on IP-literal hosts: a hostname that *resolves*
// to a link-local address at delivery time is not caught here. Closing that
// needs a connect-time net.Dialer.Control guard, which the owner-only trust
// model does not warrant; redirects are already refused (see dispatcher).
func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return invalidError{message: "webhook URL is not a valid URL"}
	}
	host := u.Hostname()
	if host == "" {
		return invalidError{message: "webhook URL must be absolute (include a host)"}
	}
	if isLinkLocalHost(host) {
		return invalidError{message: "webhook URL must not target a link-local or cloud-metadata address (169.254.0.0/16)"}
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLANHost(host) {
			return nil
		}
		return invalidError{message: "webhook URL must use https (http is allowed only for localhost or a private/LAN address)"}
	default:
		return invalidError{message: "webhook URL must use https"}
	}
}

// isLinkLocalHost reports whether host is an IP literal in the link-local /
// cloud-metadata range (169.254.0.0/16 or fe80::/10). URL.Hostname returns
// scoped IPv6 literals with the zone decoded (e.g. "fe80::1%eth0"); strip that
// zone before parsing so a link-local address cannot bypass validation merely
// by naming an interface.
func isLinkLocalHost(host string) bool {
	ip := parseHostIP(host)
	return ip != nil && (ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast())
}

// isLANHost reports whether host is the local machine or a private/LAN address
// (localhost, loopback, RFC1918, or IPv6 ULA) — the destinations for which http
// is acceptable.
func isLANHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := parseHostIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func parseHostIP(host string) net.IP {
	if strings.Contains(host, ":") {
		if before, _, ok := strings.Cut(host, "%"); ok {
			host = before
		}
	}
	return net.ParseIP(host)
}

// normalizeEvents validates and canonicalizes the requested event allowlist.
// Empty (nil or all-blank) means "all events" and is stored as the empty
// string. Unknown identifiers are rejected. The result is deduplicated and
// ordered canonically so the stored value is stable regardless of input order.
func normalizeEvents(events []string) ([]string, error) {
	seen := make(map[string]bool, len(events))
	for _, e := range events {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if !isKnownEvent(e) {
			return nil, invalidError{message: fmt.Sprintf("unknown webhook event %q", e)}
		}
		seen[e] = true
	}
	if len(seen) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(seen))
	for e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		return eventOrder(out[i]) < eventOrder(out[j])
	})
	return out, nil
}

func isKnownEvent(e string) bool {
	return slices.Contains(knownEvents, e)
}

// eventOrder returns the canonical sort index of an event identifier.
func eventOrder(e string) int {
	for i, k := range knownEvents {
		if k == e {
			return i
		}
	}
	return len(knownEvents)
}

// parseEvents splits a stored comma-separated allowlist back into a slice,
// dropping blanks. The empty string yields nil ("all events").
func parseEvents(stored string) []string {
	if strings.TrimSpace(stored) == "" {
		return nil
	}
	raw := strings.Split(stored, ",")
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
