package config

import (
	"fmt"
	"net/url"
	"strings"
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
	redacted.Env.WebhookCallbackURL = redactRelayURLToken(redacted.Env.WebhookCallbackURL)
	return redacted
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
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
