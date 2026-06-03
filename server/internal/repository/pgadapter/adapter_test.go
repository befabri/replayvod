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

// TestListVideos_SortDimensions pins the CASE-based dynamic ORDER BY:
// every enum sort_key variant produces the expected ordering. Mirrors
// the SQLite equivalent so both adapters share an assertion contract.
func TestListVideos_SortDimensions(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	if _, err := a.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-1", BroadcasterLogin: "bc", BroadcasterName: "BC",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	//                           duration  size    display_name
	//   alpha   (a)             100        500    "Alpha"        oldest
	//   bravo   (b)             500       5000    "Bravo"        middle
	//   charlie (c)             300       1000    "Charlie"      newest
	type seed struct {
		jobID, displayName string
		duration           float64
		size               int64
	}
	seeds := []seed{
		{"j-a", "Alpha", 100, 500},
		{"j-b", "Bravo", 500, 5000},
		{"j-c", "Charlie", 300, 1000},
	}
	ids := make(map[string]int64, len(seeds))
	for _, s := range seeds {
		v, err := a.CreateVideo(ctx, &repository.VideoInput{
			JobID:         s.jobID,
			Filename:      s.jobID,
			DisplayName:   s.displayName,
			Status:        repository.VideoStatusDone,
			Quality:       repository.QualityHigh,
			BroadcasterID: "bc-1",
			ViewerCount:   0,
			Language:      "en",
			RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("create %s: %v", s.displayName, err)
		}
		if err := a.MarkVideoDone(ctx, v.ID, s.duration, s.size, nil, "complete", false); err != nil {
			t.Fatalf("mark done %s: %v", s.displayName, err)
		}
		ids[s.displayName] = v.ID
	}

	// Explicit timestamps so created_at sort direction is tested
	// directly, not the id-DESC tiebreaker behavior.
	base := time.Now().UTC().Truncate(time.Second)
	for i, name := range []string{"Alpha", "Bravo", "Charlie"} {
		if _, err := a.db.Exec(ctx,
			"UPDATE videos SET start_download_at = $1 WHERE id = $2",
			base.Add(time.Duration(i)*time.Minute),
			ids[name],
		); err != nil {
			t.Fatalf("override start_download_at for %s: %v", name, err)
		}
	}

	cases := []struct {
		name      string
		opts      repository.ListVideosOpts
		wantOrder []string
	}{
		{"default (empty sort/order) = created desc", repository.ListVideosOpts{Limit: 10}, []string{"Charlie", "Bravo", "Alpha"}},
		{"duration desc", repository.ListVideosOpts{Sort: "duration", Order: "desc", Limit: 10}, []string{"Bravo", "Charlie", "Alpha"}},
		{"duration asc", repository.ListVideosOpts{Sort: "duration", Order: "asc", Limit: 10}, []string{"Alpha", "Charlie", "Bravo"}},
		{"size desc", repository.ListVideosOpts{Sort: "size", Order: "desc", Limit: 10}, []string{"Bravo", "Charlie", "Alpha"}},
		{"size asc", repository.ListVideosOpts{Sort: "size", Order: "asc", Limit: 10}, []string{"Alpha", "Charlie", "Bravo"}},
		{"channel asc", repository.ListVideosOpts{Sort: "channel", Order: "asc", Limit: 10}, []string{"Alpha", "Bravo", "Charlie"}},
		{"channel desc", repository.ListVideosOpts{Sort: "channel", Order: "desc", Limit: 10}, []string{"Charlie", "Bravo", "Alpha"}},
		{"created_at asc", repository.ListVideosOpts{Sort: "created_at", Order: "asc", Limit: 10}, []string{"Alpha", "Bravo", "Charlie"}},
		{"created_at desc = default", repository.ListVideosOpts{Sort: "created_at", Order: "desc", Limit: 10}, []string{"Charlie", "Bravo", "Alpha"}},
		{"status filter narrows result", repository.ListVideosOpts{Status: "DONE", Limit: 10}, []string{"Charlie", "Bravo", "Alpha"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := a.ListVideos(ctx, tc.opts)
			if err != nil {
				t.Fatalf("ListVideos: %v", err)
			}
			if len(got) != len(tc.wantOrder) {
				t.Fatalf("row count: want %d got %d", len(tc.wantOrder), len(got))
			}
			for i, want := range tc.wantOrder {
				if got[i].DisplayName != want {
					t.Errorf("row %d: want %s got %s", i, want, got[i].DisplayName)
				}
			}
		})
	}
}

func TestListChannelsPage_CursorPagination(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	channels := []repository.Channel{
		{BroadcasterID: "1", BroadcasterLogin: "alpha", BroadcasterName: "Alpha"},
		{BroadcasterID: "2", BroadcasterLogin: "bravo", BroadcasterName: "Bravo"},
		{BroadcasterID: "3", BroadcasterLogin: "bravo-alt", BroadcasterName: "Bravo"},
		{BroadcasterID: "4", BroadcasterLogin: "charlie", BroadcasterName: "Charlie"},
	}
	for _, c := range channels {
		ch := c
		if _, err := a.UpsertChannel(ctx, &ch); err != nil {
			t.Fatalf("seed channel %s: %v", c.BroadcasterLogin, err)
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	for _, liveID := range []string{"1", "3"} {
		if _, err := a.UpsertStream(ctx, &repository.StreamInput{
			ID: liveID + "-live", BroadcasterID: liveID, Type: "live", Language: "en",
			ViewerCount: 1, StartedAt: now,
		}); err != nil {
			t.Fatalf("seed live stream %s: %v", liveID, err)
		}
	}

	cases := []struct {
		name     string
		sort     string
		liveOnly bool
		want     []string
	}{
		{"name asc", "name_asc", false, []string{"alpha", "bravo", "bravo-alt", "charlie"}},
		{"name desc", "name_desc", false, []string{"charlie", "bravo-alt", "bravo", "alpha"}},
		{"live only", "name_asc", true, []string{"alpha", "bravo-alt"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collectChannelPageLogins(t, ctx, a, 2, tc.sort, tc.liveOnly)
			assertStringSlice(t, got, tc.want)
		})
	}
}

func TestListVideosPage_CursorPagination(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	seedVideoListPageFixture(t, ctx, a)
	durationMin := 250.0
	sizeMin := int64(2500)

	cases := []struct {
		name string
		opts repository.ListVideosOpts
		want []string
	}{
		{"created desc", repository.ListVideosOpts{Sort: "created_at", Order: "desc", Limit: 2}, []string{"job-d2", "job-d1", "job-c", "job-b", "job-a"}},
		{"created asc", repository.ListVideosOpts{Sort: "created_at", Order: "asc", Limit: 2}, []string{"job-a", "job-b", "job-c", "job-d1", "job-d2"}},
		{"channel asc", repository.ListVideosOpts{Sort: "channel", Order: "asc", Limit: 2}, []string{"job-a", "job-b", "job-c", "job-d1", "job-d2"}},
		{"channel desc", repository.ListVideosOpts{Sort: "channel", Order: "desc", Limit: 2}, []string{"job-d2", "job-d1", "job-c", "job-b", "job-a"}},
		{"duration desc", repository.ListVideosOpts{Sort: "duration", Order: "desc", Limit: 2}, []string{"job-b", "job-c", "job-d2", "job-d1", "job-a"}},
		{"duration asc", repository.ListVideosOpts{Sort: "duration", Order: "asc", Limit: 2}, []string{"job-a", "job-d1", "job-d2", "job-c", "job-b"}},
		{"size desc", repository.ListVideosOpts{Sort: "size", Order: "desc", Limit: 2}, []string{"job-b", "job-c", "job-d2", "job-d1", "job-a"}},
		{"size asc", repository.ListVideosOpts{Sort: "size", Order: "asc", Limit: 2}, []string{"job-a", "job-d1", "job-d2", "job-c", "job-b"}},
		{"duration min filter", repository.ListVideosOpts{Sort: "created_at", Order: "desc", Limit: 2, DurationMinSeconds: &durationMin}, []string{"job-c", "job-b"}},
		{"size min filter", repository.ListVideosOpts{Sort: "created_at", Order: "desc", Limit: 2, SizeMinBytes: &sizeMin}, []string{"job-d2", "job-c", "job-b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collectVideoListPageJobIDs(t, ctx, a, tc.opts)
			assertStringSlice(t, got, tc.want)
		})
	}
}

func collectChannelPageLogins(t *testing.T, ctx context.Context, a *PGAdapter, limit int, sort string, liveOnly bool) []string {
	t.Helper()
	var cursor *repository.ChannelPageCursor
	out := []string{}
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("channel pagination did not terminate")
		}
		page, err := a.ListChannelsPage(ctx, limit, sort, liveOnly, cursor)
		if err != nil {
			t.Fatalf("ListChannelsPage: %v", err)
		}
		if len(page.Items) > limit {
			t.Fatalf("page size: got %d, limit %d", len(page.Items), limit)
		}
		for _, item := range page.Items {
			out = append(out, item.BroadcasterLogin)
		}
		if page.NextCursor == nil {
			return out
		}
		if len(page.Items) == 0 {
			t.Fatal("empty channel page returned a next cursor")
		}
		cursor = page.NextCursor
	}
}

func seedVideoListPageFixture(t *testing.T, ctx context.Context, a *PGAdapter) {
	t.Helper()
	if _, err := a.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-page", BroadcasterLogin: "page", BroadcasterName: "Page",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	base := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	seeds := []struct {
		jobID       string
		displayName string
		duration    float64
		size        int64
		startedAt   time.Time
	}{
		{"job-a", "Alpha", 100, 1000, base.Add(1 * time.Minute)},
		{"job-b", "Bravo", 400, 4000, base.Add(2 * time.Minute)},
		{"job-c", "Charlie", 300, 3000, base.Add(3 * time.Minute)},
		{"job-d1", "Delta", 200, 2000, base.Add(4 * time.Minute)},
		{"job-d2", "Delta", 200, 2500, base.Add(4 * time.Minute)},
	}
	for _, s := range seeds {
		v, err := a.CreateVideo(ctx, &repository.VideoInput{
			JobID:         s.jobID,
			Filename:      s.jobID,
			DisplayName:   s.displayName,
			Status:        repository.VideoStatusDone,
			Quality:       repository.QualityHigh,
			BroadcasterID: "bc-page",
			Language:      "en",
			RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("create %s: %v", s.jobID, err)
		}
		if err := a.MarkVideoDone(ctx, v.ID, s.duration, s.size, nil, repository.CompletionKindComplete, false); err != nil {
			t.Fatalf("mark done %s: %v", s.jobID, err)
		}
		if _, err := a.db.Exec(ctx, "UPDATE videos SET start_download_at = $1 WHERE id = $2", s.startedAt, v.ID); err != nil {
			t.Fatalf("override start_download_at %s: %v", s.jobID, err)
		}
	}
}

func TestListVideosPage_FiltersAndNullCursor(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	seedVideoListFilterFixture(t, ctx, a)
	qualityHigh := repository.QualityHigh

	cases := []struct {
		name string
		opts repository.ListVideosOpts
		want []string
	}{
		{
			"quality filter",
			repository.ListVideosOpts{Sort: "created_at", Order: "desc", Limit: 2, Quality: qualityHigh},
			[]string{"job-f-failed-b", "job-f-failed-a", "job-f-high-b", "job-f-high-a"},
		},
		{
			"broadcaster filter",
			repository.ListVideosOpts{Sort: "created_at", Order: "desc", Limit: 2, BroadcasterID: "bc-filter-a"},
			[]string{"job-f-failed-a", "job-f-low-a", "job-f-high-a"},
		},
		{
			"language filter",
			repository.ListVideosOpts{Sort: "created_at", Order: "desc", Limit: 2, Language: "fr"},
			[]string{"job-f-failed-b", "job-f-low-b", "job-f-high-b"},
		},
		{
			"duration desc crosses into NULL",
			repository.ListVideosOpts{Sort: "duration", Order: "desc", Limit: 2},
			[]string{"job-f-low-b", "job-f-high-b", "job-f-low-a", "job-f-high-a", "job-f-failed-b", "job-f-failed-a"},
		},
		{
			"size desc crosses into NULL",
			repository.ListVideosOpts{Sort: "size", Order: "desc", Limit: 2},
			[]string{"job-f-low-b", "job-f-high-b", "job-f-low-a", "job-f-high-a", "job-f-failed-b", "job-f-failed-a"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collectVideoListPageJobIDs(t, ctx, a, tc.opts)
			assertStringSlice(t, got, tc.want)
		})
	}
}

func seedVideoListFilterFixture(t *testing.T, ctx context.Context, a *PGAdapter) {
	t.Helper()
	for _, ch := range []struct{ id, login, name string }{
		{"bc-filter-a", "filter-a", "Filter A"},
		{"bc-filter-b", "filter-b", "Filter B"},
	} {
		if _, err := a.UpsertChannel(ctx, &repository.Channel{
			BroadcasterID: ch.id, BroadcasterLogin: ch.login, BroadcasterName: ch.name,
		}); err != nil {
			t.Fatalf("seed channel %s: %v", ch.id, err)
		}
	}

	base := time.Date(2026, 4, 23, 14, 0, 0, 0, time.UTC)
	type seed struct {
		jobID         string
		broadcasterID string
		quality       string
		language      string
		duration      float64
		size          int64
		minute        int
		failed        bool
	}
	seeds := []seed{
		{"job-f-high-a", "bc-filter-a", repository.QualityHigh, "en", 100, 1000, 1, false},
		{"job-f-high-b", "bc-filter-b", repository.QualityHigh, "fr", 400, 4000, 2, false},
		{"job-f-low-a", "bc-filter-a", repository.QualityLow, "en", 200, 2000, 3, false},
		{"job-f-low-b", "bc-filter-b", repository.QualityLow, "fr", 500, 5000, 4, false},
		{"job-f-failed-a", "bc-filter-a", repository.QualityHigh, "en", 0, 0, 5, true},
		{"job-f-failed-b", "bc-filter-b", repository.QualityHigh, "fr", 0, 0, 6, true},
	}
	for _, s := range seeds {
		v, err := a.CreateVideo(ctx, &repository.VideoInput{
			JobID:         s.jobID,
			Filename:      s.jobID,
			DisplayName:   s.jobID,
			Status:        repository.VideoStatusDone,
			Quality:       s.quality,
			BroadcasterID: s.broadcasterID,
			Language:      s.language,
			RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("create %s: %v", s.jobID, err)
		}
		if s.failed {
			if err := a.MarkVideoFailed(ctx, v.ID, "seed-failed", repository.CompletionKindComplete, true); err != nil {
				t.Fatalf("mark failed %s: %v", s.jobID, err)
			}
		} else {
			if err := a.MarkVideoDone(ctx, v.ID, s.duration, s.size, nil, repository.CompletionKindComplete, false); err != nil {
				t.Fatalf("mark done %s: %v", s.jobID, err)
			}
		}
		startedAt := base.Add(time.Duration(s.minute) * time.Minute)
		if _, err := a.db.Exec(ctx, "UPDATE videos SET start_download_at = $1 WHERE id = $2", startedAt, v.ID); err != nil {
			t.Fatalf("override start_download_at %s: %v", s.jobID, err)
		}
	}
}

func collectVideoListPageJobIDs(t *testing.T, ctx context.Context, a *PGAdapter, opts repository.ListVideosOpts) []string {
	t.Helper()
	var cursor *repository.VideoListPageCursor
	out := []string{}
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("video pagination did not terminate")
		}
		page, err := a.ListVideosPage(ctx, opts, cursor)
		if err != nil {
			t.Fatalf("ListVideosPage: %v", err)
		}
		if len(page.Items) > opts.Limit {
			t.Fatalf("page size: got %d, limit %d", len(page.Items), opts.Limit)
		}
		for _, item := range page.Items {
			out = append(out, item.JobID)
		}
		if page.NextCursor == nil {
			return out
		}
		if len(page.Items) == 0 {
			t.Fatal("empty video page returned a next cursor")
		}
		cursor = page.NextCursor
	}
}

func assertStringSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("row count: want %d (%v), got %d (%v)", len(want), want, len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d: want %s, got %s (all: %v)", i, want[i], got[i], got)
		}
	}
}

func TestVideoMetadataDurations_TracksHistoryAndPrimaryCategory(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	if _, err := a.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-meta", BroadcasterLogin: "meta", BroadcasterName: "Meta",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	for _, c := range []repository.Category{{ID: "cat-a", Name: "Alpha"}, {ID: "cat-b", Name: "Bravo"}} {
		cat := c
		if _, err := a.UpsertCategory(ctx, &cat); err != nil {
			t.Fatalf("seed category %s: %v", c.ID, err)
		}
	}
	video, err := a.CreateVideo(ctx, &repository.VideoInput{
		JobID:         "job-meta-1",
		Filename:      "meta-video",
		DisplayName:   "Meta",
		Title:         "Opening",
		Status:        repository.VideoStatusPending,
		Quality:       repository.QualityHigh,
		BroadcasterID: "bc-meta",
		Language:      "en",
	})
	if err != nil {
		t.Fatalf("create video: %v", err)
	}
	titleA, err := a.UpsertTitle(ctx, "Opening")
	if err != nil {
		t.Fatalf("title A: %v", err)
	}
	titleB, err := a.UpsertTitle(ctx, "Main Run")
	if err != nil {
		t.Fatalf("title B: %v", err)
	}
	if err := a.LinkVideoTitle(ctx, video.ID, titleA.ID); err != nil {
		t.Fatalf("link title A: %v", err)
	}
	if err := a.LinkVideoTitle(ctx, video.ID, titleB.ID); err != nil {
		t.Fatalf("link title B: %v", err)
	}
	if err := a.LinkVideoCategory(ctx, video.ID, "cat-a"); err != nil {
		t.Fatalf("link category A: %v", err)
	}
	if err := a.LinkVideoCategory(ctx, video.ID, "cat-b"); err != nil {
		t.Fatalf("link category B: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	at1 := now.Add(-5 * time.Minute)
	at2 := now.Add(-3 * time.Minute)
	at3 := now.Add(-1 * time.Minute)
	resumeAt := now.Add(30 * time.Second)
	endAt := resumeAt.Add(30 * time.Second)

	if err := a.UpsertVideoTitleSpan(ctx, video.ID, titleA.ID, at1); err != nil {
		t.Fatalf("span title A1: %v", err)
	}
	if err := a.UpsertVideoCategorySpan(ctx, video.ID, "cat-a", at1); err != nil {
		t.Fatalf("span category A1: %v", err)
	}
	if err := a.UpsertVideoTitleSpan(ctx, video.ID, titleB.ID, at2); err != nil {
		t.Fatalf("span title B: %v", err)
	}
	if err := a.UpsertVideoCategorySpan(ctx, video.ID, "cat-b", at2); err != nil {
		t.Fatalf("span category B: %v", err)
	}
	if err := a.UpsertVideoTitleSpan(ctx, video.ID, titleA.ID, at3); err != nil {
		t.Fatalf("span title A2: %v", err)
	}
	if err := a.UpsertVideoCategorySpan(ctx, video.ID, "cat-a", at3); err != nil {
		t.Fatalf("span category A2: %v", err)
	}
	if err := a.CloseOpenVideoMetadataSpans(ctx, video.ID, now); err != nil {
		t.Fatalf("close spans at now: %v", err)
	}
	if err := a.ResumeVideoMetadataSpans(ctx, video.ID, resumeAt); err != nil {
		t.Fatalf("resume spans: %v", err)
	}
	if err := a.CloseOpenVideoMetadataSpans(ctx, video.ID, endAt); err != nil {
		t.Fatalf("close resumed spans: %v", err)
	}

	titles, err := a.ListTitlesForVideo(ctx, video.ID)
	if err != nil {
		t.Fatalf("list titles: %v", err)
	}
	if len(titles) != 4 {
		t.Fatalf("want 4 title spans, got %d", len(titles))
	}
	if titles[0].Name != "Opening" || titles[1].Name != "Main Run" || titles[2].Name != "Opening" || titles[3].Name != "Opening" {
		t.Fatalf("unexpected title span order: %+v", titles)
	}
	if titles[0].DurationSeconds < 119 || titles[1].DurationSeconds < 119 || titles[2].DurationSeconds < 59 || titles[3].DurationSeconds < 29 {
		t.Fatalf("unexpected title durations: %+v", titles)
	}

	cats, err := a.ListCategoriesForVideo(ctx, video.ID)
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if len(cats) != 4 {
		t.Fatalf("want 4 category spans, got %d", len(cats))
	}
	if cats[0].Name != "Alpha" || cats[1].Name != "Bravo" || cats[2].Name != "Alpha" || cats[3].Name != "Alpha" {
		t.Fatalf("unexpected category span order: %+v", cats)
	}
	if cats[0].DurationSeconds < 119 || cats[1].DurationSeconds < 119 || cats[2].DurationSeconds < 59 || cats[3].DurationSeconds < 29 {
		t.Fatalf("unexpected category durations: %+v", cats)
	}
	primary, err := a.ListPrimaryCategoriesForVideos(ctx, []int64{video.ID})
	if err != nil {
		t.Fatalf("primary categories: %v", err)
	}
	if got, ok := primary[video.ID]; !ok || got.ID != "cat-a" {
		t.Fatalf("primary category = %+v, want cat-a", got)
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

// TestSearchCategories mirrors the SQLite test so the PG ILIKE path
// and the SQLite lower()+LIKE path are held to the same contract.
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
}

// TestUpsertCategory_PreservesBoxArt mirrors the SQLite case:
// COALESCE(EXCLUDED.*, categories.*) on box_art_url + igdb_id must
// keep existing values when the caller upserts with only (id, name).
func TestUpsertCategory_PreservesBoxArt(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	art := "https://cdn.example.com/art-{width}x{height}.jpg"
	igdb := "igdb-42"
	if _, err := a.UpsertCategory(ctx, &repository.Category{
		ID:        "g-1",
		Name:      "Old Name",
		BoxArtURL: &art,
		IGDBID:    &igdb,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := a.UpsertCategory(ctx, &repository.Category{
		ID: "g-1", Name: "New Name",
	}); err != nil {
		t.Fatalf("webhook-path upsert: %v", err)
	}

	got, err := a.GetCategory(ctx, "g-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "New Name" {
		t.Errorf("name should update: got %q", got.Name)
	}
	if got.BoxArtURL == nil || *got.BoxArtURL != art {
		t.Errorf("box_art_url was wiped: got %v, want %q", got.BoxArtURL, art)
	}
	if got.IGDBID == nil || *got.IGDBID != igdb {
		t.Errorf("igdb_id was wiped: got %v, want %q", got.IGDBID, igdb)
	}
}

func TestUpdateCategoryBoxArt(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	if _, err := a.UpsertCategory(ctx, &repository.Category{ID: "g-2", Name: "G"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	art := "https://cdn.example.com/g-2-{width}x{height}.jpg"
	if err := a.UpdateCategoryBoxArt(ctx, "g-2", art); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := a.GetCategory(ctx, "g-2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.BoxArtURL == nil || *got.BoxArtURL != art {
		t.Errorf("box_art_url: got %v, want %q", got.BoxArtURL, art)
	}
}

// TestListVideosByJobIDs pins the batched job-ID dereference the active-
// downloads snapshot uses in place of one GetVideoByJobID per running job.
// Subtests cover: matched+missing mix, nil-input no-op, duplicate IDs.
func TestListVideosByJobIDs(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	if _, err := a.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-1", BroadcasterLogin: "bc", BroadcasterName: "BC",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	for _, jobID := range []string{"job-a", "job-b", "job-c"} {
		if _, err := a.CreateVideo(ctx, &repository.VideoInput{
			JobID:         jobID,
			Filename:      jobID,
			DisplayName:   jobID,
			Status:        repository.VideoStatusDone,
			Quality:       repository.QualityHigh,
			BroadcasterID: "bc-1",
			Language:      "en",
			RecordingType: repository.RecordingTypeVideo,
		}); err != nil {
			t.Fatalf("seed %s: %v", jobID, err)
		}
	}

	t.Run("matched + missing job ids", func(t *testing.T) {
		got, err := a.ListVideosByJobIDs(ctx, []string{"job-a", "job-c", "job-missing"})
		if err != nil {
			t.Fatalf("by job ids: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2 matched rows, got %d", len(got))
		}
		gotJobs := map[string]bool{}
		for _, v := range got {
			gotJobs[v.JobID] = true
		}
		if !gotJobs["job-a"] || !gotJobs["job-c"] {
			t.Errorf("expected job-a and job-c, got %v", gotJobs)
		}
	})

	t.Run("nil job ids returns empty, no error", func(t *testing.T) {
		empty, err := a.ListVideosByJobIDs(ctx, nil)
		if err != nil {
			t.Fatalf("by nil ids: %v", err)
		}
		if len(empty) != 0 {
			t.Errorf("nil ids should return 0 rows, got %d", len(empty))
		}
	})

	t.Run("duplicate job ids collapse", func(t *testing.T) {
		got, err := a.ListVideosByJobIDs(ctx, []string{"job-a", "job-a", "job-b"})
		if err != nil {
			t.Fatalf("by dup ids: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("duplicates should collapse, got %d rows", len(got))
		}
	})
}

// TestListChannelsByIDs pins the batch-dereference primitive used by
// stream.followed to pre-filter Helix results against the local
// mirror. Subtests cover: matched+missing mix, nil-input no-op,
// duplicate IDs in input.
func TestListChannelsByIDs(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	for _, id := range []string{"1", "2", "3"} {
		if _, err := a.UpsertChannel(ctx, &repository.Channel{
			BroadcasterID: id, BroadcasterLogin: "l-" + id, BroadcasterName: "n-" + id,
		}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}

	t.Run("matched + missing ids", func(t *testing.T) {
		got, err := a.ListChannelsByIDs(ctx, []string{"1", "3", "missing"})
		if err != nil {
			t.Fatalf("by ids: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2 matched rows, got %d", len(got))
		}
	})

	t.Run("nil ids returns empty, no error", func(t *testing.T) {
		empty, err := a.ListChannelsByIDs(ctx, nil)
		if err != nil {
			t.Fatalf("by nil ids: %v", err)
		}
		if len(empty) != 0 {
			t.Errorf("nil ids should return 0 rows, got %d", len(empty))
		}
	})

	t.Run("duplicate ids deduped by set semantics", func(t *testing.T) {
		got, err := a.ListChannelsByIDs(ctx, []string{"1", "1", "2"})
		if err != nil {
			t.Fatalf("by dup ids: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("duplicates should collapse, got %d rows", len(got))
		}
	})
}

func TestListLatestLivePerChannel_OnePerBroadcaster(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	profile := "https://example.com/a.png"
	for _, c := range []repository.Channel{
		{BroadcasterID: "ch-a", BroadcasterLogin: "a", BroadcasterName: "A", ProfileImageURL: &profile},
		{BroadcasterID: "ch-b", BroadcasterLogin: "b", BroadcasterName: "B"},
	} {
		ch := c
		if _, err := a.UpsertChannel(ctx, &ch); err != nil {
			t.Fatalf("seed channel %s: %v", c.BroadcasterID, err)
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	streams := []struct {
		id, bc string
		offset time.Duration
	}{
		{"s-a-old", "ch-a", -4 * time.Hour},
		{"s-a-new", "ch-a", -30 * time.Minute},
		{"s-b-1", "ch-b", -2 * time.Hour},
	}
	for _, s := range streams {
		if _, err := a.UpsertStream(ctx, &repository.StreamInput{
			ID: s.id, BroadcasterID: s.bc, Type: "live", Language: "en",
			ViewerCount: 1, StartedAt: now.Add(s.offset),
		}); err != nil {
			t.Fatalf("seed stream %s: %v", s.id, err)
		}
	}

	got, err := a.ListLatestLivePerChannel(ctx, 10)
	if err != nil {
		t.Fatalf("latest live: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rows (one per broadcaster), got %d", len(got))
	}
	if got[0].ID != "s-a-new" || got[0].BroadcasterID != "ch-a" {
		t.Errorf("row 0: want s-a-new/ch-a, got %s/%s", got[0].ID, got[0].BroadcasterID)
	}
	if got[0].BroadcasterLogin != "a" || got[0].BroadcasterName != "A" {
		t.Errorf("row 0 display info: got login=%q name=%q", got[0].BroadcasterLogin, got[0].BroadcasterName)
	}
	if got[0].ProfileImageURL == nil || *got[0].ProfileImageURL != profile {
		t.Errorf("row 0 profile image: got %v", got[0].ProfileImageURL)
	}
	if got[1].ID != "s-b-1" || got[1].BroadcasterID != "ch-b" {
		t.Errorf("row 1: want s-b-1/ch-b, got %s/%s", got[1].ID, got[1].BroadcasterID)
	}
}

func TestServerHMACSecret_PreservedAcrossUpsert(t *testing.T) {
	ctx := context.Background()
	adapter := newTestAdapter(t)

	// A missing row reads as "" (not an error), which is what the resolver
	// keys on to decide it must seed.
	if got, err := adapter.GetServerHMACSecret(ctx); err != nil || got != "" {
		t.Fatalf("GetServerHMACSecret on empty = (%q, %v), want (\"\", nil)", got, err)
	}

	// Compare-and-swap seeds an empty slot, then refuses to overwrite it.
	if err := adapter.EnsureServerHMACSecret(ctx, "secret-one"); err != nil {
		t.Fatalf("EnsureServerHMACSecret: %v", err)
	}
	if err := adapter.EnsureServerHMACSecret(ctx, "secret-two"); err != nil {
		t.Fatalf("EnsureServerHMACSecret (second): %v", err)
	}
	if got, _ := adapter.GetServerHMACSecret(ctx); got != "secret-one" {
		t.Fatalf("hmac after second Ensure = %q, want secret-one (CAS must not overwrite)", got)
	}

	// Saving server settings from the owner UI must not wipe the secret.
	if _, err := adapter.UpsertServerSettings(ctx, &repository.ServerSettings{ServerMode: "poll"}); err != nil {
		t.Fatalf("UpsertServerSettings: %v", err)
	}
	if got, _ := adapter.GetServerHMACSecret(ctx); got != "secret-one" {
		t.Fatalf("hmac after UpsertServerSettings = %q, want secret-one (UI save must preserve it)", got)
	}
}

func TestRecordingWebhookConfig_RoundTrip(t *testing.T) {
	ctx := context.Background()
	adapter := newTestAdapter(t)

	saved, err := adapter.UpsertRecordingWebhookConfig(ctx, true,
		"https://hooks.example/recordings", "recording.completed,recording.failed")
	if err != nil {
		t.Fatalf("UpsertRecordingWebhookConfig: %v", err)
	}
	if !saved.RecordingWebhookEnabled {
		t.Fatal("enabled should round-trip as true")
	}
	if saved.RecordingWebhookURL != "https://hooks.example/recordings" {
		t.Fatalf("url = %q", saved.RecordingWebhookURL)
	}
	if saved.RecordingWebhookEvents != "recording.completed,recording.failed" {
		t.Fatalf("events = %q", saved.RecordingWebhookEvents)
	}
	// The config write never sets a secret — that is the secret methods' job.
	if saved.RecordingWebhookSecret != "" {
		t.Fatalf("config upsert must not set a secret, got %q", saved.RecordingWebhookSecret)
	}

	disabled, err := adapter.UpsertRecordingWebhookConfig(ctx, false, "", "")
	if err != nil {
		t.Fatalf("UpsertRecordingWebhookConfig (disable): %v", err)
	}
	if disabled.RecordingWebhookEnabled {
		t.Fatal("enabled should round-trip as false")
	}
}

// TestRecordingWebhookSecret_EnsureIsCASSetIsUnconditional pins the two secret
// writes: ensure seeds only an empty slot (so it never disturbs a live key),
// set rotates unconditionally, and a config save preserves whatever is stored.
func TestRecordingWebhookSecret_EnsureIsCASSetIsUnconditional(t *testing.T) {
	ctx := context.Background()
	adapter := newTestAdapter(t)

	if err := adapter.EnsureRecordingWebhookSecret(ctx, "first"); err != nil {
		t.Fatalf("EnsureRecordingWebhookSecret: %v", err)
	}
	if row, _ := adapter.GetServerSettings(ctx); row.RecordingWebhookSecret != "first" {
		t.Fatalf("ensure should seed an empty slot, got %q", row.RecordingWebhookSecret)
	}
	// CAS: a second ensure is a no-op while a secret exists.
	if err := adapter.EnsureRecordingWebhookSecret(ctx, "second"); err != nil {
		t.Fatalf("EnsureRecordingWebhookSecret (2): %v", err)
	}
	if row, _ := adapter.GetServerSettings(ctx); row.RecordingWebhookSecret != "first" {
		t.Fatalf("ensure must not overwrite an existing secret, got %q", row.RecordingWebhookSecret)
	}
	// Rotate is unconditional.
	if err := adapter.SetRecordingWebhookSecret(ctx, "rotated"); err != nil {
		t.Fatalf("SetRecordingWebhookSecret: %v", err)
	}
	if row, _ := adapter.GetServerSettings(ctx); row.RecordingWebhookSecret != "rotated" {
		t.Fatalf("set should rotate, got %q", row.RecordingWebhookSecret)
	}
	// A config save preserves the secret.
	if _, err := adapter.UpsertRecordingWebhookConfig(ctx, false, "", ""); err != nil {
		t.Fatalf("UpsertRecordingWebhookConfig: %v", err)
	}
	if row, _ := adapter.GetServerSettings(ctx); row.RecordingWebhookSecret != "rotated" {
		t.Fatalf("config save wiped the secret, got %q", row.RecordingWebhookSecret)
	}
}

// TestRecordingWebhookConfig_PreservedAcrossServerModeUpsert is the shared-row
// guarantee: the server-mode form, the webhook config, and the webhook secret
// write disjoint columns, so none clobbers the others (nor the HMAC secret).
func TestRecordingWebhookConfig_PreservedAcrossServerModeUpsert(t *testing.T) {
	ctx := context.Background()
	adapter := newTestAdapter(t)

	if err := adapter.EnsureServerHMACSecret(ctx, "hmac-keep"); err != nil {
		t.Fatalf("EnsureServerHMACSecret: %v", err)
	}
	if _, err := adapter.UpsertRecordingWebhookConfig(ctx, true, "https://hooks.example/x", "recording.failed"); err != nil {
		t.Fatalf("UpsertRecordingWebhookConfig: %v", err)
	}
	if err := adapter.EnsureRecordingWebhookSecret(ctx, "webhook-keep"); err != nil {
		t.Fatalf("EnsureRecordingWebhookSecret: %v", err)
	}

	if _, err := adapter.UpsertServerSettings(ctx, &repository.ServerSettings{ServerMode: "poll"}); err != nil {
		t.Fatalf("UpsertServerSettings: %v", err)
	}
	row, _ := adapter.GetServerSettings(ctx)
	if !row.RecordingWebhookEnabled || row.RecordingWebhookURL != "https://hooks.example/x" || row.RecordingWebhookSecret != "webhook-keep" {
		t.Fatalf("server-mode save clobbered webhook config: %+v", row)
	}

	if _, err := adapter.UpsertRecordingWebhookConfig(ctx, true, "https://hooks.example/y", ""); err != nil {
		t.Fatalf("UpsertRecordingWebhookConfig (2): %v", err)
	}
	row, _ = adapter.GetServerSettings(ctx)
	if row.ServerMode != "poll" {
		t.Fatalf("webhook save clobbered server_mode: %q", row.ServerMode)
	}
	if row.RecordingWebhookSecret != "webhook-keep" {
		t.Fatalf("webhook config save clobbered the webhook secret: %q", row.RecordingWebhookSecret)
	}
	if got, _ := adapter.GetServerHMACSecret(ctx); got != "hmac-keep" {
		t.Fatalf("webhook save clobbered hmac secret: %q", got)
	}
}

func TestRecordingWebhookDelivery_OutboxLifecycle(t *testing.T) {
	ctx := context.Background()
	adapter := newTestAdapter(t)
	now := time.Now().UTC().Truncate(time.Second)

	created, err := adapter.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
		MessageID:     "msg-1",
		DedupeKey:     "recording.completed:42",
		Event:         "recording.completed",
		VideoID:       42,
		NextAttemptAt: now,
	})
	if err != nil {
		t.Fatalf("CreateRecordingWebhookDelivery: %v", err)
	}
	if created.Status != repository.RecordingWebhookDeliveryPending {
		t.Fatalf("status = %q, want pending", created.Status)
	}

	claimed, err := adapter.ClaimDueRecordingWebhookDeliveries(ctx, now, 1)
	if err != nil {
		t.Fatalf("ClaimDueRecordingWebhookDeliveries: %v", err)
	}
	if len(claimed) != 1 || claimed[0].Attempts != 1 || claimed[0].Status != repository.RecordingWebhookDeliveryDelivering {
		t.Fatalf("unexpected claim: %+v", claimed)
	}

	next := now.Add(time.Minute)
	if err := adapter.MarkRecordingWebhookDeliveryFinal(ctx, created.ID, repository.RecordingWebhookDeliveryPending, 503, "HTTP 503 after 1 attempts", next, now); err != nil {
		t.Fatalf("MarkRecordingWebhookDeliveryFinal: %v", err)
	}
	claimed, err = adapter.ClaimDueRecordingWebhookDeliveries(ctx, now.Add(30*time.Second), 1)
	if err != nil {
		t.Fatalf("Claim before next due: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("delivery should not be due before backoff, got %+v", claimed)
	}

	claimed, err = adapter.ClaimDueRecordingWebhookDeliveries(ctx, next, 1)
	if err != nil {
		t.Fatalf("Claim after next due: %v", err)
	}
	if len(claimed) != 1 || claimed[0].Attempts != 2 {
		t.Fatalf("second claim should increment attempts, got %+v", claimed)
	}
	if err := adapter.MarkRecordingWebhookDeliveryDelivered(ctx, created.ID, 204, next); err != nil {
		t.Fatalf("MarkRecordingWebhookDeliveryDelivered: %v", err)
	}
	rows, err := adapter.ListRecordingWebhookDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecordingWebhookDeliveries: %v", err)
	}
	if len(rows) != 1 || rows[0].Status != repository.RecordingWebhookDeliveryDelivered || rows[0].LastStatus != 204 || rows[0].DeliveredAt == nil {
		t.Fatalf("unexpected final row: %+v", rows)
	}
}

// TestCreateClaimedRecordingWebhookDelivery_NotClaimable pins the SendTest
// double-delivery fix: a CreateClaimed row starts 'delivering' (one attempt) so
// the poller's pending-only claim never picks it up; a plain pending row does.
func TestCreateClaimedRecordingWebhookDelivery_NotClaimable(t *testing.T) {
	ctx := context.Background()
	adapter := newTestAdapter(t)
	now := time.Now().UTC().Truncate(time.Second)

	claimed, err := adapter.CreateClaimedRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
		MessageID: "test-msg", DedupeKey: "test:abc", Event: "recording.test", Test: true, NextAttemptAt: now,
	})
	if err != nil {
		t.Fatalf("CreateClaimedRecordingWebhookDelivery: %v", err)
	}
	if claimed.Status != repository.RecordingWebhookDeliveryDelivering || claimed.Attempts != 1 {
		t.Fatalf("claimed row = %+v, want status=delivering attempts=1", claimed)
	}
	got, err := adapter.ClaimDueRecordingWebhookDeliveries(ctx, now, 10)
	if err != nil {
		t.Fatalf("ClaimDueRecordingWebhookDeliveries: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a pre-claimed row must not be claimable by the poller, got %+v", got)
	}

	if _, err := adapter.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
		MessageID: "pending-msg", DedupeKey: "recording.completed:7", Event: "recording.completed", VideoID: 7, NextAttemptAt: now,
	}); err != nil {
		t.Fatalf("CreateRecordingWebhookDelivery: %v", err)
	}
	got, err = adapter.ClaimDueRecordingWebhookDeliveries(ctx, now, 10)
	if err != nil {
		t.Fatalf("ClaimDueRecordingWebhookDeliveries (2): %v", err)
	}
	if len(got) != 1 || got[0].VideoID != 7 {
		t.Fatalf("only the pending row should be claimable, got %+v", got)
	}
}

// TestDeleteOldRecordingWebhookDeliveries_PrunesTerminalKeepsActive pins the
// retention sweep: terminal rows whose latest terminal update is older than the
// cutoff are pruned; recent terminal outcomes plus pending/delivering rows are
// kept regardless of age.
func TestDeleteOldRecordingWebhookDeliveries_PrunesTerminalKeepsActive(t *testing.T) {
	ctx := context.Background()
	adapter := newTestAdapter(t)
	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-48 * time.Hour)
	cutoff := now.Add(-24 * time.Hour)

	mkPending := func(dk string, vid int64) *repository.RecordingWebhookDelivery {
		row, err := adapter.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
			MessageID: dk, DedupeKey: dk, Event: "recording.completed", VideoID: vid, NextAttemptAt: now,
		})
		if err != nil {
			t.Fatalf("create %s: %v", dk, err)
		}
		return row
	}

	d1 := mkPending("recording.completed:1", 1)
	if _, err := adapter.ClaimDueRecordingWebhookDeliveries(ctx, now, 1); err != nil {
		t.Fatalf("claim d1: %v", err)
	}
	if err := adapter.MarkRecordingWebhookDeliveryDelivered(ctx, d1.ID, 200, now); err != nil {
		t.Fatalf("mark delivered: %v", err)
	}
	if _, err := adapter.db.Exec(ctx,
		"UPDATE recording_webhook_deliveries SET created_at = $1, updated_at = $1, delivered_at = $1 WHERE id = $2",
		old, d1.ID); err != nil {
		t.Fatalf("backdate delivered row: %v", err)
	}
	d2 := mkPending("recording.completed:2", 2)
	if err := adapter.MarkRecordingWebhookDeliveryFinal(ctx, d2.ID, repository.RecordingWebhookDeliveryFailed, 500, "boom", now, now); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if _, err := adapter.db.Exec(ctx,
		"UPDATE recording_webhook_deliveries SET created_at = $1 WHERE id = $2",
		old, d2.ID); err != nil {
		t.Fatalf("backdate failed row created_at: %v", err)
	}
	pending := mkPending("recording.completed:3", 3)
	if _, err := adapter.db.Exec(ctx,
		"UPDATE recording_webhook_deliveries SET created_at = $1, updated_at = $1 WHERE id = $2",
		old, pending.ID); err != nil {
		t.Fatalf("backdate pending row: %v", err)
	}
	delivering, err := adapter.CreateClaimedRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
		MessageID: "test:x", DedupeKey: "test:x", Event: "recording.test", Test: true, NextAttemptAt: now,
	})
	if err != nil {
		t.Fatalf("create claimed: %v", err)
	}
	if _, err := adapter.db.Exec(ctx,
		"UPDATE recording_webhook_deliveries SET created_at = $1, updated_at = $1 WHERE id = $2",
		old, delivering.ID); err != nil {
		t.Fatalf("backdate delivering row: %v", err)
	}

	if err := adapter.DeleteOldRecordingWebhookDeliveries(ctx, cutoff); err != nil {
		t.Fatalf("DeleteOldRecordingWebhookDeliveries: %v", err)
	}
	rows, err := adapter.ListRecordingWebhookDeliveries(ctx, 50)
	if err != nil {
		t.Fatalf("ListRecordingWebhookDeliveries: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 surviving (recent terminal + pending + delivering), got %d: %+v", len(rows), rows)
	}
	seenRecentTerminal := false
	for _, r := range rows {
		if r.ID == d1.ID {
			t.Fatalf("old delivered row survived retention: %+v", r)
		}
		if r.ID == d2.ID {
			seenRecentTerminal = true
		}
		if r.Status != repository.RecordingWebhookDeliveryPending &&
			r.Status != repository.RecordingWebhookDeliveryDelivering &&
			r.ID != d2.ID {
			t.Fatalf("retention kept an unexpected terminal row or deleted an active one: %+v", r)
		}
	}
	if !seenRecentTerminal {
		t.Fatalf("recent terminal row was pruned even though updated_at is after cutoff")
	}
}

