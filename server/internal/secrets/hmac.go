// Package secrets resolves process secrets the server provisions for itself.
// Today that is the EventSub HMAC secret: the value Twitch signs each EventSub
// delivery with and the recorder verifies against. The database is the source
// of truth. The secret is read from server_settings, and an empty slot is
// seeded once on first boot: from the HMAC_SECRET environment variable when set
// (so an install upgrading from the old env-only setup keeps the same secret
// and its subscriptions keep verifying), otherwise from a freshly generated
// value. After that first boot the env var is ignored, so it can be dropped
// from a deployment and removed entirely in a future release without breaking
// anything. The secret must stay stable across restarts: changing it would make
// Twitch's signatures fail verification until every subscription is recreated.
package secrets

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/befabri/replayvod/server/internal/config"
)

// Source records where ResolveHMAC obtained the secret, for logging.
type Source string

const (
	// FromDatabase means an existing stored secret was used (the steady state).
	FromDatabase Source = "database"
	// FromEnv means an empty slot was seeded from HMAC_SECRET (the upgrade path).
	FromEnv Source = "environment"
	// Generated means an empty slot was seeded with a fresh random secret.
	Generated Source = "generated"
)

// hmacStore is the slice of repository.Repository that ResolveHMAC needs. Kept
// narrow so the resolver is trivial to exercise with a fake.
type hmacStore interface {
	GetServerHMACSecret(ctx context.Context) (string, error)
	EnsureServerHMACSecret(ctx context.Context, secret string) error
}

// ResolveHMAC returns the EventSub HMAC secret, treating the database as the
// source of truth. A stored secret is returned unchanged. An empty slot is
// seeded once: from envSecret when set, otherwise from a fresh random value.
// The seed is persisted with a compare-and-swap, so concurrent boots converge
// on a single value.
func ResolveHMAC(ctx context.Context, store hmacStore, envSecret string) (secret string, source Source, err error) {
	stored, err := store.GetServerHMACSecret(ctx)
	if err != nil {
		return "", "", fmt.Errorf("read stored hmac secret: %w", err)
	}
	if stored != "" {
		return stored, FromDatabase, nil
	}

	seed := strings.TrimSpace(envSecret)
	source = FromEnv
	if seed == "" {
		seed, err = generate()
		if err != nil {
			return "", "", err
		}
		source = Generated
	} else if !config.ValidHMACSecret(seed) {
		// Reject a bad seed before persisting it; otherwise the invalid value
		// would latch into the database and fixing HMAC_SECRET would no longer
		// help (the env var only seeds an empty slot).
		return "", "", fmt.Errorf("HMAC_SECRET must be 10-100 ASCII characters (Twitch's EventSub rule); unset it to auto-generate one")
	}

	if err := store.EnsureServerHMACSecret(ctx, seed); err != nil {
		return "", "", fmt.Errorf("persist hmac secret: %w", err)
	}

	// Re-read: EnsureServerHMACSecret is a compare-and-swap, so a concurrent
	// boot may have stored a different value first. Everyone converges on it.
	stored, err = store.GetServerHMACSecret(ctx)
	if err != nil {
		return "", "", fmt.Errorf("read stored hmac secret: %w", err)
	}
	if stored == "" {
		return "", "", fmt.Errorf("hmac secret still empty after seeding")
	}
	if stored != seed {
		// Lost the compare-and-swap race; the winner's value is authoritative.
		source = FromDatabase
	}
	return stored, source, nil
}

// generate returns 32 random bytes hex-encoded (64 characters), comfortably
// inside Twitch's 10-100 character EventSub secret rule.
func generate() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate hmac secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}
