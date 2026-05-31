// Package videodownload mints and verifies signed, expiring, unauthenticated
// download URLs for a specific recorded video part.
//
// The session-gated streaming endpoint (api/video.streamVideo) is the right
// surface for a logged-in dashboard user, but it cannot serve an unattended
// consumer: a script or a remote service reacting to a recording webhook has no
// session cookie, and the streaming route only ever serves part 01 of a
// multi-part recording. These signed URLs close both gaps. Each one is scoped
// to one (video, part), carries an expiry, and is authenticated by an HMAC the
// receiver cannot forge, so the route that serves it needs no session.
//
// The signing key is DERIVED from the server HMAC secret rather than being the
// secret itself: deriveKey runs one HMAC with a fixed domain label, so the key
// used here is cryptographically independent of the one twitch.Verify-
// EventSubSignature checks. A receiver that learns a download signature learns
// HMAC(downloadKey, …), never HMAC(hmacSecret, …), so it can never be replayed
// against the inbound EventSub verifier. Reusing the already-bootstrapped HMAC
// secret this way avoids a second persisted secret and its migration.
package videodownload

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Query parameter names on a signed URL.
const (
	ParamExpires   = "exp"
	ParamSignature = "sig"
)

// keyLabel domain-separates the download-signing subkey from every other use of
// the server HMAC secret. Changing it invalidates every outstanding URL.
const keyLabel = "replayvod/video-download-key/v1"

// ErrInvalidSignature is returned by Verifier.Verify for any failure: a
// malformed expiry, an expired URL, or a signature mismatch. The reasons are
// deliberately collapsed into one error so the served route cannot become an
// oracle that distinguishes "expired" from "wrong key".
var ErrInvalidSignature = errors.New("videodownload: invalid or expired signature")

// deriveKey returns the download-signing subkey for a server HMAC secret.
func deriveKey(hmacSecret string) []byte {
	m := hmac.New(sha256.New, []byte(hmacSecret))
	m.Write([]byte(keyLabel))
	return m.Sum(nil)
}

// computeSig is the canonical signature over a (video, part, expiry) tuple. The
// newline-delimited, length-unambiguous layout means no two distinct tuples can
// produce the same signed message.
func computeSig(key []byte, videoID int64, partIndex int32, expUnix int64) string {
	m := hmac.New(sha256.New, key)
	fmt.Fprintf(m, "v1\n%d\n%d\n%d", videoID, partIndex, expUnix)
	return hex.EncodeToString(m.Sum(nil))
}

// Signer mints absolute signed URLs. baseURL is the public scheme://host the API
// is reachable at; an empty baseURL or a non-positive ttl disables minting
// (PartURL returns ""), which the payload builder treats as "omit the URL".
type Signer struct {
	key     []byte
	ttl     time.Duration
	baseURL string
	now     func() time.Time
}

// NewSigner builds a Signer. baseURL is trimmed of any trailing slash so URL
// assembly is a plain concatenation.
func NewSigner(hmacSecret, baseURL string, ttl time.Duration) *Signer {
	return &Signer{
		key:     deriveKey(hmacSecret),
		ttl:     ttl,
		baseURL: strings.TrimRight(baseURL, "/"),
		now:     time.Now,
	}
}

// Enabled reports whether this Signer can mint URLs. A disabled Signer is the
// honest "we cannot resolve a public origin or signed links are turned off"
// state, distinct from a present-but-broken one.
func (s *Signer) Enabled() bool {
	return s != nil && s.baseURL != "" && s.ttl > 0
}

// PartURL returns an absolute, signed, expiring download URL for one video part,
// or "" when the Signer is disabled.
func (s *Signer) PartURL(videoID int64, partIndex int32) string {
	return s.PartURLUntil(videoID, partIndex, nil)
}

// PartURLUntil returns a signed part URL whose expiry is capped at notAfter when
// provided. If that cap is already reached, it returns "" rather than minting a
// URL that advertises access beyond the recording's retention deadline.
func (s *Signer) PartURLUntil(videoID int64, partIndex int32, notAfter *time.Time) string {
	if !s.Enabled() {
		return ""
	}
	now := s.now()
	expAt := now.Add(s.ttl)
	if notAfter != nil && notAfter.Before(expAt) {
		expAt = *notAfter
	}
	if !expAt.After(now) {
		return ""
	}
	exp := expAt.Unix()
	sig := computeSig(s.key, videoID, partIndex, exp)
	return fmt.Sprintf("%s/api/v1/videos/%d/parts/%d/download?%s=%d&%s=%s",
		s.baseURL, videoID, partIndex, ParamExpires, exp, ParamSignature, sig)
}

// Verifier checks the query parameters of a signed URL. It is split from Signer
// so the serving route carries only the key, not the minting policy (ttl, base
// URL) it has no business knowing.
type Verifier struct {
	key []byte
	now func() time.Time
}

// NewVerifier builds a Verifier from the same server HMAC secret a Signer uses.
func NewVerifier(hmacSecret string) *Verifier {
	return &Verifier{key: deriveKey(hmacSecret), now: time.Now}
}

// Verify reports whether sig authenticates (videoID, partIndex) and has not
// expired. rawExpires and sig are the untrusted query-string values. The
// signature comparison is constant time.
func (v *Verifier) Verify(videoID int64, partIndex int32, rawExpires, sig string) error {
	exp, err := strconv.ParseInt(rawExpires, 10, 64)
	if err != nil {
		return ErrInvalidSignature
	}
	if v.now().Unix() > exp {
		return ErrInvalidSignature
	}
	want := computeSig(v.key, videoID, partIndex, exp)
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return ErrInvalidSignature
	}
	return nil
}

// SameOrigin is a small helper for callers that already hold a parsed base URL
// and want the scheme://host form. Unused by the signer itself; kept here so the
// origin-derivation rule lives next to the URLs it builds.
func SameOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
