package secrets

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
)

// casStore models the real EnsureServerHMACSecret: it stores a secret only
// while the slot is still empty (compare-and-swap).
type casStore struct {
	secret      string
	getErr      error
	ensureCalls int
}

func (s *casStore) GetServerHMACSecret(context.Context) (string, error) {
	if s.getErr != nil {
		return "", s.getErr
	}
	return s.secret, nil
}

func (s *casStore) EnsureServerHMACSecret(_ context.Context, secret string) error {
	s.ensureCalls++
	if s.secret == "" {
		s.secret = secret
	}
	return nil
}

func TestResolveHMAC_StoredWins(t *testing.T) {
	store := &casStore{secret: "stored"}
	secret, source, err := ResolveHMAC(context.Background(), store, "env-value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secret != "stored" || source != FromDatabase {
		t.Errorf("got (%q, %q), want (stored, database): a stored secret must win over env", secret, source)
	}
	if store.ensureCalls != 0 {
		t.Errorf("ensureCalls = %d, want 0", store.ensureCalls)
	}
}

func TestResolveHMAC_SeedsFromEnv(t *testing.T) {
	store := &casStore{}
	secret, source, err := ResolveHMAC(context.Background(), store, "env-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secret != "env-secret" || source != FromEnv {
		t.Errorf("got (%q, %q), want (env-secret, environment)", secret, source)
	}
	if store.secret != "env-secret" {
		t.Errorf("env value not persisted to the store: %q", store.secret)
	}
	if store.ensureCalls != 1 {
		t.Errorf("ensureCalls = %d, want 1", store.ensureCalls)
	}
}

func TestResolveHMAC_BlankEnvGenerates(t *testing.T) {
	store := &casStore{}
	secret, source, err := ResolveHMAC(context.Background(), store, "   ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if source != Generated {
		t.Errorf("source = %q, want generated (blank env should not seed)", source)
	}
	if secret != store.secret {
		t.Errorf("returned %q but persisted %q", secret, store.secret)
	}
	// 32 random bytes hex-encoded = 64 chars, inside Twitch's 10-100 rule.
	if len(secret) != 64 {
		t.Errorf("len(secret) = %d, want 64", len(secret))
	}
	if _, err := hex.DecodeString(secret); err != nil {
		t.Errorf("secret is not valid hex: %v", err)
	}
}

// When the compare-and-swap is lost to a concurrent boot, the persisted value
// differs from this process's seed; the re-read must return the winner and
// report it as coming from the database.
func TestResolveHMAC_ConcurrentWinnerWins(t *testing.T) {
	winner := &scriptedStore{gets: []string{"", "winner-secret"}}
	secret, source, err := ResolveHMAC(context.Background(), winner, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secret != "winner-secret" {
		t.Errorf("secret = %q, want winner-secret", secret)
	}
	if source != FromDatabase {
		t.Errorf("source = %q, want database when another boot won the CAS", source)
	}
}

func TestResolveHMAC_GetError(t *testing.T) {
	store := &casStore{getErr: errors.New("db down")}
	if _, _, err := ResolveHMAC(context.Background(), store, ""); err == nil {
		t.Fatal("expected error when the store read fails")
	}
}

func TestResolveHMAC_RejectsInvalidEnvSeed(t *testing.T) {
	store := &casStore{}
	if _, _, err := ResolveHMAC(context.Background(), store, "short"); err == nil {
		t.Fatal("expected error for an HMAC_SECRET seed outside Twitch's 10-100 ASCII rule")
	}
	if store.secret != "" {
		t.Errorf("invalid seed must not be persisted, got %q", store.secret)
	}
}

// scriptedStore returns a queued sequence of Get results and treats Ensure as a
// no-op, modeling a lost compare-and-swap.
type scriptedStore struct {
	gets []string
	i    int
}

func (s *scriptedStore) GetServerHMACSecret(context.Context) (string, error) {
	v := s.gets[s.i]
	if s.i < len(s.gets)-1 {
		s.i++
	}
	return v, nil
}

func (s *scriptedStore) EnsureServerHMACSecret(context.Context, string) error { return nil }
