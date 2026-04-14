package twitch

import (
	"log/slog"
	"net/http"
	"time"
)

// Public Twitch web-client ID. Same one streamlink and yt-dlp use;
// Twitch has published it on every outbound web player for years.
// Hardcoded because it's not a secret — it appears in any
// view-source of the Twitch web player.
const DefaultClientID = "kimne78kx3ncx6brgo4mv6wki5h1ko"

// User-Agent streamlink settled on after Twitch started gating
// client-integrity on browser-looking UAs (streamlink issue #6574).
// If a future Twitch rollout needs a newer UA, update here; keeping
// the string in one place so we don't drift over time.
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

const (
	gqlURL       = "https://gql.twitch.tv/gql"
	integrityURL = "https://gql.twitch.tv/integrity"
	usherBaseURL = "https://usher.ttvnw.net"

	// playerOrigin + playerReferer match what the real Twitch web
	// client sends (captured 2026-04-13 via Playwright against
	// twitch.tv/tumblurr anonymously). The embed/player.twitch.tv
	// variant that streamlink uses is the stricter anti-abuse path:
	// anonymous requests claiming to be the embed player get
	// `{"errors":[{"message":"server error"}]}` without a real
	// browser-acquired integrity token. www.twitch.tv with
	// platform=web/playerType=site works without integrity.
	playerOrigin  = "https://www.twitch.tv"
	playerReferer = "https://www.twitch.tv/"
)

// Client is a Twitch streaming-side client: it does NOT talk to
// Helix. One instance is shared across all downloader jobs because
// the underlying http.Transport is expensive to construct and the
// integrity-token cache benefits from being shared too.
//
// The zero value is not usable; construct with New.
type Client struct {
	http *http.Client
	log  *slog.Logger

	// clientID is the GQL client identifier. Almost always
	// DefaultClientID; kept configurable for tests that stub the
	// GQL endpoint.
	clientID string

	// userAgent is sent on every outbound request. Twitch gates
	// some paths on UA; a matching value smooths over those.
	userAgent string

	// deviceID is a stable per-process identifier sent as
	// Device-Id on GQL + integrity requests. The actual value
	// doesn't matter as long as it's stable — Twitch correlates
	// integrity tokens against it.
	deviceID string

	// serviceAccountRefreshToken, if set, enables authenticated
	// playback (Turbo ad-skip + HEVC unlock per spec). Empty
	// means anonymous playback, which is the common case.
	serviceAccountRefreshToken string

	// integrity holds the current client-integrity token and its
	// expiry. Acquired on demand, cached in memory, refreshed
	// only on repeated auth failures. Never persisted to disk.
	integrity *integrityCache
}

// Config carries the tunables New needs. All fields are optional;
// zero values produce a working anonymous client.
type Config struct {
	// HTTPClient lets tests substitute an httptest.Server-backed
	// client. Nil uses a new http.Client with a 15s timeout —
	// appropriate for the small JSON + manifest payloads this
	// package deals with (NOT for segment downloads, which have
	// their own transport in internal/downloader/hls).
	HTTPClient *http.Client

	// ClientID overrides DefaultClientID. Leave empty outside of
	// tests.
	ClientID string

	// UserAgent overrides DefaultUserAgent. Leave empty outside
	// of tests.
	UserAgent string

	// DeviceID overrides the auto-generated device ID. Tests use
	// this to assert that request headers carry a stable value.
	DeviceID string

	// ServiceAccountRefreshToken enables authenticated playback
	// when non-empty. See Config.ServiceAccountOAuthToken in the
	// main config for operator docs.
	ServiceAccountRefreshToken string
}

// New creates a Twitch streaming-side client. The returned client
// is safe for concurrent use and expected to be shared across all
// downloader jobs in the process.
func New(cfg Config, log *slog.Logger) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	clientID := cfg.ClientID
	if clientID == "" {
		clientID = DefaultClientID
	}
	userAgent := cfg.UserAgent
	if userAgent == "" {
		userAgent = DefaultUserAgent
	}
	deviceID := cfg.DeviceID
	if deviceID == "" {
		deviceID = generateDeviceID()
	}
	return &Client{
		http:                       httpClient,
		log:                        log.With("domain", "twitch.stream"),
		clientID:                   clientID,
		userAgent:                  userAgent,
		deviceID:                   deviceID,
		serviceAccountRefreshToken: cfg.ServiceAccountRefreshToken,
		integrity:                  newIntegrityCache(),
	}
}
