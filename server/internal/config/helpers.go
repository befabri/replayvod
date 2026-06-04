package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

func (c *Config) GetAddress() string {
	return fmt.Sprintf("%s:%d", c.Env.Host, c.Env.Port)
}

func (c *Config) GetPostgresDSN() string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.Env.PostgresUser, c.Env.PostgresPassword),
		Host:   fmt.Sprintf("%s:%d", c.Env.PostgresHost, c.Env.PostgresPort),
		Path:   c.Env.PostgresDatabase,
	}
	q := u.Query()
	q.Set("sslmode", c.Env.PostgresSSLMode)
	u.RawQuery = q.Encode()
	return u.String()
}

func (c *Config) RedactedConfig() Config {
	redacted := *c
	redacted.Env.PostgresPassword = "[REDACTED]"
	redacted.Env.SessionSecret = "[REDACTED]"
	redacted.Env.TwitchSecret = "[REDACTED]"
	redacted.Env.HMACSecret = "[REDACTED]"
	redacted.Env.ServiceAccountOAuthToken = "[REDACTED]"
	redacted.Env.RelaySubscribeURL = "[REDACTED]"
	redacted.Env.RelayIngestURL = redactRelayURLToken(redacted.Env.RelayIngestURL)
	redacted.Env.WebhookCallbackURL = redactRelayURLToken(redacted.Env.WebhookCallbackURL)
	redacted.ServerMode.RelaySubscribeURL = "[REDACTED]"
	redacted.ServerMode.RelayIngestURL = redactRelayURLToken(redacted.ServerMode.RelayIngestURL)
	redacted.ServerMode.WebhookCallbackURL = redactRelayURLToken(redacted.ServerMode.WebhookCallbackURL)
	return redacted
}

func (c *Config) ServerModeCallbackURL() string {
	return c.ServerMode.CallbackURL()
}

func (c *Config) TrustedBrowserOrigins() []string {
	var origins []string
	add := func(raw string) {
		origin := parseOrigin(raw)
		if origin == "" {
			return
		}
		for _, existing := range origins {
			if existing == origin {
				return
			}
		}
		origins = append(origins, origin)
	}
	if c.App.Development {
		add(legacyDefaultFrontendURL)
	}
	add(c.Env.PublicBaseURL)
	for _, origin := range c.Env.TrustedOrigins {
		add(origin)
	}
	return origins
}

// PublicAPIBaseURL returns the scheme://host the API is reachable at from
// outside the process. An explicit PUBLIC_BASE_URL wins when set. Otherwise,
// direct EventSub mode uses its webhook callback origin, because that is the
// public URL Twitch already reaches for this server. The OAuth CallbackURL is the
// final fallback for the common single-origin login/dashboard/API deployment.
// Relay mode deliberately does not use the relay ingest origin: that host
// receives EventSub frames, but it is not the ReplayVOD API serving video bytes.
// Returns "" when none yields a usable scheme+host, in which case callers omit
// any absolute URL.
func (c *Config) PublicAPIBaseURL() string {
	if base := parseOrigin(c.Env.PublicBaseURL); base != "" {
		return base
	}
	if c.ServerMode.Mode == ServerModeDirect {
		if base := parseOrigin(c.ServerMode.WebhookCallbackURL); base != "" {
			return base
		}
	}
	return parseOrigin(c.Env.CallbackURL)
}

// parseOrigin returns the scheme://host of raw, or "" when raw is empty,
// unparseable, or missing a scheme or host.
func parseOrigin(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return canonicalOrigin(u.Scheme, u.Host)
}

func canonicalOrigin(scheme, hostport string) string {
	scheme = strings.ToLower(scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	u, err := url.Parse(scheme + "://" + hostport)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ""
	}
	port := u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return scheme + "://" + host
}

// OriginIsLoopback reports whether the host of origin (a scheme://host as
// returned by PublicAPIBaseURL) is loopback: the literal "localhost" or an IP in
// 127.0.0.0/8 or ::1. A signed part-download URL built on a loopback origin is
// useless to an external recording-webhook consumer, so main warns when signed
// downloads are enabled but the derived public origin is loopback — typically
// the localhost CallbackURL default an operator forgot to override with a real
// PUBLIC_BASE_URL. An empty or unparseable origin is not loopback (there is no
// link to mislead anyone with).
func OriginIsLoopback(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// SignedDownloadURLTTL is the maximum recording-webhook signed part-download URL
// lifetime from Download.SignedURLTTLHours. Per-recording retention can cap it
// further. A non-positive value yields a zero duration, which the URL signer
// reports as "not enabled".
func (c *Config) SignedDownloadURLTTL() time.Duration {
	h := c.App.Download.SignedURLTTLHours
	if h <= 0 {
		return 0
	}
	return time.Duration(h) * time.Hour
}

func redactRelayURLToken(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "u" || !isRelayToken(parts[1]) {
		return raw
	}
	parts[1] = "REDACTED"
	u.Path = "/" + strings.Join(parts, "/")
	u.RawPath = ""
	return u.String()
}

// isRelayToken mirrors TOKEN_PATTERN in relay/src/index.ts. Keep the two in
// sync — any URL whose /u/<token> segment passes this check is treated as a
// relay URL and has its token redacted in logs.
func isRelayToken(value string) bool {
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !isRelayTokenChar(r) {
			return false
		}
	}
	return true
}

// relayTokenAlphabet is the exact character set a relay token may contain:
// ASCII letters, digits, underscore, and hyphen.
const relayTokenAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"

func isRelayTokenChar(r rune) bool {
	return strings.ContainsRune(relayTokenAlphabet, r)
}
