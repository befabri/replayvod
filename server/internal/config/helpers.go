package config

import (
	"fmt"
	"net/url"
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
	return redacted
}
