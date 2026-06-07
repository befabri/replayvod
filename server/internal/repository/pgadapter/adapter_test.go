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

// TestSetSchedulesPaused_RoundTripAndIsolation pins the global pause flag: it
// flips on a fresh row and reads back, and it is isolated from the other
// selective server-settings writes in both directions — pausing must not clobber
// the server mode, and an unrelated settings write must not clear the pause
// flag. That isolation is exactly what "resume restores prior state" relies on.
func TestSetSchedulesPaused_RoundTripAndIsolation(t *testing.T) {
	ctx := context.Background()
	adapter := newTestAdapter(t)

	// Fresh DB: the write creates the row and echoes the flag back.
	saved, err := adapter.SetSchedulesPaused(ctx, true)
	if err != nil {
		t.Fatalf("SetSchedulesPaused(true): %v", err)
	}
	if !saved.SchedulesPaused {
		t.Fatal("SetSchedulesPaused(true) returned SchedulesPaused=false")
	}
	if reloaded, err := adapter.GetServerSettings(ctx); err != nil {
		t.Fatalf("GetServerSettings: %v", err)
	} else if !reloaded.SchedulesPaused {
		t.Fatal("schedules_paused not persisted")
	}

	// Toggling back off works.
	if off, err := adapter.SetSchedulesPaused(ctx, false); err != nil {
		t.Fatalf("SetSchedulesPaused(false): %v", err)
	} else if off.SchedulesPaused {
		t.Fatal("SetSchedulesPaused(false) left the flag on")
	}

	// Isolation 1: pausing must not clobber an unrelated setting.
	if _, err := adapter.UpsertServerSettings(ctx, &repository.ServerSettings{ServerMode: "relay"}); err != nil {
		t.Fatalf("UpsertServerSettings: %v", err)
	}
	if _, err := adapter.SetSchedulesPaused(ctx, true); err != nil {
		t.Fatalf("SetSchedulesPaused after upsert: %v", err)
	}
	afterPause, err := adapter.GetServerSettings(ctx)
	if err != nil {
		t.Fatalf("GetServerSettings: %v", err)
	}
	if !afterPause.SchedulesPaused {
		t.Fatal("pause flag lost")
	}
	if afterPause.ServerMode != "relay" {
		t.Fatalf("ServerMode = %q, want relay (pause write clobbered it)", afterPause.ServerMode)
	}

	// Isolation 2: an unrelated settings write must leave the pause flag on.
	if _, err := adapter.UpsertServerSettings(ctx, &repository.ServerSettings{ServerMode: "direct"}); err != nil {
		t.Fatalf("second UpsertServerSettings: %v", err)
	}
	afterUpsert, err := adapter.GetServerSettings(ctx)
	if err != nil {
		t.Fatalf("GetServerSettings: %v", err)
	}
	if !afterUpsert.SchedulesPaused {
		t.Fatal("unrelated settings write clobbered schedules_paused")
	}
	if afterUpsert.ServerMode != "direct" {
		t.Fatalf("ServerMode = %q, want direct", afterUpsert.ServerMode)
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

func TestCreateVideo_NormalizesRecordingSettings(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	if _, err := a.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-audio", BroadcasterLogin: "audio", BroadcasterName: "Audio",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	got, err := a.CreateVideo(ctx, &repository.VideoInput{
		JobID:         "job-audio",
		Filename:      "audio",
		DisplayName:   "Audio",
		Status:        repository.VideoStatusPending,
		BroadcasterID: "bc-audio",
		Language:      "en",
		RecordingType: repository.RecordingTypeAudio,
		ForceH264:     true,
	})
	if err != nil {
		t.Fatalf("CreateVideo: %v", err)
	}
	if got.RecordingType != repository.RecordingTypeAudio {
		t.Fatalf("recording_type = %q, want audio", got.RecordingType)
	}
	if got.Quality != repository.QualityHigh {
		t.Fatalf("quality = %q, want HIGH default", got.Quality)
	}
	if got.ForceH264 {
		t.Fatalf("force_h264 = true for audio row, want false")
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
	if _, err := a.UpsertUser(ctx, &repository.User{
		ID: "user-channel-favorites", Login: "channel-favorites", DisplayName: "Channel Favorites", Role: "viewer",
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := a.UpsertUser(ctx, &repository.User{
		ID: "user-channel-other", Login: "channel-other", DisplayName: "Channel Other", Role: "viewer",
	}); err != nil {
		t.Fatalf("seed other user: %v", err)
	}

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

	seedVideo := func(jobID, broadcasterID string, failed, deleted bool) {
		t.Helper()
		v, err := a.CreateVideo(ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: jobID,
			Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
			BroadcasterID: broadcasterID, Language: "en",
			RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("seed video %s: %v", jobID, err)
		}
		if failed {
			if err := a.MarkVideoFailed(ctx, v.ID, "seed failed", repository.CompletionKindPartial, true); err != nil {
				t.Fatalf("mark failed %s: %v", jobID, err)
			}
			return
		}
		if err := a.MarkVideoDone(ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
			t.Fatalf("mark done %s: %v", jobID, err)
		}
		if deleted {
			if err := a.SoftDeleteVideo(ctx, v.ID, repository.DeletionKindManual); err != nil {
				t.Fatalf("soft delete %s: %v", jobID, err)
			}
		}
	}
	seedVideo("job-bravo", "2", false, false)
	seedVideo("job-bravo-alt-failed", "3", true, false)
	seedVideo("job-charlie-deleted", "4", false, true)
	if _, err := a.SetChannelFavorite(ctx, "user-channel-favorites", "1", true); err != nil {
		t.Fatalf("seed favorite alpha: %v", err)
	}
	if _, err := a.SetChannelFavorite(ctx, "user-channel-favorites", "3", true); err != nil {
		t.Fatalf("seed favorite bravo-alt: %v", err)
	}
	if _, err := a.SetChannelFavorite(ctx, "user-channel-other", "2", true); err != nil {
		t.Fatalf("seed other favorite bravo: %v", err)
	}

	cases := []struct {
		name   string
		sort   string
		filter string
		want   []string
	}{
		{"name asc", "name_asc", repository.ChannelFilterAll, []string{"alpha", "bravo", "bravo-alt", "charlie"}},
		{"name desc", "name_desc", repository.ChannelFilterAll, []string{"charlie", "bravo-alt", "bravo", "alpha"}},
		{"live only", "name_asc", repository.ChannelFilterLive, []string{"alpha", "bravo-alt"}},
		{"downloaded only", "name_asc", repository.ChannelFilterDownloaded, []string{"bravo"}},
		{"favorites only", "name_asc", repository.ChannelFilterFavorites, []string{"alpha", "bravo-alt"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collectChannelPageLogins(t, ctx, a, 2, tc.sort, tc.filter, "user-channel-favorites")
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

func TestListVideosByBroadcasterAndCategoryPage(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	seedVideoPageFixture(t, ctx, a)
	limit := 2

	cases := []struct {
		name  string
		fetch func(*repository.VideoPageCursor) (*repository.VideoPage, error)
		want  []string
	}{
		{
			name: "broadcaster filters, pages, and skips deleted",
			fetch: func(cursor *repository.VideoPageCursor) (*repository.VideoPage, error) {
				return a.ListVideosByBroadcaster(ctx, "bc-video-page-target", limit, cursor)
			},
			want: []string{"job-other-category", "job-new", "job-tie-high", "job-tie-low", "job-old"},
		},
		{
			name: "category filters, pages, and skips deleted",
			fetch: func(cursor *repository.VideoPageCursor) (*repository.VideoPage, error) {
				return a.ListVideosByCategory(ctx, "cat-video-page-target", limit, cursor)
			},
			want: []string{"job-other-broadcaster", "job-new", "job-tie-high", "job-tie-low", "job-old"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collectVideoPageJobIDs(t, limit, tc.fetch)
			assertStringSlice(t, got, tc.want)
		})
	}
}

func collectChannelPageLogins(t *testing.T, ctx context.Context, a *PGAdapter, limit int, sort string, filter string, userID string) []string {
	t.Helper()
	var cursor *repository.ChannelPageCursor
	out := []string{}
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("channel pagination did not terminate")
		}
		page, err := a.ListChannelsPage(ctx, limit, sort, filter, userID, cursor)
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

func seedCategoryPageFixture(t *testing.T, ctx context.Context, a *PGAdapter) {
	t.Helper()
	if _, err := a.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-category-page", BroadcasterLogin: "categorypage", BroadcasterName: "Category Page",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	for _, c := range []repository.Category{
		{ID: "cat-alpha-page", Name: "Alpha Game"},
		{ID: "cat-bravo-page", Name: "Bravo Game"},
		{ID: "cat-charlie-page", Name: "Charlie Game"},
		{ID: "cat-deleted-page", Name: "Deleted Game"},
	} {
		cat := c
		if _, err := a.UpsertCategory(ctx, &cat); err != nil {
			t.Fatalf("seed category %s: %v", cat.ID, err)
		}
	}

	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	seeds := []struct {
		jobID      string
		categoryID string
		startedAt  time.Time
		deleted    bool
	}{
		{"cat-page-alpha-1", "cat-alpha-page", base.Add(1 * time.Minute), false},
		{"cat-page-bravo-1", "cat-bravo-page", base.Add(2 * time.Minute), false},
		{"cat-page-bravo-2", "cat-bravo-page", base.Add(3 * time.Minute), false},
		{"cat-page-bravo-3", "cat-bravo-page", base.Add(4 * time.Minute), false},
		{"cat-page-charlie-1", "cat-charlie-page", base.Add(5 * time.Minute), false},
		{"cat-page-charlie-2", "cat-charlie-page", base.Add(6 * time.Minute), false},
		{"cat-page-deleted-1", "cat-deleted-page", base.Add(7 * time.Minute), true},
	}
	for _, seed := range seeds {
		video, err := a.CreateVideo(ctx, &repository.VideoInput{
			JobID:         seed.jobID,
			Filename:      seed.jobID,
			DisplayName:   "Category Page",
			Title:         seed.jobID,
			Status:        repository.VideoStatusDone,
			Quality:       repository.QualityHigh,
			BroadcasterID: "bc-category-page",
			Language:      "en",
			RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("create video %s: %v", seed.jobID, err)
		}
		if err := a.LinkVideoCategory(ctx, video.ID, seed.categoryID); err != nil {
			t.Fatalf("link category %s: %v", seed.jobID, err)
		}
		if _, err := a.db.Exec(ctx, "UPDATE videos SET start_download_at = $1 WHERE id = $2", seed.startedAt, video.ID); err != nil {
			t.Fatalf("override start_download_at %s: %v", seed.jobID, err)
		}
		if seed.deleted {
			if err := a.SoftDeleteVideo(ctx, video.ID, repository.DeletionKindManual); err != nil {
				t.Fatalf("soft delete %s: %v", seed.jobID, err)
			}
		}
	}
}

func collectCategoryPageNames(t *testing.T, ctx context.Context, a *PGAdapter, limit int, sort string) []string {
	t.Helper()
	var cursor *repository.CategoryPageCursor
	out := []string{}
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("category pagination did not terminate")
		}
		page, err := a.ListCategoriesWithVideosPage(ctx, limit, sort, cursor)
		if err != nil {
			t.Fatalf("ListCategoriesWithVideosPage: %v", err)
		}
		if len(page.Items) > limit {
			t.Fatalf("page size: got %d, limit %d", len(page.Items), limit)
		}
		for _, item := range page.Items {
			out = append(out, item.Name)
		}
		if page.NextCursor == nil {
			return out
		}
		if len(page.Items) == 0 {
			t.Fatal("empty category page returned a next cursor")
		}
		cursor = page.NextCursor
	}
}

func seedVideoPageFixture(t *testing.T, ctx context.Context, a *PGAdapter) {
	t.Helper()
	for _, ch := range []struct{ id, login, name string }{
		{"bc-video-page-target", "video-page-target", "Video Page Target"},
		{"bc-video-page-other", "video-page-other", "Video Page Other"},
	} {
		if _, err := a.UpsertChannel(ctx, &repository.Channel{
			BroadcasterID: ch.id, BroadcasterLogin: ch.login, BroadcasterName: ch.name,
		}); err != nil {
			t.Fatalf("seed channel %s: %v", ch.id, err)
		}
	}
	for _, cat := range []repository.Category{
		{ID: "cat-video-page-target", Name: "Video Page Target"},
		{ID: "cat-video-page-other", Name: "Video Page Other"},
	} {
		category := cat
		if _, err := a.UpsertCategory(ctx, &category); err != nil {
			t.Fatalf("seed category %s: %v", cat.ID, err)
		}
	}

	base := time.Date(2026, 4, 23, 16, 0, 0, 0, time.UTC)
	seeds := []struct {
		jobID         string
		broadcasterID string
		categoryID    string
		startedAt     time.Time
		deleted       bool
	}{
		{"job-old", "bc-video-page-target", "cat-video-page-target", base.Add(1 * time.Minute), false},
		{"job-tie-low", "bc-video-page-target", "cat-video-page-target", base.Add(2 * time.Minute), false},
		{"job-tie-high", "bc-video-page-target", "cat-video-page-target", base.Add(2 * time.Minute), false},
		{"job-new", "bc-video-page-target", "cat-video-page-target", base.Add(3 * time.Minute), false},
		{"job-other-broadcaster", "bc-video-page-other", "cat-video-page-target", base.Add(4 * time.Minute), false},
		{"job-other-category", "bc-video-page-target", "cat-video-page-other", base.Add(5 * time.Minute), false},
		{"job-deleted", "bc-video-page-target", "cat-video-page-target", base.Add(6 * time.Minute), true},
	}
	for _, s := range seeds {
		v, err := a.CreateVideo(ctx, &repository.VideoInput{
			JobID:         s.jobID,
			Filename:      s.jobID,
			DisplayName:   s.jobID,
			Title:         s.jobID,
			Status:        repository.VideoStatusDone,
			Quality:       repository.QualityHigh,
			BroadcasterID: s.broadcasterID,
			Language:      "en",
			RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("create %s: %v", s.jobID, err)
		}
		if err := a.LinkVideoCategory(ctx, v.ID, s.categoryID); err != nil {
			t.Fatalf("link category %s: %v", s.jobID, err)
		}
		if _, err := a.db.Exec(ctx, "UPDATE videos SET start_download_at = $1 WHERE id = $2", s.startedAt, v.ID); err != nil {
			t.Fatalf("override start_download_at %s: %v", s.jobID, err)
		}
		if s.deleted {
			if err := a.SoftDeleteVideo(ctx, v.ID, repository.DeletionKindManual); err != nil {
				t.Fatalf("soft delete %s: %v", s.jobID, err)
			}
		}
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

func collectVideoPageJobIDs(t *testing.T, limit int, fetch func(*repository.VideoPageCursor) (*repository.VideoPage, error)) []string {
	t.Helper()
	var cursor *repository.VideoPageCursor
	out := []string{}
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("video page pagination did not terminate")
		}
		page, err := fetch(cursor)
		if err != nil {
			t.Fatalf("fetch video page: %v", err)
		}
		if len(page.Items) > limit {
			t.Fatalf("page size: got %d, limit %d", len(page.Items), limit)
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

// TestListVideosPage_Scope pins the Postgres side of the removed-inclusive
// scope contract: default list pages stay active-only, "removed" returns only
// tombstones with their deletion_kind, and "all" returns both.
func TestListVideosPage_Scope(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	if _, err := a.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-scope", BroadcasterLogin: "scope", BroadcasterName: "Scope",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	base := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	mk := func(jobID string, startedAt time.Time) *repository.Video {
		v, err := a.CreateVideo(ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: "Scope", Status: repository.VideoStatusDone,
			Quality: repository.QualityHigh, BroadcasterID: "bc-scope", Language: "en",
		})
		if err != nil {
			t.Fatalf("create %s: %v", jobID, err)
		}
		if _, err := a.db.Exec(ctx,
			"UPDATE videos SET start_download_at = $1 WHERE id = $2",
			startedAt, v.ID,
		); err != nil {
			t.Fatalf("override start %s: %v", jobID, err)
		}
		return v
	}
	mk("job-scope-live", base)
	gone := mk("job-scope-gone", base.Add(time.Hour))
	if err := a.SoftDeleteVideo(ctx, gone.ID, repository.DeletionKindRetention); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	baseOpts := repository.ListVideosOpts{Sort: "created_at", Order: "desc", Limit: 10}
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, a, baseOpts), []string{"job-scope-live"})

	removed := baseOpts
	removed.Scope = "removed"
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, a, removed), []string{"job-scope-gone"})

	all := baseOpts
	all.Scope = "all"
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, a, all), []string{"job-scope-gone", "job-scope-live"})

	page, err := a.ListVideosPage(ctx, removed, nil)
	if err != nil {
		t.Fatalf("ListVideosPage removed: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].DeletionKind == nil ||
		*page.Items[0].DeletionKind != repository.DeletionKindRetention {
		t.Fatalf("removed row deletion_kind: got %+v, want %q", page.Items, repository.DeletionKindRetention)
	}
	if page.Items[0].DeletedAt == nil {
		t.Fatal("removed row deleted_at must be set")
	}
}

func TestVideoUserStateFiltersAndStatistics(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	userID := "user-video-state"
	if _, err := a.UpsertUser(ctx, &repository.User{
		ID: userID, Login: "state", DisplayName: "State", Role: "viewer",
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	otherUserID := "user-video-state-other"
	if _, err := a.UpsertUser(ctx, &repository.User{
		ID: otherUserID, Login: "state-other", DisplayName: "State Other", Role: "viewer",
	}); err != nil {
		t.Fatalf("seed other user: %v", err)
	}
	if _, err := a.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-state", BroadcasterLogin: "state", BroadcasterName: "State",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	mk := func(jobID, status string) *repository.Video {
		v, err := a.CreateVideo(ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: "State", Status: status,
			Quality: repository.QualityHigh, BroadcasterID: "bc-state", Language: "en",
			RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("create %s: %v", jobID, err)
		}
		return v
	}
	watched := mk("job-state-watched", repository.VideoStatusDone)
	later := mk("job-state-later", repository.VideoStatusDone)
	plain := mk("job-state-plain", repository.VideoStatusDone)
	running := mk("job-state-running", repository.VideoStatusRunning)
	failed := mk("job-state-failed", repository.VideoStatusFailed)

	if state, err := a.SetVideoWatchLater(ctx, userID, later.ID, true); err != nil {
		t.Fatalf("set watch later: %v", err)
	} else if !state.WatchLater {
		t.Fatal("watch later state not persisted")
	}
	if _, err := a.SetVideoWatchLater(ctx, userID, running.ID, true); err != nil {
		t.Fatalf("set running watch later: %v", err)
	}
	if _, err := a.SetVideoWatchLater(ctx, userID, failed.ID, true); err != nil {
		t.Fatalf("set failed watch later: %v", err)
	}
	if _, err := a.SetVideoWatchLater(ctx, otherUserID, watched.ID, true); err != nil {
		t.Fatalf("set other user watch later: %v", err)
	}
	if state, err := a.UpdateVideoWatchProgress(ctx, userID, watched.ID, 42.5, false, 1000); err != nil {
		t.Fatalf("update progress: %v", err)
	} else if state.WatchedAt == nil || state.LastPositionSeconds != 42.5 {
		t.Fatalf("watched state = %+v, want watched_at and 42.5s", state)
	}
	if state, err := a.UpdateVideoWatchProgress(ctx, userID, watched.ID, 60, true, 2000); err != nil {
		t.Fatalf("complete progress: %v", err)
	} else if state.CompletedAt == nil {
		t.Fatalf("completed state = %+v, want completed_at", state)
	}
	if _, err := a.UpdateVideoWatchProgress(ctx, userID, running.ID, 12, false, 3000); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("running progress err = %v, want ErrNotFound", err)
	}

	oldWatchedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, err := a.db.Exec(ctx, "UPDATE video_user_states SET watched_at = $1 WHERE user_id = $2 AND video_id = $3", oldWatchedAt, userID, watched.ID); err != nil {
		t.Fatalf("backdate watched_at: %v", err)
	}
	if state, err := a.UpdateVideoWatchProgress(ctx, userID, watched.ID, 90, false, 4000); err != nil {
		t.Fatalf("second progress: %v", err)
	} else if state.WatchedAt == nil || !state.WatchedAt.Equal(oldWatchedAt) {
		t.Fatalf("watched_at = %v, want preserved %v", state.WatchedAt, oldWatchedAt)
	} else if state.LastPositionSeconds != 90 {
		t.Fatalf("last_position_seconds = %v, want 90", state.LastPositionSeconds)
	}
	if state, err := a.UpdateVideoWatchProgress(ctx, userID, watched.ID, 1, false, 3500); err != nil {
		t.Fatalf("stale progress: %v", err)
	} else if state.LastPositionSeconds != 90 {
		t.Fatalf("stale progress rewound position to %v, want 90", state.LastPositionSeconds)
	} else if state.LastProgressAtMs == nil || *state.LastProgressAtMs != 4000 {
		t.Fatalf("stale progress watermark = %v, want 4000", state.LastProgressAtMs)
	}

	states, err := a.ListVideoUserStatesForVideos(ctx, userID, []int64{watched.ID, later.ID, plain.ID})
	if err != nil {
		t.Fatalf("list video user states: %v", err)
	}
	if len(states) != 2 {
		t.Fatalf("state count = %d, want 2; states=%+v", len(states), states)
	}

	baseOpts := repository.ListVideosOpts{UserID: userID, Sort: "created_at", Order: "desc", Limit: 10}
	watchLaterOpts := baseOpts
	watchLaterOpts.WatchLaterOnly = true
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, a, watchLaterOpts), []string{"job-state-failed", "job-state-running", "job-state-later"})

	unwatchedOpts := baseOpts
	unwatchedOpts.UnwatchedOnly = true
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, a, unwatchedOpts), []string{"job-state-plain", "job-state-later"})

	emptyUserUnwatchedOpts := unwatchedOpts
	emptyUserUnwatchedOpts.UserID = ""
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, a, emptyUserUnwatchedOpts), []string{})

	totals, err := a.VideoStatsTotals(ctx, userID)
	if err != nil {
		t.Fatalf("video stats totals: %v", err)
	}
	if totals.WatchLater != 3 || totals.Unwatched != 2 {
		t.Fatalf("stats watch_later/unwatched = %d/%d, want 3/2", totals.WatchLater, totals.Unwatched)
	}
	emptyTotals, err := a.VideoStatsTotals(ctx, "")
	if err != nil {
		t.Fatalf("empty-user video stats totals: %v", err)
	}
	if emptyTotals.WatchLater != 0 || emptyTotals.Unwatched != 0 {
		t.Fatalf("empty-user stats watch_later/unwatched = %d/%d, want 0/0", emptyTotals.WatchLater, emptyTotals.Unwatched)
	}
	otherTotals, err := a.VideoStatsTotals(ctx, otherUserID)
	if err != nil {
		t.Fatalf("other video stats totals: %v", err)
	}
	if otherTotals.WatchLater != 1 {
		t.Fatalf("other stats watch_later = %d, want 1", otherTotals.WatchLater)
	}
	otherWatchLaterOpts := baseOpts
	otherWatchLaterOpts.UserID = otherUserID
	otherWatchLaterOpts.WatchLaterOnly = true
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, a, otherWatchLaterOpts), []string{"job-state-watched"})

	if state, err := a.SetVideoWatchLater(ctx, userID, later.ID, false); err != nil {
		t.Fatalf("unset watch later: %v", err)
	} else if state.WatchLater {
		t.Fatal("watch later state stayed true after unset")
	}
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, a, watchLaterOpts), []string{"job-state-failed", "job-state-running"})
}