// TestRetryRecordingWebhookDelivery_OnlyFailedOrRejected pins the manual-retry
// constraint: only failed/rejected rows re-queue (attempts reset); a delivered
// or missing row yields ErrNotFound and is left untouched.
func TestRetryRecordingWebhookDelivery_OnlyFailedOrRejected(t *testing.T) {
	ctx := context.Background()
	adapter := newTestAdapter(t)
	now := time.Now().UTC().Truncate(time.Second)

	mk := func(dk string, vid int64) *repository.RecordingWebhookDelivery {
		row, err := adapter.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
			MessageID: dk, DedupeKey: dk, Event: "recording.completed", VideoID: vid, NextAttemptAt: now,
		})
		if err != nil {
			t.Fatalf("create %s: %v", dk, err)
		}
		return row
	}

	d1 := mk("recording.completed:1", 1)
	if _, err := adapter.ClaimDueRecordingWebhookDeliveries(ctx, now, 1); err != nil {
		t.Fatalf("claim d1: %v", err)
	}
	if err := adapter.MarkRecordingWebhookDeliveryDelivered(ctx, d1.ID, 200, now); err != nil {
		t.Fatalf("deliver d1: %v", err)
	}
	if _, err := adapter.RetryRecordingWebhookDelivery(ctx, d1.ID, now); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("retry of a delivered row = %v, want ErrNotFound", err)
	}

	d2 := mk("recording.completed:2", 2)
	if _, err := adapter.ClaimDueRecordingWebhookDeliveries(ctx, now, 1); err != nil {
		t.Fatalf("claim d2: %v", err)
	}
	if err := adapter.MarkRecordingWebhookDeliveryFinal(ctx, d2.ID, repository.RecordingWebhookDeliveryFailed, 500, "boom", now, now); err != nil {
		t.Fatalf("fail d2: %v", err)
	}
	retried, err := adapter.RetryRecordingWebhookDelivery(ctx, d2.ID, now)
	if err != nil {
		t.Fatalf("retry of a failed row: %v", err)
	}
	if retried.Status != repository.RecordingWebhookDeliveryPending || retried.Attempts != 0 || retried.LastStatus != 0 {
		t.Fatalf("retry should reset to pending/attempts=0/last_status=0, got %+v", retried)
	}

	if _, err := adapter.RetryRecordingWebhookDelivery(ctx, 999999, now); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("retry of a missing id = %v, want ErrNotFound", err)
	}
}

