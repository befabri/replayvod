package twitch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// PlaybackAccessToken persisted query hash + operation name.
// Twitch accepts GQL calls either as a full query string or as a
// persisted-query reference — the reference form is ~40x smaller
// over the wire and is what the web player sends. The hash is
// public and observable; any change to Twitch's GQL schema
// invalidates it, at which point we'd update this constant.
//
// Operation name is "PlaybackAccessToken" — the hash above is
// server-side registered under that bare name. The real web
// client sends the full inline query string with operationName
// "PlaybackAccessToken_Template", but we use the persisted-query
// path (same as streamlink + dropsminer), so the op name must
// match what the hash resolves to. Claiming "_Template" with this
// hash returns `no operation with name "PlaybackAccessToken_Template"`.
const (
	playbackAccessTokenOp     = "PlaybackAccessToken"
	playbackAccessTokenSHA256 = "ed230aa1e33e07eebb8928504583da78a5173989fadfb1ac94be06a04f3cdbe9"
)

// gqlPersistedQuery is the envelope Twitch expects for a persisted
// query call. OperationName is the stored operation (see
// playbackAccessTokenOp); Variables carries the query-specific input
// (login + isLive etc. for playback token); Extensions.PersistedQuery
// is the hash lookup that tells Twitch which canned query to run.
type gqlPersistedQuery struct {
	OperationName string            `json:"operationName"`
	Variables     map[string]any    `json:"variables"`
	Extensions    gqlExtensions     `json:"extensions"`
	Query         string            `json:"query,omitempty"`
}

type gqlExtensions struct {
	PersistedQuery gqlPersistedRef `json:"persistedQuery"`
}

type gqlPersistedRef struct {
	Version    int    `json:"version"`
	SHA256Hash string `json:"sha256Hash"`
}

// gqlPlaybackResponse is the partial GQL response shape we decode.
// The real payload has more fields (cached channelTitle etc.) but
// we only need value + signature.
type gqlPlaybackResponse struct {
	Errors []gqlError            `json:"errors,omitempty"`
	Data   *gqlPlaybackResponseData `json:"data,omitempty"`
}

type gqlPlaybackResponseData struct {
	StreamPlaybackAccessToken *playbackTokenRaw `json:"streamPlaybackAccessToken,omitempty"`
	VideoPlaybackAccessToken  *playbackTokenRaw `json:"videoPlaybackAccessToken,omitempty"`
}

type playbackTokenRaw struct {
	Value     string `json:"value"`
	Signature string `json:"signature"`
}

type gqlError struct {
	Message string `json:"message"`
}

// PlaybackToken performs the GQL PlaybackAccessToken_Template call
// for a live channel. login is the broadcaster login (lowercase, as
// Twitch expects it). Returns the signed playback token used to
// fetch the master playlist.
//
// Client-Integrity fallback (streamlink plugins/twitch.py:517-545):
//  1. First attempt: no integrity header.
//  2. If error / empty value / retryable auth failure: acquire
//     integrity, retry once.
//  3. If still failing: return the permanent error so the
//     orchestrator can classify it (permanent entitlement vs.
//     exhausted retries).
//
// Authenticated playback: if ServiceAccountRefreshToken is set on
// the client and accessToken is non-empty, we include it as an
// Authorization: OAuth header. accessToken is passed in rather than
// fetched here because refresh-token → access-token exchange lives
// in the project's existing OAuth plumbing, not in this package.
func (c *Client) PlaybackToken(ctx context.Context, login, accessToken string) (PlaybackToken, error) {
	c.log.Debug("playback token attempt", "login", login, "authenticated", accessToken != "")
	// First attempt — no integrity.
	token, err := c.playbackAttempt(ctx, login, accessToken, "")
	if err == nil && !token.Empty() {
		return token, nil
	}
	// Any non-permanent failure on the anonymous attempt warrants
	// the integrity retry. Twitch's anti-abuse layer commonly
	// fast-rejects unauthenticated requests with a generic
	// `{"errors":[{"message":"server error"}]}` at HTTP 200 — the
	// path that integrity is specifically designed to get past.
	// Only permanent entitlement codes short-circuit.
	permanent, _ := classifyAuthError(err)
	if permanent {
		return PlaybackToken{}, err
	}

	c.log.Debug("playback token retry with integrity", "login", login, "error", err)

	integrity, iErr := c.integrity.Acquire(ctx, c)
	if iErr != nil {
		// Can't acquire integrity — surface the original error,
		// which is more actionable than "integrity endpoint
		// down".
		if err != nil {
			return PlaybackToken{}, fmt.Errorf("%w (integrity acquire failed: %v)", err, iErr)
		}
		return PlaybackToken{}, fmt.Errorf("integrity acquire failed: %w", iErr)
	}

	token, err = c.playbackAttempt(ctx, login, accessToken, integrity)
	if err != nil {
		return PlaybackToken{}, err
	}
	if token.Empty() {
		// Integrity path also returned empty — most likely a
		// permanent restriction that integrity can't bypass.
		// Invalidate the cached integrity (might be stale) so
		// the next job acquires fresh.
		c.integrity.MarkBad()
		return PlaybackToken{}, ErrPlaybackTokenEmpty
	}
	return token, nil
}