func TestListVideosPage_TerminalOnlyHistoryWhen(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	if _, err := a.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-history", BroadcasterLogin: "history", BroadcasterName: "History",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	mk := func(jobID, status string, offset time.Duration) *repository.Video {
		v, err := a.CreateVideo(ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: "History", Status: status,
			Quality: repository.QualityHigh, BroadcasterID: "bc-history", Language: "en",
		})
		if err != nil {
			t.Fatalf("create %s: %v", jobID, err)
		}
		if _, err := a.db.Exec(ctx,
			"UPDATE videos SET start_download_at = $1 WHERE id = $2",
			base.Add(offset), v.ID,
		); err != nil {
			t.Fatalf("override start %s: %v", jobID, err)
		}
		return v
	}
	done := mk("job-history-done", repository.VideoStatusDone, 0)
	failed := mk("job-history-failed", repository.VideoStatusFailed, time.Hour)
	removed := mk("job-history-removed", repository.VideoStatusDone, 2*time.Hour)
	removedRetention := mk("job-history-removed-retention", repository.VideoStatusDone, 150*time.Minute)
	mk("job-history-running", repository.VideoStatusRunning, 3*time.Hour)
	mk("job-history-pending", repository.VideoStatusPending, 4*time.Hour)

	downloadedDone := base.Add(48 * time.Hour)
	downloadedFailed := base.Add(24 * time.Hour)
	deletedRemoved := base.Add(72 * time.Hour)
	deletedRetention := base.Add(36 * time.Hour)
	if _, err := a.db.Exec(ctx,
		"UPDATE videos SET downloaded_at = $1 WHERE id = $2",
		downloadedDone, done.ID,
	); err != nil {
		t.Fatalf("set done downloaded_at: %v", err)
	}
	if _, err := a.db.Exec(ctx,
		"UPDATE videos SET downloaded_at = $1 WHERE id = $2",
		downloadedFailed, failed.ID,
	); err != nil {
		t.Fatalf("set failed downloaded_at: %v", err)
	}
	if err := a.SoftDeleteVideo(ctx, removed.ID, repository.DeletionKindManual); err != nil {
		t.Fatalf("soft delete removed: %v", err)
	}
	if err := a.SoftDeleteVideo(ctx, removedRetention.ID, repository.DeletionKindRetention); err != nil {
		t.Fatalf("soft delete retention removed: %v", err)
	}
	if _, err := a.db.Exec(ctx,
		"UPDATE videos SET deleted_at = $1 WHERE id = $2",
		deletedRemoved, removed.ID,
	); err != nil {
		t.Fatalf("set removed deleted_at: %v", err)
	}
	if _, err := a.db.Exec(ctx,
		"UPDATE videos SET deleted_at = $1 WHERE id = $2",
		deletedRetention, removedRetention.ID,
	); err != nil {
		t.Fatalf("set retention removed deleted_at: %v", err)
	}

	opts := repository.ListVideosOpts{
		Sort:         "history_when",
		Order:        "desc",
		Scope:        "all",
		TerminalOnly: true,
		Limit:        2,
	}
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, a, opts), []string{
		"job-history-removed",
		"job-history-done",
		"job-history-removed-retention",
		"job-history-failed",
	})

	removedOpts := opts
	removedOpts.Scope = "removed"
	removedOpts.Limit = 1
	assertStringSlice(t, collectVideoListPageJobIDs(t, ctx, a, removedOpts), []string{
		"job-history-removed",
		"job-history-removed-retention",
	})
}

