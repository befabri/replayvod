package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

const (
	ServerModeOff    = "off"
	ServerModePoll   = "poll"
	ServerModeDirect = "direct"
	ServerModeRelay  = "relay"

	ServerModeConfigSourceUnset = "unset"
	ServerModeConfigSourceEnv   = "env"
	ServerModeConfigSourceApp   = "app"

	// webhookCallbackPath is the fixed endpoint Twitch posts EventSub
	// notifications to (and the path the relay agent replays locally). Direct
	// mode's callback URL is this path under the server's public base origin.
	webhookCallbackPath = "/api/v1/webhook/callback"
)

// ServerModeConfig is the resolved server integration mode for this process.
// Env-managed config wins over app-managed config; an unset config is valid at
// boot and means the dashboard should show onboarding.
type ServerModeConfig struct {
	Source                string
	Mode                  string
	WebhookCallbackURL    string
	RelayIngestURL        string
	RelaySubscribeURL     string
	RelayLocalCallbackURL string
}

func ServerModeConfigFromEnv(env Environment) ServerModeConfig {
	if !env.ServerModeEnvConfigured {
		return ServerModeConfig{Source: ServerModeConfigSourceUnset}
	}
	// Env config is intentionally left un-cleared (relay URLs survive on a
	// direct config, etc.) so ValidateServerMode can reject an operator's
	// half-set env instead of silently dropping fields.
	cfg := ServerModeConfig{
		Source:                ServerModeConfigSourceEnv,
		Mode:                  env.ServerMode,
		WebhookCallbackURL:    env.WebhookCallbackURL,
		RelayIngestURL:        env.RelayIngestURL,
		RelaySubscribeURL:     env.RelaySubscribeURL,
		RelayLocalCallbackURL: env.RelayLocalCallbackURL,
	}
	cfg.Normalize()
	// Derive the relay subscribe URL from the relay URL when the operator omits
	// it (explicit RELAY_SUBSCRIBE_URL still wins). The direct callback is NOT
	// derived here: it needs the resolved public origin (PublicAPIBaseURL, which
	// also falls back to the OAuth callback origin), which only the full Config
	// knows, so that fill happens at boot/read via ResolveDerivedURLs.
	cfg.deriveRelaySubscribe()
	return cfg
}

func ServerModeConfigFromApp(mode, webhookCallbackURL, relayIngestURL, relaySubscribeURL, relayLocalCallbackURL string) ServerModeConfig {
	cfg := ServerModeConfig{
		Source:                ServerModeConfigSourceApp,
		Mode:                  mode,
		WebhookCallbackURL:    webhookCallbackURL,
		RelayIngestURL:        relayIngestURL,
		RelaySubscribeURL:     relaySubscribeURL,
		RelayLocalCallbackURL: relayLocalCallbackURL,
	}
	cfg.Normalize()
	// App config is canonical at the source: clear the URL fields the chosen
	// delivery does not use so storage, runtime, and API responses agree.
	cfg.ClearURLsForDelivery()
	// The relay subscribe URL is a pure function of the relay (ingest) URL, so
	// derive it here rather than asking the owner for it. The direct callback is
	// NOT derived here: it needs the public base URL, which this constructor
	// doesn't have, so that fill happens at the read/runtime boundary
	// (ResolveDerivedURLs) and is never persisted, staying correct if the public
	// base later changes.
	cfg.deriveRelaySubscribe()
	return cfg
}

// DeriveRelaySubscribeURL builds the wss subscribe URL the local relay agent
// dials from the public https relay (ingest) URL. The relay serves both off one
// token (POST /u/<token>, GET /u/<token>/subscribe), so the subscribe URL is
// fully determined by the ingest URL: same host and token, wss scheme, with a
// /subscribe suffix. Returns ok=false when ingestURL is not an
// https://<host>/u/<token> URL.
func DeriveRelaySubscribeURL(ingestURL string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(ingestURL))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", false
	}
	token, ok := relayIngestToken(u.Path)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("wss://%s/u/%s/subscribe", u.Host, token), true
}

func (c *ServerModeConfig) deriveRelaySubscribe() {
	if c.Mode != ServerModeRelay || c.RelaySubscribeURL != "" || c.RelayIngestURL == "" {
		return
	}
	if sub, ok := DeriveRelaySubscribeURL(c.RelayIngestURL); ok {
		c.RelaySubscribeURL = sub
	}
}

