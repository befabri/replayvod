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

func TestServerSettings_RoundTrip(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	_, err := a.GetServerSettings(ctx)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("GetServerSettings before insert = %v, want ErrNotFound", err)
	}

	saved, err := a.UpsertServerSettings(ctx, &repository.ServerSettings{
		ServerMode:                    "relay",
		EventSubWebhookCallbackURL:    "https://replayvod.example/api/v1/webhook/callback",
		EventSubRelayIngestURL:        "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		EventSubRelaySubscribeURL:     "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
		EventSubRelayLocalCallbackURL: "http://127.0.0.1:8080/api/v1/webhook/callback",
	})
	if err != nil {
		t.Fatalf("UpsertServerSettings: %v", err)
	}
	if saved.ServerMode != "relay" {
		t.Fatalf("ServerMode = %q, want relay", saved.ServerMode)
	}
	if saved.EventSubWebhookCallbackURL != "https://replayvod.example/api/v1/webhook/callback" {
		t.Fatalf("EventSubWebhookCallbackURL = %q", saved.EventSubWebhookCallbackURL)
	}
	if saved.EventSubRelayIngestURL != "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA" {
		t.Fatalf("EventSubRelayIngestURL = %q", saved.EventSubRelayIngestURL)
	}
	if saved.EventSubRelaySubscribeURL != "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe" {
		t.Fatalf("EventSubRelaySubscribeURL = %q", saved.EventSubRelaySubscribeURL)
	}
	if saved.EventSubRelayLocalCallbackURL != "http://127.0.0.1:8080/api/v1/webhook/callback" {
		t.Fatalf("EventSubRelayLocalCallbackURL = %q", saved.EventSubRelayLocalCallbackURL)
	}
	if saved.CreatedAt.IsZero() || saved.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not populated: created=%v updated=%v", saved.CreatedAt, saved.UpdatedAt)
	}

	reloaded, err := a.GetServerSettings(ctx)
	if err != nil {
		t.Fatalf("GetServerSettings after insert: %v", err)
	}
	if reloaded.EventSubWebhookCallbackURL != saved.EventSubWebhookCallbackURL {
		t.Fatalf("reloaded EventSubWebhookCallbackURL = %q, want %q", reloaded.EventSubWebhookCallbackURL, saved.EventSubWebhookCallbackURL)
	}
	if reloaded.EventSubRelayIngestURL != saved.EventSubRelayIngestURL {
		t.Fatalf("reloaded EventSubRelayIngestURL = %q, want %q", reloaded.EventSubRelayIngestURL, saved.EventSubRelayIngestURL)
	}
	if reloaded.EventSubRelaySubscribeURL != saved.EventSubRelaySubscribeURL {
		t.Fatalf("reloaded EventSubRelaySubscribeURL = %q, want %q", reloaded.EventSubRelaySubscribeURL, saved.EventSubRelaySubscribeURL)
	}
	if reloaded.EventSubRelayLocalCallbackURL != saved.EventSubRelayLocalCallbackURL {
		t.Fatalf("reloaded EventSubRelayLocalCallbackURL = %q, want %q", reloaded.EventSubRelayLocalCallbackURL, saved.EventSubRelayLocalCallbackURL)
	}

	updated, err := a.UpsertServerSettings(ctx, &repository.ServerSettings{
		ServerMode:                 "direct",
		EventSubWebhookCallbackURL: "https://new.example/api/v1/webhook/callback",
	})
	if err != nil {
		t.Fatalf("second UpsertServerSettings: %v", err)
	}
	if updated.ServerMode != "direct" {
		t.Fatalf("updated ServerMode = %q, want direct", updated.ServerMode)
	}
	if updated.EventSubWebhookCallbackURL != "https://new.example/api/v1/webhook/callback" {
		t.Fatalf("updated EventSubWebhookCallbackURL = %q", updated.EventSubWebhookCallbackURL)
	}
	if updated.EventSubRelayIngestURL != "" {
		t.Fatalf("updated EventSubRelayIngestURL = %q, want empty", updated.EventSubRelayIngestURL)
	}
	if updated.EventSubRelaySubscribeURL != "" {
		t.Fatalf("updated EventSubRelaySubscribeURL = %q, want empty", updated.EventSubRelaySubscribeURL)
	}
	if updated.EventSubRelayLocalCallbackURL != "" {
		t.Fatalf("updated EventSubRelayLocalCallbackURL = %q, want empty", updated.EventSubRelayLocalCallbackURL)
	}

	var rowCount int
	if err := a.db.QueryRow(ctx, "SELECT COUNT(*) FROM server_settings").Scan(&rowCount); err != nil {
		t.Fatalf("count server_settings rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("server_settings row count = %d, want 1", rowCount)
	}
}

// TestServerSettings_UpsertPreservesCreatedAtAndAdvancesUpdatedAt pins the
// upsert's timestamp contract on Postgres: the UPDATE branch leaves created_at
// untouched and bumps updated_at to NOW(). Backdating the row first means we
// assert the move without a real-time sleep and without depending on clock
// resolution.
func TestServerSettings_UpsertPreservesCreatedAtAndAdvancesUpdatedAt(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	if _, err := a.UpsertServerSettings(ctx, &repository.ServerSettings{ServerMode: "poll"}); err != nil {
		t.Fatalf("seed upsert: %v", err)
	}
	old := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := a.db.Exec(ctx,
		"UPDATE server_settings SET created_at = $1, updated_at = $1 WHERE id = 1", old); err != nil {
		t.Fatalf("backdate timestamps: %v", err)
	}

	updated, err := a.UpsertServerSettings(ctx, &repository.ServerSettings{ServerMode: "off"})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if !updated.CreatedAt.Equal(old) {
		t.Fatalf("created_at = %v, want preserved at %v across upsert", updated.CreatedAt, old)
	}
	if !updated.UpdatedAt.After(old) {
		t.Fatalf("updated_at = %v, want advanced past %v on upsert", updated.UpdatedAt, old)
	}
}

// TestUpsertChannel_ViewCountExceedsInt32 is the regression guard for the
// view_count int32 truncation: the largest Twitch channels report view counts
// above the signed-32-bit ceiling, and the old INTEGER column wrapped them.
// After widening to BIGINT the value must round-trip intact through both the
// upsert return and a fresh read.
func TestUpsertChannel_ViewCountExceedsInt32(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	const huge = int64(3_000_000_000) // > math.MaxInt32 (2_147_483_647)
	saved, err := a.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID:    "big-channel",
		BroadcasterLogin: "big",
		BroadcasterName:  "Big",
		ViewCount:        huge,
	})
	if err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	if saved.ViewCount != huge {
		t.Fatalf("upsert returned view_count = %d, want %d (truncated to int32?)", saved.ViewCount, huge)
	}

	got, err := a.GetChannel(ctx, "big-channel")
	if err != nil {
		t.Fatalf("GetChannel: %v", err)
	}
	if got.ViewCount != huge {
		t.Fatalf("persisted view_count = %d, want %d", got.ViewCount, huge)
	}
}