func TestManualDeleteQueue_WaitsForWebhookFrozenParts(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	if _, err := a.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-delete", BroadcasterLogin: "delete", BroadcasterName: "Delete",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	mkDone := func(jobID string) *repository.Video {
		v, err := a.CreateVideo(ctx, &repository.VideoInput{
			JobID: jobID, Filename: jobID, DisplayName: "Delete", Status: repository.VideoStatusPending,
			Quality: repository.QualityHigh, BroadcasterID: "bc-delete", RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("create %s: %v", jobID, err)
		}
		if err := a.MarkVideoDone(ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
			t.Fatalf("mark done %s: %v", jobID, err)
		}
		return v
	}
	ready := mkDone("job-delete-ready")
	blocked := mkDone("job-delete-blocked")
	pending, err := a.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-delete-pending", Filename: "job-delete-pending", DisplayName: "Delete",
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		BroadcasterID: "bc-delete", RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("create pending: %v", err)
	}

	queued, err := a.RequestVideoDelete(ctx, ready.ID)
	if err != nil {
		t.Fatalf("RequestVideoDelete ready: %v", err)
	}
	if queued.DeleteRequestedAt == nil {
		t.Fatal("ready DeleteRequestedAt is nil")
	}
	queuedAgain, err := a.RequestVideoDelete(ctx, ready.ID)
	if err != nil {
		t.Fatalf("RequestVideoDelete ready again: %v", err)
	}
	if queuedAgain.DeleteRequestedAt == nil || !queuedAgain.DeleteRequestedAt.Equal(*queued.DeleteRequestedAt) {
		t.Fatalf("RequestVideoDelete not idempotent: first %v second %v", queued.DeleteRequestedAt, queuedAgain.DeleteRequestedAt)
	}
	if _, err := a.RequestVideoDelete(ctx, pending.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("RequestVideoDelete pending err = %v, want ErrNotFound", err)
	}
	if _, err := a.RequestVideoDelete(ctx, blocked.ID); err != nil {
		t.Fatalf("RequestVideoDelete blocked: %v", err)
	}
	delivery, err := a.CreateRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
		MessageID:     "msg-delete-blocked",
		DedupeKey:     "dedupe-delete-blocked",
		Event:         "recording.completed",
		VideoID:       blocked.ID,
		NextAttemptAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateRecordingWebhookDelivery: %v", err)
	}

	rows, err := a.ListVideosPendingManualDelete(ctx, 10)
	if err != nil {
		t.Fatalf("ListVideosPendingManualDelete before freeze: %v", err)
	}
	assertStringSlice(t, pgVideoJobIDs(rows), []string{"job-delete-ready"})

	if err := a.SoftDeleteVideo(ctx, ready.ID, repository.DeletionKindManual); err != nil {
		t.Fatalf("soft delete ready: %v", err)
	}
	readyGone, err := a.GetVideo(ctx, ready.ID)
	if err != nil {
		t.Fatalf("GetVideo ready after soft delete: %v", err)
	}
	if readyGone.DeleteRequestedAt != nil {
		t.Fatalf("ready DeleteRequestedAt after tombstone = %v, want nil", readyGone.DeleteRequestedAt)
	}
	if err := a.SetRecordingWebhookDeliveryFrozenParts(ctx, delivery.ID, "[]"); err != nil {
		t.Fatalf("SetRecordingWebhookDeliveryFrozenParts: %v", err)
	}
	rows, err = a.ListVideosPendingManualDelete(ctx, 10)
	if err != nil {
		t.Fatalf("ListVideosPendingManualDelete after freeze: %v", err)
	}
	assertStringSlice(t, pgVideoJobIDs(rows), []string{"job-delete-blocked"})

	race := mkDone("job-delete-race")
	if _, err := a.RequestVideoDelete(ctx, race.ID); err != nil {
		t.Fatalf("RequestVideoDelete race: %v", err)
	}
	if err := a.SoftDeleteVideo(ctx, race.ID, repository.DeletionKindRetention); err != nil {
		t.Fatalf("soft delete race as retention: %v", err)
	}
	raceGone, err := a.GetVideo(ctx, race.ID)
	if err != nil {
		t.Fatalf("GetVideo race after soft delete: %v", err)
	}
	if raceGone.DeleteRequestedAt != nil {
		t.Fatalf("race DeleteRequestedAt after tombstone = %v, want nil", raceGone.DeleteRequestedAt)
	}
	if raceGone.DeletionKind == nil || *raceGone.DeletionKind != repository.DeletionKindManual {
		t.Fatalf("race DeletionKind = %v, want %q because manual intent wins", raceGone.DeletionKind, repository.DeletionKindManual)
	}

	failed, err := a.CreateVideo(ctx, &repository.VideoInput{
		JobID: "job-delete-failed", Filename: "job-delete-failed", DisplayName: "Delete",
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		BroadcasterID: "bc-delete", RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("create failed terminal: %v", err)
	}
	if err := a.MarkVideoFailed(ctx, failed.ID, "seed-failed", repository.CompletionKindPartial, true); err != nil {
		t.Fatalf("MarkVideoFailed failed terminal: %v", err)
	}
	failedQueued, err := a.RequestVideoDelete(ctx, failed.ID)
	if err != nil {
		t.Fatalf("RequestVideoDelete failed terminal: %v", err)
	}
	if failedQueued.DeleteRequestedAt == nil {
		t.Fatal("failed terminal DeleteRequestedAt is nil")
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

func TestListCategoriesWithVideos(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	for _, c := range []repository.Category{
		{ID: "cat-visible", Name: "Visible Game"},
		{ID: "cat-searched", Name: "Searched Only"},
		{ID: "cat-deleted", Name: "Deleted Only"},
	} {
		cat := c
		if _, err := a.UpsertCategory(ctx, &cat); err != nil {
			t.Fatalf("seed category %s: %v", cat.ID, err)
		}
	}
	if _, err := a.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-cats", BroadcasterLogin: "cats", BroadcasterName: "Cats",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	visible, err := a.CreateVideo(ctx, &repository.VideoInput{
		JobID:         "job-visible-cat",
		Filename:      "visible-cat",
		DisplayName:   "Cats",
		Status:        repository.VideoStatusDone,
		Quality:       repository.QualityHigh,
		BroadcasterID: "bc-cats",
		Language:      "en",
	})
	if err != nil {
		t.Fatalf("create visible video: %v", err)
	}
	if err := a.LinkVideoCategory(ctx, visible.ID, "cat-visible"); err != nil {
		t.Fatalf("link visible category: %v", err)
	}

	deleted, err := a.CreateVideo(ctx, &repository.VideoInput{
		JobID:         "job-deleted-cat",
		Filename:      "deleted-cat",
		DisplayName:   "Cats",
		Status:        repository.VideoStatusDone,
		Quality:       repository.QualityHigh,
		BroadcasterID: "bc-cats",
		Language:      "en",
	})
	if err != nil {
		t.Fatalf("create deleted video: %v", err)
	}
	if err := a.LinkVideoCategory(ctx, deleted.ID, "cat-deleted"); err != nil {
		t.Fatalf("link deleted category: %v", err)
	}
	if err := a.SoftDeleteVideo(ctx, deleted.ID, repository.DeletionKindManual); err != nil {
		t.Fatalf("soft delete video: %v", err)
	}

	got, err := a.ListCategoriesWithVideos(ctx)
	if err != nil {
		t.Fatalf("list categories with videos: %v", err)
	}
	if len(got) != 1 || got[0].ID != "cat-visible" {
		t.Fatalf("ListCategoriesWithVideos = %+v, want only cat-visible", got)
	}

	got, err = a.SearchCategoriesWithVideos(ctx, "Visible", 10)
	if err != nil {
		t.Fatalf("search visible categories with videos: %v", err)
	}
	if len(got) != 1 || got[0].ID != "cat-visible" {
		t.Fatalf("SearchCategoriesWithVideos visible = %+v, want only cat-visible", got)
	}
	got, err = a.SearchCategoriesWithVideos(ctx, "Searched", 10)
	if err != nil {
		t.Fatalf("search catalog-only categories with videos: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("SearchCategoriesWithVideos catalog-only = %+v, want none", got)
	}
	got, err = a.SearchCategoriesWithVideos(ctx, "Deleted", 10)
	if err != nil {
		t.Fatalf("search deleted categories with videos: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("SearchCategoriesWithVideos deleted-only = %+v, want none", got)
	}
}

func TestGetCategoryDetail(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	description := "Local detail metadata."
	if _, err := a.UpsertCategory(ctx, &repository.Category{
		ID:          "cat-detail",
		Name:        "Detail Game",
		Description: &description,
	}); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	if _, err := a.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-category-detail", BroadcasterLogin: "categorydetail", BroadcasterName: "Category Detail",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	seeds := []struct {
		jobID   string
		status  string
		size    int64
		deleted bool
	}{
		{jobID: "category-detail-sized", status: repository.VideoStatusDone, size: 2_048},
		{jobID: "category-detail-running", status: repository.VideoStatusRunning},
		{jobID: "category-detail-deleted", status: repository.VideoStatusDone, size: 8_192, deleted: true},
	}
	for _, seed := range seeds {
		video, err := a.CreateVideo(ctx, &repository.VideoInput{
			JobID:         seed.jobID,
			Filename:      seed.jobID,
			DisplayName:   "Category Detail",
			Status:        seed.status,
			Quality:       repository.QualityHigh,
			BroadcasterID: "bc-category-detail",
			Language:      "en",
			RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("create video %s: %v", seed.jobID, err)
		}
		if err := a.LinkVideoCategory(ctx, video.ID, "cat-detail"); err != nil {
			t.Fatalf("link category %s: %v", seed.jobID, err)
		}
		if seed.size > 0 {
			if err := a.MarkVideoDone(ctx, video.ID, 60, seed.size, nil, repository.CompletionKindComplete, false); err != nil {
				t.Fatalf("mark done %s: %v", seed.jobID, err)
			}
		}
		if seed.deleted {
			if err := a.SoftDeleteVideo(ctx, video.ID, repository.DeletionKindManual); err != nil {
				t.Fatalf("soft delete %s: %v", seed.jobID, err)
			}
		}
	}

	got, err := a.GetCategoryDetail(ctx, "cat-detail")
	if err != nil {
		t.Fatalf("GetCategoryDetail: %v", err)
	}
	if got.Category.Description == nil || *got.Category.Description != description {
		t.Fatalf("description = %v, want %q", got.Category.Description, description)
	}
	if got.VideoCount != 2 {
		t.Fatalf("VideoCount = %d, want 2", got.VideoCount)
	}
	if got.TotalSize != 2_048 {
		t.Fatalf("TotalSize = %d, want 2048", got.TotalSize)
	}
}

func TestListCategoriesWithVideosPage_SortAndCursor(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	seedCategoryPageFixture(t, ctx, a)

	cases := []struct {
		name string
		sort string
		want []string
	}{
		{"default", "name_asc", []string{"Alpha Game", "Bravo Game", "Charlie Game"}},
		{"latest video", "latest_video_desc", []string{"Charlie Game", "Bravo Game", "Alpha Game"}},
		{"video count", "video_count_desc", []string{"Bravo Game", "Charlie Game", "Alpha Game"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collectCategoryPageNames(t, ctx, a, 1, tc.sort)
			assertStringSlice(t, got, tc.want)
		})
	}
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

func TestUpsertCategories_PreservesBoxArtAndReturnsInputOrder(t *testing.T) {
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

	got, err := a.UpsertCategories(ctx, []repository.Category{
		{ID: "g-2", Name: "Second"},
		{ID: "g-1", Name: "New Name"},
		{ID: "g-2", Name: "Second Duplicate"},
	})
	if err != nil {
		t.Fatalf("batch upsert: %v", err)
	}
	if len(got) != 2 || got[0].ID != "g-2" || got[1].ID != "g-1" {
		t.Fatalf("batch order = %+v, want g-2 then g-1", got)
	}

	updated, err := a.GetCategory(ctx, "g-1")
	if err != nil {
		t.Fatalf("get updated: %v", err)
	}
	if updated.Name != "New Name" {
		t.Fatalf("name = %q, want New Name", updated.Name)
	}
	if updated.BoxArtURL == nil || *updated.BoxArtURL != art {
		t.Fatalf("box_art_url = %v, want %q", updated.BoxArtURL, art)
	}
	if updated.IGDBID == nil || *updated.IGDBID != igdb {
		t.Fatalf("igdb_id = %v, want %q", updated.IGDBID, igdb)
	}
}

func TestListCategoriesByIDs_ReturnsInputOrder(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	for _, c := range []repository.Category{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B"},
		{ID: "c", Name: "C"},
	} {
		cat := c
		if _, err := a.UpsertCategory(ctx, &cat); err != nil {
			t.Fatalf("seed %s: %v", c.ID, err)
		}
	}

	got, err := a.ListCategoriesByIDs(ctx, []string{"c", "missing", "a", "c", "b"})
	if err != nil {
		t.Fatalf("ListCategoriesByIDs: %v", err)
	}
	want := []string{"c", "a", "b"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("row %d ID = %q, want %q: %+v", i, got[i].ID, id, got)
		}
	}
}

func TestUpdateCategoryGameMetadata(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	existingArt := "https://cdn.example.com/existing.jpg"
	if _, err := a.UpsertCategory(ctx, &repository.Category{
		ID:        "g-meta",
		Name:      "Meta",
		BoxArtURL: &existingArt,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := a.UpdateCategoryGameMetadata(ctx, "g-meta", "", "9876"); err != nil {
		t.Fatalf("update igdb only: %v", err)
	}
	got, err := a.GetCategory(ctx, "g-meta")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.BoxArtURL == nil || *got.BoxArtURL != existingArt {
		t.Fatalf("box_art_url = %v, want preserved %q", got.BoxArtURL, existingArt)
	}
	if got.IGDBID == nil || *got.IGDBID != "9876" {
		t.Fatalf("igdb_id = %v, want 9876", got.IGDBID)
	}
	if got.GameMetadataCheckedAt == nil {
		t.Fatal("game_metadata_checked_at must be set after game metadata update")
	}

	newArt := "https://cdn.example.com/new.jpg"
	if err := a.UpdateCategoryGameMetadata(ctx, "g-meta", newArt, ""); err != nil {
		t.Fatalf("update art only: %v", err)
	}
	got, err = a.GetCategory(ctx, "g-meta")
	if err != nil {
		t.Fatalf("get updated: %v", err)
	}
	if got.BoxArtURL == nil || *got.BoxArtURL != newArt {
		t.Fatalf("box_art_url = %v, want %q", got.BoxArtURL, newArt)
	}
	if got.IGDBID == nil || *got.IGDBID != "9876" {
		t.Fatalf("igdb_id = %v, want preserved 9876", got.IGDBID)
	}
}

func TestListCategoriesMissingGameMetadata(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	art := "art"
	igdb := "123"
	for _, category := range []repository.Category{
		{ID: "missing-both", Name: "A"},
		{ID: "missing-igdb", Name: "B", BoxArtURL: &art},
		{ID: "complete", Name: "C", BoxArtURL: &art, IGDBID: &igdb},
		{ID: "missing-art", Name: "D", IGDBID: &igdb},
		{ID: "recently-checked", Name: "E", BoxArtURL: &art},
	} {
		c := category
		if _, err := a.UpsertCategory(ctx, &c); err != nil {
			t.Fatalf("seed %s: %v", c.ID, err)
		}
	}
	if err := a.MarkCategoryGameMetadataChecked(ctx, "recently-checked"); err != nil {
		t.Fatalf("MarkCategoryGameMetadataChecked: %v", err)
	}

	got, err := a.ListCategoriesMissingGameMetadata(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListCategoriesMissingGameMetadata: %v", err)
	}
	want := []string{"missing-both", "missing-igdb", "missing-art"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %+v", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("row %d ID = %q, want %q: %+v", i, got[i].ID, id, got)
		}
	}
	retry, err := a.ListCategoriesMissingGameMetadata(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("ListCategoriesMissingGameMetadata retry: %v", err)
	}
	wantRetry := []string{"missing-both", "missing-igdb", "missing-art", "recently-checked"}
	if len(retry) != len(wantRetry) {
		t.Fatalf("retry len = %d, want %d: %+v", len(retry), len(wantRetry), retry)
	}
	for i, id := range wantRetry {
		if retry[i].ID != id {
			t.Fatalf("retry row %d ID = %q, want %q: %+v", i, retry[i].ID, id, retry)
		}
	}
}

func TestCategoryDescriptionMethods(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	igdb := "123"
	existing := "Existing"
	for _, category := range []repository.Category{
		{ID: "needs-description", Name: "A", IGDBID: &igdb},
		{ID: "no-igdb", Name: "B"},
		{ID: "has-description", Name: "C", IGDBID: &igdb, Description: &existing},
		{ID: "recently-checked", Name: "D", IGDBID: &igdb},
	} {
		c := category
		if _, err := a.UpsertCategory(ctx, &c); err != nil {
			t.Fatalf("seed %s: %v", c.ID, err)
		}
	}
	if err := a.MarkCategoryDescriptionChecked(ctx, "recently-checked"); err != nil {
		t.Fatalf("MarkCategoryDescriptionChecked: %v", err)
	}

	missing, err := a.ListCategoriesMissingDescription(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListCategoriesMissingDescription: %v", err)
	}
	if len(missing) != 1 || missing[0].ID != "needs-description" {
		t.Fatalf("missing description = %+v, want only needs-description", missing)
	}
	retry, err := a.ListCategoriesMissingDescription(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("ListCategoriesMissingDescription retry: %v", err)
	}
	if len(retry) != 2 || retry[0].ID != "needs-description" || retry[1].ID != "recently-checked" {
		t.Fatalf("retryable missing description = %+v, want needs-description and recently-checked", retry)
	}
	if err := a.UpdateCategoryDescription(ctx, "needs-description", "New description"); err != nil {
		t.Fatalf("UpdateCategoryDescription: %v", err)
	}
	got, err := a.GetCategory(ctx, "needs-description")
	if err != nil {
		t.Fatalf("GetCategory: %v", err)
	}
	if got.Description == nil || *got.Description != "New description" {
		t.Fatalf("description = %v, want New description", got.Description)
	}
	if got.DescriptionCheckedAt == nil {
		t.Fatal("description_checked_at must be set when description is updated")
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