// ResolveDerivedURLs fills the URLs an operator no longer supplies: the relay
// subscribe URL from the relay (ingest) URL, and the direct webhook callback
// from the server's public origin (pass Config.PublicAPIBaseURL). Both fill only
// when blank, so an explicit value still wins, and it is idempotent. The direct
// callback is resolved at read/runtime (not stored), so it tracks the public
// origin.
func (c *ServerModeConfig) ResolveDerivedURLs(publicOriginURL string) {
	c.deriveRelaySubscribe()
	if c.Mode == ServerModeDirect && c.WebhookCallbackURL == "" {
		c.WebhookCallbackURL = PublicWebhookCallbackURL(publicOriginURL)
	}
}

// PublicWebhookCallbackURL is the callback URL direct mode uses: the fixed
// EventSub webhook path under the server's public origin. Callers pass the
// resolved public origin (Config.PublicAPIBaseURL, which falls back from
// PUBLIC_BASE_URL to the OAuth callback origin), so an operator who configured
// Twitch login already has a usable origin without setting PUBLIC_BASE_URL.
// Returns "" when the origin would not yield a Twitch-reachable callback (e.g. a
// localhost-only dev server), which the dashboard surfaces as "set a public URL"
// and the validator rejects.
func PublicWebhookCallbackURL(publicOriginURL string) string {
	origin := parseOrigin(publicOriginURL)
	if origin == "" {
		return ""
	}
	candidate := origin + webhookCallbackPath
	if !IsUsableWebhookURL(candidate) {
		return ""
	}
	return candidate
}

func (c *ServerModeConfig) Normalize() {
	c.Source = strings.TrimSpace(c.Source)
	c.Mode = strings.ToLower(strings.TrimSpace(c.Mode))
	c.WebhookCallbackURL = strings.TrimSpace(c.WebhookCallbackURL)
	c.RelayIngestURL = strings.TrimSpace(c.RelayIngestURL)
	c.RelaySubscribeURL = strings.TrimSpace(c.RelaySubscribeURL)
	c.RelayLocalCallbackURL = strings.TrimSpace(c.RelayLocalCallbackURL)
	// Mode is the single source of truth for "configured": an empty mode is
	// the unset/onboarding state regardless of declared Source.
	if c.Mode == "" {
		c.Source = ServerModeConfigSourceUnset
	}
}

// SetupRequired reports whether server mode still needs configuring: no mode
// has been chosen by env or the owner dashboard.
func (c ServerModeConfig) SetupRequired() bool {
	return strings.TrimSpace(c.Mode) == ""
}

// EnvManaged reports whether environment variables own this config. When true,
// dashboard updates are rejected because env is authoritative.
func (c ServerModeConfig) EnvManaged() bool {
	return c.Source == ServerModeConfigSourceEnv
}

// ClearURLsForDelivery blanks the URL fields the chosen delivery mode does not
// use. It is the single definition of which URLs belong to which mode, so
// storage, runtime config, and API responses cannot disagree.
func (c *ServerModeConfig) ClearURLsForDelivery() {
	switch c.Mode {
	case "", ServerModeOff, ServerModePoll:
		c.WebhookCallbackURL = ""
		c.RelayIngestURL = ""
		c.RelaySubscribeURL = ""
		c.RelayLocalCallbackURL = ""
	case ServerModeDirect:
		c.RelayIngestURL = ""
		c.RelaySubscribeURL = ""
		c.RelayLocalCallbackURL = ""
	case ServerModeRelay:
		c.WebhookCallbackURL = ""
	}
}

func (c ServerModeConfig) CallbackURL() string {
	switch c.Mode {
	case ServerModeRelay:
		return c.RelayIngestURL
	case ServerModeDirect:
		return c.WebhookCallbackURL
	default:
		return ""
	}
}

func (c ServerModeConfig) CreatesTwitchSubscriptions() bool {
	return c.Mode == ServerModeDirect || c.Mode == ServerModeRelay
}

// ProcessesWebhookNotifications reports whether signed EventSub notifications
// should produce application side effects after the webhook handler audits
// them. Verification and revocation requests are still handled by the webhook
// endpoint even when this returns false.
func (c ServerModeConfig) ProcessesWebhookNotifications() bool {
	return c.Mode == ServerModeDirect || c.Mode == ServerModeRelay
}

func (c ServerModeConfig) UsesRelayAgent() bool {
	return c.Mode == ServerModeRelay
}

func (c ServerModeConfig) PollsHelix() bool {
	return c.Mode == ServerModePoll
}

func (c ServerModeConfig) TracksTitlesViaPoll() bool {
	return c.Mode == ServerModePoll
}

func (c ServerModeConfig) TracksTitlesViaWebhook() bool {
	return c.Mode == ServerModeDirect || c.Mode == ServerModeRelay
}