func TestMarkVideoDoneAndEnqueueRecordingWebhook_ConditionalAndDedupe(t *testing.T) {
	ctx := context.Background()
	adapter := newTestAdapter(t)
	if _, err := adapter.UpsertRecordingWebhookConfig(ctx, true, "https://hooks.example/x", "recording.completed"); err != nil {
		t.Fatalf("UpsertRecordingWebhookConfig: %v", err)
	}
	if err := adapter.EnsureRecordingWebhookSecret(ctx, "secret"); err != nil {
		t.Fatalf("EnsureRecordingWebhookSecret: %v", err)
	}
	video := createWebhookOutboxVideo(t, adapter, "job-webhook-done")
	input := &repository.RecordingWebhookDeliveryInput{
		MessageID:     "msg-terminal",
		DedupeKey:     "recording.completed:1",
		Event:         "recording.completed",
		VideoID:       video.ID,
		NextAttemptAt: time.Now().UTC(),
	}
	if err := adapter.MarkVideoDoneAndEnqueueRecordingWebhook(ctx, video.ID, 60, 1024, nil, repository.CompletionKindComplete, false, input); err != nil {
		t.Fatalf("MarkVideoDoneAndEnqueueRecordingWebhook: %v", err)
	}
	if err := adapter.MarkVideoDoneAndEnqueueRecordingWebhook(ctx, video.ID, 60, 1024, nil, repository.CompletionKindComplete, false, input); err != nil {
		t.Fatalf("MarkVideoDoneAndEnqueueRecordingWebhook duplicate: %v", err)
	}
	rows, err := adapter.ListRecordingWebhookDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecordingWebhookDeliveries: %v", err)
	}
	if len(rows) != 1 || rows[0].Event != "recording.completed" || rows[0].VideoID != video.ID {
		t.Fatalf("expected one deduped completed delivery, got %+v", rows)
	}

	failedVideo := createWebhookOutboxVideo(t, adapter, "job-webhook-failed")
	failedInput := &repository.RecordingWebhookDeliveryInput{
		MessageID:     "msg-failed",
		DedupeKey:     "recording.failed:2",
		Event:         "recording.failed",
		VideoID:       failedVideo.ID,
		NextAttemptAt: time.Now().UTC(),
	}
	if err := adapter.MarkVideoFailedAndEnqueueRecordingWebhook(ctx, failedVideo.ID, "boom", repository.CompletionKindComplete, true, failedInput); err != nil {
		t.Fatalf("MarkVideoFailedAndEnqueueRecordingWebhook: %v", err)
	}
	rows, err = adapter.ListRecordingWebhookDeliveries(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecordingWebhookDeliveries: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("failed event outside allowlist should not enqueue, got %+v", rows)
	}
}

