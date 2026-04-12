package pgadapter

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/testdb"
)

func TestMain(m *testing.M) {
	os.Exit(testdb.SetupPG(m))
}

func newTestAdapter(t *testing.T) *PGAdapter {
	t.Helper()
	pool := testdb.NewPGPool(t)
	return New(pool)
}

// TestUser_Upsert_RoundTrip covers the primary auth path: OAuth callback
// upserts a Twitch user, later reads load the same fields. A second upsert
// should update mutable fields (DisplayName) but preserve CreatedAt so the
// "first-login wins" semantics aren't lost.
func TestUser_Upsert_RoundTrip(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	email := "test@example.com"
	profile := "https://example.com/pic.png"
	created, err := a.UpsertUser(ctx, &repository.User{
		ID:              "12345",
		Login:           "testuser",
		DisplayName:     "TestUser",
		Email:           &email,
		ProfileImageURL: &profile,
		Role:            "viewer",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if created.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set by the DB default")
	}

	got, err := a.GetUser(ctx, "12345")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Login != "testuser" {
		t.Errorf("Login: got %q", got.Login)
	}
	if got.Role != "viewer" {
		t.Errorf("Role: got %q", got.Role)
	}
	if got.Email == nil || *got.Email != email {
		t.Errorf("Email round-trip: %v", got.Email)
	}
	if got.ProfileImageURL == nil || *got.ProfileImageURL != profile {
		t.Errorf("ProfileImageURL round-trip: %v", got.ProfileImageURL)
	}

	// Re-upsert with a changed display name AND a different role. The query
	// deliberately excludes `role` from the ON CONFLICT UPDATE SET so that
	// a returning user keeps whatever role the admin assigned them — not
	// whatever Twitch-synced default the caller happens to pass. This test
	// is the regression gate: if someone adds `role = EXCLUDED.role` to the
	// upsert, a privilege downgrade (or escalation) ships silently.
	updated, err := a.UpsertUser(ctx, &repository.User{
		ID: "12345", Login: "testuser", DisplayName: "Renamed",
		Email: &email, ProfileImageURL: &profile, Role: "viewer",
	})
	if err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if updated.DisplayName != "Renamed" {
		t.Errorf("DisplayName not updated: got %q", updated.DisplayName)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("CreatedAt must be preserved across upsert: was %v, now %v",
			created.CreatedAt, updated.CreatedAt)
	}

	// Manually promote to admin via the dedicated mutation, then re-upsert
	// with the default "viewer" role — the promoted role must survive.
	if err := a.UpdateUserRole(ctx, "12345", "admin"); err != nil {
		t.Fatalf("update role: %v", err)
	}
	reUpsert, err := a.UpsertUser(ctx, &repository.User{
		ID: "12345", Login: "testuser", DisplayName: "Renamed",
		Email: &email, ProfileImageURL: &profile, Role: "viewer",
	})
	if err != nil {
		t.Fatalf("re-upsert after promote: %v", err)
	}
	if reUpsert.Role != "admin" {
		t.Errorf("upsert must not clobber role: want admin, got %q", reUpsert.Role)
	}
}

// TestSession_CreateRead_Timestamps exercises the TIMESTAMPTZ round-trip
// (ExpiresAt, LastActiveAt, CreatedAt) and the BYTEA encrypted_tokens column
// — both are easy to get wrong in the pgx adapter.
func TestSession_CreateRead_Timestamps(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	if _, err := a.UpsertUser(ctx, &repository.User{
		ID: "u1", Login: "u1", DisplayName: "u1", Role: "viewer",
	}); err != nil {
		t.Fatalf("upsert user: %v", err)
	}

	expiresAt := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Microsecond)
	ua := "test-user-agent"
	ip := "127.0.0.1"
	sess := &repository.Session{
		HashedID:        "abc123",
		UserID:          "u1",
		EncryptedTokens: []byte("encrypted-bytes"),
		ExpiresAt:       expiresAt,
		UserAgent:       &ua,
		IPAddress:       &ip,
	}
	if err := a.CreateSession(ctx, sess); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := a.GetSession(ctx, "abc123")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.ExpiresAt.Equal(expiresAt) {
		t.Errorf("ExpiresAt: want %v got %v", expiresAt, got.ExpiresAt)
	}
	if got.LastActiveAt.IsZero() {
		t.Error("LastActiveAt should be set by DB default")
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set by DB default")
	}
	if got.UserAgent == nil || *got.UserAgent != ua {
		t.Errorf("UserAgent: %v", got.UserAgent)
	}
	if got.IPAddress == nil || *got.IPAddress != ip {
		t.Errorf("IPAddress: %v", got.IPAddress)
	}
	if string(got.EncryptedTokens) != "encrypted-bytes" {
		t.Errorf("EncryptedTokens: got %q", string(got.EncryptedTokens))
	}
}

// TestWhitelist_AddIsIdempotent asserts ON CONFLICT DO NOTHING semantics —
// the bootstrap seed relies on this: it calls AddToWhitelist for OWNER_TWITCH_ID
// on every startup and must not error on repeat runs.
func TestWhitelist_AddIsIdempotent(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	if err := a.AddToWhitelist(ctx, "12345"); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := a.AddToWhitelist(ctx, "12345"); err != nil {
		t.Fatalf("second add must be idempotent, got: %v", err)
	}

	yes, err := a.IsWhitelisted(ctx, "12345")
	if err != nil {
		t.Fatalf("check present: %v", err)
	}
	if !yes {
		t.Error("expected whitelisted=true after add")
	}

	no, err := a.IsWhitelisted(ctx, "does-not-exist")
	if err != nil {
		t.Fatalf("check missing: %v", err)
	}
	if no {
		t.Error("expected whitelisted=false for unknown id")
	}
}

// TestPGAdapter_ErrNotFound asserts the pgx.ErrNoRows → repository.ErrNotFound
// translation. Services at the tRPC boundary do errors.Is(err, ErrNotFound)
// to map to 404s; without the translation they'd silently 500.
func TestPGAdapter_ErrNotFound(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	_, err := a.GetUser(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing user")
	}
	if !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("expected repository.ErrNotFound, got %v", err)
	}
}