func (c ServerModeConfig) RelayLocalCallbackURLOrDefault(port int) string {
	if c.RelayLocalCallbackURL != "" {
		return c.RelayLocalCallbackURL
	}
	return fmt.Sprintf("http://127.0.0.1:%d/api/v1/webhook/callback", port)
}

func (c ServerModeConfig) RuntimeEqual(other ServerModeConfig) bool {
	c.Normalize()
	other.Normalize()
	return c.Mode == other.Mode &&
		c.WebhookCallbackURL == other.WebhookCallbackURL &&
		c.RelayIngestURL == other.RelayIngestURL &&
		c.RelaySubscribeURL == other.RelaySubscribeURL &&
		c.RelayLocalCallbackURL == other.RelayLocalCallbackURL
}

// ValidateServerMode checks a resolved config against the rules for its mode.
// Messages are deliberately field-neutral (no env-var names): the same
// function validates both env-managed config at boot and owner config from the
// dashboard, and the error text is surfaced to dashboard users.
func ValidateServerMode(cfg ServerModeConfig) error {
	cfg.Normalize()
	switch cfg.Mode {
	case "":
		return validateModeWithoutURLs(cfg, "callback and relay URLs require a server mode")
	case ServerModeOff, ServerModePoll:
		return validateModeWithoutURLs(cfg, fmt.Sprintf("server mode %s does not use any callback or relay URLs", cfg.Mode))
	case ServerModeDirect:
		return validateDirectMode(cfg)
	case ServerModeRelay:
		return validateRelayMode(cfg)
	default:
		return fmt.Errorf("server mode must be one of %q, %q, %q, or %q",
			ServerModeOff, ServerModePoll, ServerModeDirect, ServerModeRelay)
	}
}

// hasDeliveryURLs reports whether any callback or relay URL field is set. The
// no-URL modes (unset, off, poll) reject a config when this is true.
func (c ServerModeConfig) hasDeliveryURLs() bool {
	return c.WebhookCallbackURL != "" || c.RelayIngestURL != "" || c.RelaySubscribeURL != "" || c.RelayLocalCallbackURL != ""
}

// validateModeWithoutURLs rejects any callback or relay URL for the modes that
// use none; msg is the mode-specific rejection message.
func validateModeWithoutURLs(cfg ServerModeConfig, msg string) error {
	if cfg.hasDeliveryURLs() {
		return errors.New(msg)
	}
	return nil
}

func validateDirectMode(cfg ServerModeConfig) error {
	if cfg.RelayIngestURL != "" || cfg.RelaySubscribeURL != "" || cfg.RelayLocalCallbackURL != "" {
		return fmt.Errorf("direct mode does not use relay URLs")
	}
	if !IsUsableWebhookURL(cfg.WebhookCallbackURL) {
		return fmt.Errorf("direct mode requires a public HTTPS callback URL on port 443")
	}
	return nil
}

func validateRelayMode(cfg ServerModeConfig) error {
	if cfg.WebhookCallbackURL != "" {
		return fmt.Errorf("relay mode does not use a webhook callback URL; it uses the relay URL")
	}
	if cfg.RelayIngestURL == "" {
		return fmt.Errorf("relay mode requires a relay URL")
	}
	// The subscribe URL is derived from the relay URL, and the single-field UI has
	// no subscribe input, so validate the relay URL itself: a malformed one must
	// report against the relay URL, not the derived (and therefore empty)
	// subscribe URL. A relay URL that passes the public-HTTPS check but yields no
	// subscribe URL has a bad /u/<token> path.
	if !IsUsableWebhookURL(cfg.RelayIngestURL) {
		return fmt.Errorf("relay URL must be a public HTTPS URL")
	}
	if cfg.RelaySubscribeURL == "" {
		return fmt.Errorf("relay URL must use the form https://<host>/u/<token>")
	}
	if err := ValidateRelayURLs(cfg.RelayIngestURL, cfg.RelaySubscribeURL); err != nil {
		return err
	}
	return validateRelayLocalCallbackURL(cfg.RelayLocalCallbackURL)
}

func ValidateServerModeHMACSecret(cfg ServerModeConfig, hmacSecret string) error {
	cfg.Normalize()
	if !cfg.ProcessesWebhookNotifications() {
		return nil
	}
	if !ValidHMACSecret(hmacSecret) {
		return fmt.Errorf("webhook delivery requires an EventSub HMAC secret between 10 and 100 ASCII characters")
	}
	return nil
}

