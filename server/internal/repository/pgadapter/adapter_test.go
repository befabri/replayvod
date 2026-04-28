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