// ErrPlaybackTokenEmpty is returned when Twitch responds 2xx with
// an empty value/signature — observed on some subscriber-only
// streams when no entitlement header is attached. The orchestrator
// should treat this as unrecoverable for this job; a fresh Device-Id
// or integrity token doesn't change the outcome.
var ErrPlaybackTokenEmpty = errors.New("twitch: empty playback token")

// playbackAttempt performs one request. integrity is the
// Client-Integrity header value (empty on the first attempt); the
// Device-Id header is always sent so Twitch can correlate if we do
// need to acquire integrity.
func (c *Client) playbackAttempt(ctx context.Context, login, accessToken, integrity string) (PlaybackToken, error) {
	body := gqlPersistedQuery{
		OperationName: playbackAccessTokenOp,
		Variables: map[string]any{
			"isLive":     true,
			"login":      login,
			"isVod":      false,
			"vodID":      "",
			"playerType": "site",
			"platform":   "web",
		},
		Extensions: gqlExtensions{
			PersistedQuery: gqlPersistedRef{
				Version:    1,
				SHA256Hash: playbackAccessTokenSHA256,
			},
		},
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return PlaybackToken{}, fmt.Errorf("encode gql body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.gqlURL, bytes.NewReader(buf))
	if err != nil {
		return PlaybackToken{}, fmt.Errorf("build gql request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Client-ID", c.clientID)
	req.Header.Set("Device-Id", c.deviceID)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Origin", playerOrigin)
	req.Header.Set("Referer", playerReferer)
	if accessToken != "" {
		req.Header.Set("Authorization", "OAuth "+accessToken)
	}
	if integrity != "" {
		req.Header.Set("Client-Integrity", integrity)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return PlaybackToken{}, fmt.Errorf("gql request: %w", err)
	}
	defer drainAndClose(resp)

	// Read the body once — callers that need to look at it for
	// error classification reuse the bytes rather than re-read.
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return PlaybackToken{}, fmt.Errorf("gql body read: %w", err)
	}

	// Twitch returns 200 even for application-level errors (GQL
	// convention), so status-code alone isn't authoritative.
	// Decode first; treat malformed JSON as transport-level
	// weirdness.
	var parsed gqlPlaybackResponse
	if decErr := json.Unmarshal(bodyBytes, &parsed); decErr != nil {
		if resp.StatusCode != http.StatusOK {
			return PlaybackToken{}, NewAuthError(resp.StatusCode, bodyBytes)
		}
		return PlaybackToken{}, fmt.Errorf("gql decode: %w", decErr)
	}

	if len(parsed.Errors) > 0 {
		msg := parsed.Errors[0].Message
		c.log.Debug("playback token gql error",
			"login", login,
			"status", resp.StatusCode,
			"message", msg,
			"integrity", integrity != "",
			"authenticated", accessToken != "",
			"body", string(bodyBytes))
		return PlaybackToken{}, &AuthError{
			Status:  resp.StatusCode,
			Code:    gqlMessageToCode(msg),
			Message: msg,
			Body:    bodyBytes,
			kind:    authErrorKindGQL,
		}
	}

	if resp.StatusCode != http.StatusOK {
		return PlaybackToken{}, NewAuthError(resp.StatusCode, bodyBytes)
	}

	if parsed.Data == nil || parsed.Data.StreamPlaybackAccessToken == nil {
		// 2xx + no data + no errors is Twitch's way of saying
		// "channel unknown" for some accounts. Surface as empty
		// to trigger the integrity fallback once, then fail.
		return PlaybackToken{}, nil
	}
	raw := parsed.Data.StreamPlaybackAccessToken
	return PlaybackToken{Value: raw.Value, Signature: raw.Signature}, nil
}
