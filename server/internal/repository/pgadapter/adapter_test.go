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

// TestSearchChannels mirrors the SQLite test so the PG ILIKE-based
// path and the SQLite lower()+LIKE path are held to the same contract.
// Split into subtests to surface individual assertion failures clearly.
func TestSearchChannels(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	seed := []repository.Channel{
		{BroadcasterID: "1", BroadcasterLogin: "shroud", BroadcasterName: "shroud"},
		{BroadcasterID: "2", BroadcasterLogin: "shoal", BroadcasterName: "Shoal"},
		{BroadcasterID: "3", BroadcasterLogin: "ashotoftoast", BroadcasterName: "ashot"},
		{BroadcasterID: "4", BroadcasterLogin: "unrelated", BroadcasterName: "Elsewhere"},
		{BroadcasterID: "5", BroadcasterLogin: "percent_tester", BroadcasterName: "100% tester"},
	}
	for _, c := range seed {
		ch := c
		if _, err := a.UpsertChannel(ctx, &ch); err != nil {
			t.Fatalf("seed %s: %v", c.BroadcasterLogin, err)
		}
	}

	loginsOf := func(channels []repository.Channel) []string {
		out := make([]string, len(channels))
		for i, c := range channels {
			out[i] = c.BroadcasterLogin
		}
		return out
	}

	t.Run("prefix beats substring, alphabetical within prefix", func(t *testing.T) {
		got, err := a.SearchChannels(ctx, "sh", 10)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		want := []string{"shoal", "shroud", "ashotoftoast"}
		if len(got) != len(want) {
			t.Fatalf("row count: want %d (%v) got %d (%v)", len(want), want, len(got), loginsOf(got))
		}
		for i, w := range want {
			if got[i].BroadcasterLogin != w {
				t.Errorf("row %d: want %s got %s", i, w, got[i].BroadcasterLogin)
			}
		}
	})

	t.Run("exact login match ranks above prefix match", func(t *testing.T) {
		got, err := a.SearchChannels(ctx, "shroud", 10)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(got) == 0 || got[0].BroadcasterLogin != "shroud" {
			t.Errorf("exact match should rank first, got %v", loginsOf(got))
		}
	})

	t.Run("empty query returns all", func(t *testing.T) {
		got, err := a.SearchChannels(ctx, "", 10)
		if err != nil {
			t.Fatalf("search empty: %v", err)
		}
		if len(got) != len(seed) {
			t.Fatalf("want all %d seeded, got %d", len(seed), len(got))
		}
	})

	t.Run("limit caps result rows", func(t *testing.T) {
		got, err := a.SearchChannels(ctx, "", 2)
		if err != nil {
			t.Fatalf("search limit: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("limit=2 should return 2 rows, got %d", len(got))
		}
	})

	t.Run("query matches display name via ILIKE", func(t *testing.T) {
		// Name "100% tester" has "100" in it; login doesn't. Searching
		// "100" returns the row via the name branch.
		got, err := a.SearchChannels(ctx, "100", 10)
		if err != nil {
			t.Fatalf("search by name: %v", err)
		}
		if len(got) != 1 || got[0].BroadcasterID != "5" {
			t.Errorf("expected 1 row matched via display name, got %v", loginsOf(got))
		}
	})
}

// TestSearchCategories mirrors the SQLite test so the PG lower()+LIKE path
// and the SQLite unicode_lower()+LIKE path are held to the same contract.
// Split into subtests so an individual assertion failure doesn't
// mask later ones.
func TestSearchCategories(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	seed := []repository.Category{
		{ID: "1", Name: "Valorant"},
		{ID: "2", Name: "Valheim"},
		{ID: "3", Name: "The Legend of Valor"},
		{ID: "4", Name: "Celeste"},
		{ID: "5", Name: "Échecs"},
	}
	for _, c := range seed {
		cat := c
		if _, err := a.UpsertCategory(ctx, &cat); err != nil {
			t.Fatalf("seed %s: %v", c.Name, err)
		}
	}

	namesOf := func(cats []repository.Category) []string {
		out := make([]string, len(cats))
		for i, c := range cats {
			out[i] = c.Name
		}
		return out
	}

	t.Run("prefix beats substring, alphabetical within prefix", func(t *testing.T) {
		got, err := a.SearchCategories(ctx, "val", 10)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		want := []string{"Valheim", "Valorant", "The Legend of Valor"}
		if len(got) != len(want) {
			t.Fatalf("row count: want %d (%v) got %d (%v)", len(want), want, len(got), namesOf(got))
		}
		for i, w := range want {
			if got[i].Name != w {
				t.Errorf("row %d: want %s got %s", i, w, got[i].Name)
			}
		}
	})

	t.Run("exact name match ranks above prefix", func(t *testing.T) {
		got, err := a.SearchCategories(ctx, "Valorant", 10)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if len(got) == 0 || got[0].Name != "Valorant" {
			t.Errorf("exact match should rank first, got %v", namesOf(got))
		}
	})

	t.Run("empty query returns all up to limit", func(t *testing.T) {
		got, err := a.SearchCategories(ctx, "", 10)
		if err != nil {
			t.Fatalf("search empty: %v", err)
		}
		if len(got) != len(seed) {
			t.Fatalf("want %d rows, got %d", len(seed), len(got))
		}
	})

	t.Run("limit caps result rows", func(t *testing.T) {
		got, err := a.SearchCategories(ctx, "", 2)
		if err != nil {
			t.Fatalf("search limit: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("limit=2 should return 2 rows, got %d", len(got))
		}
	})

	t.Run("unicode case fold matches non ASCII names", func(t *testing.T) {
		got, err := a.SearchCategories(ctx, "é", 10)
		if err != nil {
			t.Fatalf("search unicode: %v", err)
		}
		if len(got) == 0 || got[0].Name != "Échecs" {
			t.Fatalf("unicode search = %v, want Échecs first", namesOf(got))
		}
	})
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
