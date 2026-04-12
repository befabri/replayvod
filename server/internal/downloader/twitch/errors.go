package twitch

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// permanentCodes enumerates Twitch error codes that retry/refresh
// will never fix. Two categories live here:
//
//   - Viewer-side entitlement restrictions (subscriber-only,
//     geoblock, VOD manifest restrictions). Normally arrive on
//     401/403; observed in yt-dlp extractor/twitch.py:206-223.
//
//   - GQL-contract failures like PersistedQueryNotFound. These
//     can surface with status=200 (the classifier's status check
//     would otherwise skip them) or, hypothetically, status=403.
//     Membership here short-circuits both paths.
//
// This list is intentionally conservative: when in doubt, we treat
// a 401/403 as refreshable and let the authRefreshes budget bound
// the retries. Adding a code here turns one more class of failures
// from "retry uselessly, then fail" into "fail fast with a useful
// message."
var permanentCodes = map[string]struct{}{
	// Entitlement restrictions.
	"unauthorized_entitlements":   {},
	"vod_manifest_restricted":     {},
	"subscriptions_restricted":    {},
	"subs_only_restricted":        {},
	"geoblock_restricted":         {},
	"content_restricted":          {},
	"content_moderation_required": {},
	// GQL hash drift — no amount of integrity-acquiring or
	// token-refreshing resolves a stale persisted-query hash.
	GQLCodePersistedQueryNotFound: {},
}

// GQL error code constants. GQL application errors don't carry a
// dedicated `error_code` field — Twitch surfaces them as free-form
// messages on the `errors[].message` path. We synthesize a stable
// code by matching on the message so the classifier can route them
// without a string-match escape hatch in every call site.
const (
	// GQLCodePersistedQueryNotFound means the SHA256 we sent isn't
	// registered on Twitch's GQL server — usually because the
	// schema drifted and our hash is stale. Retryable in principle
	// (re-sending with the full `query` body works) but in practice
	// means the sync broke and the next job will fail the same way.
	GQLCodePersistedQueryNotFound = "persisted_query_not_found"

	// GQLCodeServiceTimeout is Twitch's internal upstream timeout.
	// Retryable — the caller's backoff handles it.
	GQLCodeServiceTimeout = "service_timeout"

	// GQLCodeServiceUnavailable is the generic Twitch 5xx-in-200
	// signal. Retryable via backoff.
	GQLCodeServiceUnavailable = "service_unavailable"
)

// gqlMessageToCode recognizes a small set of GQL application-error
// messages and returns a stable code. Unknown messages return "".
// Kept intentionally narrow — we only synthesize codes for errors
// we know how to classify; anything else stays as a raw message
// and the classifier treats it as "not a permanent failure".
func gqlMessageToCode(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "persistedquerynotfound"):
		return GQLCodePersistedQueryNotFound
	case strings.Contains(lower, "service timeout"):
		return GQLCodeServiceTimeout
	case strings.Contains(lower, "service unavailable"):
		return GQLCodeServiceUnavailable
	}
	return ""
}

// AuthError carries everything the orchestrator needs to decide
// whether to refresh + retry or fail the job. Always wrap low-level
// 401/403s in AuthError so the classifier has something to work with.
type AuthError struct {
	// Status is the HTTP status code from the response that
	// triggered the error.
	Status int

	// Code is Twitch's machine-readable error identifier, parsed
	// from the JSON error body. Empty when the response wasn't
	// JSON or didn't carry a code.
	Code string

	// Message is a human-readable description — either from
	// Twitch's JSON body or a synthesized fallback.
	Message string

	// Body is the raw response body for debug logging. Truncated
	// to 4 KB at construction time.
	Body []byte

	// kind tracks whether this came from GQL application errors
	// or a transport-level non-2xx response. Consumers don't need
	// it, but the classifier uses it to refine decisions.
	kind authErrorKind
}

type authErrorKind int

const (
	authErrorKindHTTP authErrorKind = iota
	authErrorKindGQL
)

// Error makes AuthError a standard error. Format chosen so the
// string is useful in a log line without needing a custom formatter.
func (e *AuthError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("twitch auth %d: %s: %s", e.Status, e.Code, e.Message)
	}
	if e.Message != "" {
		return fmt.Sprintf("twitch auth %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("twitch auth %d", e.Status)
}

// NewAuthError constructs an AuthError from an HTTP status and
// response body. Attempts to parse a JSON error body to populate
// Code and Message; falls back to raw-body preview when parsing
// fails.
func NewAuthError(status int, body []byte) *AuthError {
	e := &AuthError{Status: status, kind: authErrorKindHTTP}
	if len(body) > 4<<10 {
		e.Body = append([]byte(nil), body[:4<<10]...)
	} else {
		e.Body = append([]byte(nil), body...)
	}

	// Twitch's error bodies come in several shapes:
	//   {"error": "...", "status": 403, "message": "..."}
	//   {"error_code": "...", "error": "..."} (usher)
	//   [{"error_code": "...", "error": "..."}] (usher array)
	// Decode into a superset and pick whichever is populated.
	var primary struct {
		Error     string `json:"error"`
		ErrorCode string `json:"error_code"`
		Message   string `json:"message"`
		Status    int    `json:"status"`
	}
	if err := json.Unmarshal(body, &primary); err == nil {
		e.Code = firstNonEmpty(primary.ErrorCode, primary.Error)
		e.Message = firstNonEmpty(primary.Message, primary.Error)
		return e
	}

	var arr []struct {
		Error     string `json:"error"`
		ErrorCode string `json:"error_code"`
	}
	if err := json.Unmarshal(body, &arr); err == nil && len(arr) > 0 {
		e.Code = firstNonEmpty(arr[0].ErrorCode, arr[0].Error)
		e.Message = arr[0].Error
		return e
	}

	// Un-parseable body — surface a short preview so logs are
	// useful without the raw dump.
	preview := string(body)
	if len(preview) > 200 {
		preview = preview[:200] + "…"
	}
	e.Message = preview
	return e
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// classifyAuthError decides what the caller should do next.
// Returns (permanent, isAuth):
//   - permanent=true: fail the job. No amount of retrying will help.
//   - permanent=false + isAuth=true: retryable auth — refresh and
//     try again (caller must honor its retry budget).
//   - permanent=false + isAuth=false: not an auth error at all —
//     likely transport; caller handles per its own policy.
//
// Permanent classification runs *before* the status check: codes
// like PersistedQueryNotFound come back with status=200 and would
// otherwise escape the status-gated auth branch entirely.
//
// nil error is treated as not-auth (caller gets (false, false)).
func classifyAuthError(err error) (permanent, isAuth bool) {
	if err == nil {
		return false, false
	}
	var ae *AuthError
	if !errors.As(err, &ae) {
		return false, false
	}
	isAuth401or403 := ae.Status == http.StatusUnauthorized || ae.Status == http.StatusForbidden
	if _, ok := permanentCodes[ae.Code]; ok {
		return true, isAuth401or403
	}
	// No permanent code: the only remaining way to be an "auth
	// error" is a 401/403. 5xx / 400 / 200-with-unknown-GQL-err
	// fall through as not-auth so the caller uses transport-
	// level retry instead of refresh.
	if !isAuth401or403 {
		return false, false
	}
	return false, true
}

// IsPermanent reports whether err is a permanent auth failure per
// the Twitch entitlement classification. Wraps classifyAuthError
// for external callers that only care about the binary answer.
func IsPermanent(err error) bool {
	perm, _ := classifyAuthError(err)
	return perm
}