// ValidHMACSecret reports whether s meets Twitch's EventSub secret rule:
// 10-100 ASCII characters.
func ValidHMACSecret(s string) bool {
	if len(s) < 10 || len(s) > 100 {
		return false
	}
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

func ValidateServerModeRuntimeConfig(cfg ServerModeConfig, hmacSecret string) error {
	if err := ValidateServerMode(cfg); err != nil {
		return err
	}
	return ValidateServerModeHMACSecret(cfg, hmacSecret)
}

// ValidateRelayURLs enforces the invariants that tie the optional Connect relay
// to the local server. Twitch posts to the ingest URL while the local agent
// dials the subscribe URL; both must address the same relay host and /u/<token>
// Durable Object or verification challenges will miss the subscriber. Relay
// mode requires public HTTPS ingest and wss:// subscribe.
func ValidateRelayURLs(ingestURL, subscribeURL string) error {
	if subscribeURL == "" {
		return nil
	}
	ingest, err := url.Parse(ingestURL)
	if err != nil {
		return fmt.Errorf("parse relay ingest URL: %w", err)
	}
	subscribe, err := url.Parse(subscribeURL)
	if err != nil {
		return fmt.Errorf("parse relay subscribe URL: %w", err)
	}
	if !IsUsableWebhookURL(ingestURL) {
		return fmt.Errorf("relay ingest URL must be a public HTTPS URL")
	}
	if subscribe.Scheme != "wss" {
		return fmt.Errorf("relay subscribe URL must use wss://")
	}
	return validateRelayURLPair(ingest, subscribe)
}

func validateRelayURLPair(ingest, subscribe *url.URL) error {
	if !strings.EqualFold(ingest.Host, subscribe.Host) {
		return fmt.Errorf("relay ingest and subscribe URLs must use the same relay host")
	}
	ingestToken, ok := relayIngestToken(ingest.Path)
	if !ok {
		return fmt.Errorf("relay ingest URL must use /u/<token>")
	}
	subscribeToken, ok := relaySubscribeToken(subscribe.Path)
	if !ok {
		return fmt.Errorf("relay subscribe URL must use /u/<token>/subscribe")
	}
	if ingestToken != subscribeToken {
		return fmt.Errorf("relay ingest and subscribe URLs must use the same relay token")
	}
	return nil
}

func validateRelayLocalCallbackURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := parseLocalCallbackURL(raw)
	if err != nil {
		return err
	}
	if !isHTTPScheme(u) {
		return fmt.Errorf("local callback URL must use http:// or https://")
	}
	if !isLoopbackHostname(u.Hostname()) {
		return fmt.Errorf("local callback URL must use a loopback host")
	}
	if u.Path != "/api/v1/webhook/callback" {
		return fmt.Errorf("local callback URL must use /api/v1/webhook/callback")
	}
	return nil
}

// parseLocalCallbackURL parses raw and requires a non-empty host, the first rule
// a relay local callback URL must satisfy.
func parseLocalCallbackURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("local callback URL must be a URL")
	}
	return u, nil
}

func isHTTPScheme(u *url.URL) bool {
	return u.Scheme == "http" || u.Scheme == "https"
}

func isLoopbackHostname(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func relayIngestToken(path string) (string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 || parts[0] != "u" || !isRelayToken(parts[1]) {
		return "", false
	}
	return parts[1], true
}

func relaySubscribeToken(path string) (string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 || parts[0] != "u" || parts[2] != "subscribe" || !isRelayToken(parts[1]) {
		return "", false
	}
	return parts[1], true
}

func URLHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

func SameURL(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	if errA != nil || errB != nil {
		return false
	}
	return strings.EqualFold(ua.Scheme, ub.Scheme) &&
		sameURLHost(ua, ub) &&
		ua.EscapedPath() == ub.EscapedPath() &&
		ua.Query().Encode() == ub.Query().Encode()
}

func sameURLHost(a, b *url.URL) bool {
	return canonicalURLHost(a) == canonicalURLHost(b)
}

func canonicalURLHost(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" || (strings.EqualFold(u.Scheme, "https") && port == "443") || (strings.EqualFold(u.Scheme, "http") && port == "80") {
		return host
	}
	return net.JoinHostPort(host, port)
}

// IsUsableWebhookURL is the canonical rule for whether Twitch's webhook
// transport will accept a callback URL (HTTPS, non-loopback host, standard
// port). Startup validation, API validation, and the runtime guard in
// service/eventsub (which aliases this) all defer to it so they cannot drift.
func IsUsableWebhookURL(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return false
	}
	if isLoopbackHostname(u.Hostname()) {
		return false
	}
	return !hasNonStandardPort(u)
}

// hasNonStandardPort reports whether u carries an explicit port other than the
// HTTPS default 443.
func hasNonStandardPort(u *url.URL) bool {
	return u.Port() != "" && u.Port() != "443"
}
