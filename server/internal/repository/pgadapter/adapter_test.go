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