func createWebhookOutboxVideo(t *testing.T, adapter *PGAdapter, jobID string) *repository.Video {
	t.Helper()
	if _, err := adapter.UpsertChannel(context.Background(), &repository.Channel{
		BroadcasterID:    "broadcaster",
		BroadcasterLogin: "streamer",
		BroadcasterName:  "Streamer",
	}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	v, err := adapter.CreateVideo(context.Background(), &repository.VideoInput{
		JobID:         jobID,
		Filename:      jobID + ".mp4",
		DisplayName:   "Streamer",
		Status:        repository.VideoStatusRunning,
		Quality:       repository.QualityHigh,
		BroadcasterID: "broadcaster",
		ViewerCount:   1,
		Language:      "en",
	})
	if err != nil {
		t.Fatalf("CreateVideo: %v", err)
	}
	return v
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

// TestResetStaleRecordingWebhookDeliveries is the Postgres counterpart to the
// SQLite crash-recovery guard: ResetStale must re-arm only rows that have been
// stuck in 'delivering' past the cutoff, and never disturb a fresh in-flight
// delivery or any terminal/pending row.
func TestResetStaleRecordingWebhookDeliveries(t *testing.T) {
	ctx := context.Background()

	t.Run("re-arms only stale delivering rows, leaves terminal and pending alone", func(t *testing.T) {
		a := newTestAdapter(t)

		stale, err := a.CreateClaimedRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
			MessageID: "stale", DedupeKey: "stale", Event: "recording.completed", VideoID: 1,
		})
		if err != nil {
			t.Fatalf("create stale delivering: %v", err)
		}
		if stale.Status != repository.RecordingWebhookDeliveryDelivering {
			t.Fatalf("precondition: stale row status = %q, want delivering", stale.Status)
		}

		pending, err := a.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
			MessageID: "pending", DedupeKey: "pending", Event: "recording.completed", VideoID: 2,
		})
		if err != nil {
			t.Fatalf("create pending: %v", err)
		}

		failed, err := a.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
			MessageID: "failed", DedupeKey: "failed", Event: "recording.completed", VideoID: 3,
		})
		if err != nil {
			t.Fatalf("create failed: %v", err)
		}
		fin := time.Now().UTC().Truncate(time.Second)
		if err := a.MarkRecordingWebhookDeliveryFinal(ctx, failed.ID, repository.RecordingWebhookDeliveryFailed, 500, "boom", fin, fin); err != nil {
			t.Fatalf("mark failed: %v", err)
		}

		delivered, err := a.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
			MessageID: "delivered", DedupeKey: "delivered", Event: "recording.completed", VideoID: 4, NextAttemptAt: fin,
		})
		if err != nil {
			t.Fatalf("create delivered: %v", err)
		}
		if _, err := a.ClaimDueRecordingWebhookDeliveries(ctx, fin, 1); err != nil {
			t.Fatalf("claim delivered: %v", err)
		}
		if err := a.MarkRecordingWebhookDeliveryDelivered(ctx, delivered.ID, 204, fin); err != nil {
			t.Fatalf("mark delivered: %v", err)
		}

		resetNow := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
		before := time.Now().UTC().Add(24 * time.Hour)
		if err := a.ResetStaleRecordingWebhookDeliveries(ctx, before, resetNow); err != nil {
			t.Fatalf("ResetStaleRecordingWebhookDeliveries: %v", err)
		}

		byID := pgDeliveriesByID(t, a, ctx)
		if got := byID[stale.ID].Status; got != repository.RecordingWebhookDeliveryPending {
			t.Fatalf("stale delivering row status = %q, want pending (re-armed)", got)
		}
		if got := byID[stale.ID].NextAttemptAt; !got.Equal(resetNow) {
			t.Fatalf("re-armed row next_attempt_at = %v, want %v (due immediately)", got, resetNow)
		}
		if got := byID[pending.ID].Status; got != repository.RecordingWebhookDeliveryPending {
			t.Fatalf("pending row status = %q, want pending (untouched)", got)
		}
		if got := byID[failed.ID].Status; got != repository.RecordingWebhookDeliveryFailed {
			t.Fatalf("failed row status = %q, want failed (untouched)", got)
		}
		if got := byID[delivered.ID].Status; got != repository.RecordingWebhookDeliveryDelivered {
			t.Fatalf("delivered row status = %q, want delivered (untouched)", got)
		}
	})

	t.Run("leaves fresh delivering rows untouched", func(t *testing.T) {
		a := newTestAdapter(t)
		fresh, err := a.CreateClaimedRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
			MessageID: "fresh", DedupeKey: "fresh", Event: "recording.completed", VideoID: 1,
		})
		if err != nil {
			t.Fatalf("create fresh delivering: %v", err)
		}
		before := time.Now().UTC().Add(-24 * time.Hour)
		if err := a.ResetStaleRecordingWebhookDeliveries(ctx, before, time.Now().UTC()); err != nil {
			t.Fatalf("ResetStaleRecordingWebhookDeliveries: %v", err)
		}
		if got := pgDeliveriesByID(t, a, ctx)[fresh.ID].Status; got != repository.RecordingWebhookDeliveryDelivering {
			t.Fatalf("fresh delivering row status = %q, want delivering (not yet stale)", got)
		}
	})
}

func pgDeliveriesByID(t *testing.T, a *PGAdapter, ctx context.Context) map[int64]repository.RecordingWebhookDelivery {
	t.Helper()
	rows, err := a.ListRecordingWebhookDeliveries(ctx, 100)
	if err != nil {
		t.Fatalf("ListRecordingWebhookDeliveries: %v", err)
	}
	out := make(map[int64]repository.RecordingWebhookDelivery, len(rows))
	for _, r := range rows {
		out[r.ID] = r
	}
	return out
}
