package sqliteadapter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitetype"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// newTestAdapter wires up a fresh adapter against a fresh migrated SQLite DB.
func newTestAdapter(t *testing.T) *SQLiteAdapter {
	t.Helper()
	return New(testdb.NewSQLiteDB(t))
}

// TestTimeRoundtrip exercises the SQLite timestamp formatter/parser directly.
// No DB involved — this is the cheapest regression signal if someone
// accidentally changes the format string.
//
// SQLite's "datetime('now')" default emits a second-precision TEXT with a
// space separator (`YYYY-MM-DD HH:MM:SS`). sqlitetype.Format must produce exactly
// that layout so Go writes round-trip through the default-NOW columns, and
// sqlitetype.Parse must accept it back. Nanoseconds are dropped on the floor — that
// matches SQLite's default precision and is fine for our purposes (download
// timestamps don't need ns precision).
func TestTimeRoundtrip(t *testing.T) {
	cases := []time.Time{
		time.Date(2026, 4, 12, 15, 30, 45, 0, time.UTC),
		time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC),
		time.Date(2038, 1, 19, 3, 14, 7, 0, time.UTC), // past int32 epoch, just in case
		time.Now().UTC().Truncate(time.Second),
	}
	for _, want := range cases {
		t.Run(want.Format(time.RFC3339), func(t *testing.T) {
			got, err := sqlitetype.Parse(sqlitetype.Format(want))
			if err != nil {
				t.Fatalf("Parse(Format()) error: %v", err)
			}
			if !got.Equal(want) {
				t.Errorf("roundtrip mismatch: want %v, got %v (via %q)", want, got, sqlitetype.Format(want))
			}
		})
	}
}

// TestTimeRoundtrip_DropsSubsecond documents that we intentionally discard
// sub-second precision. If we ever want to preserve it, this test will fail
// and force an explicit decision rather than a silent change.
func TestTimeRoundtrip_DropsSubsecond(t *testing.T) {
	withNs := time.Date(2026, 4, 12, 15, 30, 45, 123_456_789, time.UTC)
	got, err := sqlitetype.Parse(sqlitetype.Format(withNs))
	if err != nil {
		t.Fatalf("Parse(Format()) error: %v", err)
	}
	want := withNs.Truncate(time.Second)
	if !got.Equal(want) {
		t.Errorf("expected truncation to second precision: want %v, got %v", want, got)
	}
}

// TestSQLiteTimeParse pins the parse logic used by sqlitetype.Time.Scan: both
// accepted layouts round-trip, nil is the only legitimately absent scanned
// value, and malformed TEXT values hard-fail.
func TestSQLiteTimeParse(t *testing.T) {
	t.Run("space layout", func(t *testing.T) {
		got, err := sqlitetype.Parse("2026-04-12 15:30:45")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := time.Date(2026, 4, 12, 15, 30, 45, 0, time.UTC); !got.Equal(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
	t.Run("rfc3339 layout", func(t *testing.T) {
		got, err := sqlitetype.Parse("2026-04-12T15:30:45Z")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := time.Date(2026, 4, 12, 15, 30, 45, 0, time.UTC); !got.Equal(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
	t.Run("empty hard-fails", func(t *testing.T) {
		got, err := sqlitetype.Parse("")
		if err == nil {
			t.Fatal("expected an error for an empty timestamp, got nil")
		}
		if !got.IsZero() {
			t.Fatalf("error path should still return the zero instant, got %v", got)
		}
	})
	t.Run("malformed surfaces an error", func(t *testing.T) {
		got, err := sqlitetype.Parse("not-a-timestamp")
		if err == nil {
			t.Fatal("expected an error for an unparseable non-empty value, got nil (silent zero)")
		}
		if !got.IsZero() {
			t.Fatalf("error path should still return the zero instant, got %v", got)
		}
	})
}

func TestMalformedTimestampHardFails(t *testing.T) {
	tests := []string{"", "not-a-timestamp"}
	for _, bad := range tests {
		t.Run(fmt.Sprintf("%q", bad), func(t *testing.T) {
			ctx := context.Background()
			a := newTestAdapter(t)
			_, err := a.UpsertUser(ctx, &repository.User{
				ID:          "u1",
				Login:       "login",
				DisplayName: "Display",
				Role:        "user",
			})
			if err != nil {
				t.Fatalf("UpsertUser: %v", err)
			}
			if _, err := a.db.ExecContext(ctx, "UPDATE users SET created_at = ? WHERE id = ?", bad, "u1"); err != nil {
				t.Fatalf("corrupt timestamp: %v", err)
			}
			_, err = a.GetUser(ctx, "u1")
			if err == nil {
				t.Fatal("GetUser succeeded with a malformed timestamp; want scan error")
			}
			if !strings.Contains(err.Error(), "unparseable") {
				t.Fatalf("GetUser error = %v, want unparseable timestamp error", err)
			}
		})
	}
}

func TestServerSettings_RoundTrip(t *testing.T) {
	ctx := context.Background()
	adapter := newTestAdapter(t)

	_, err := adapter.GetServerSettings(ctx)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("GetServerSettings before insert = %v, want ErrNotFound", err)
	}

	// Set every URL column, including the webhook callback, so a per-column
	// mapping bug in the SQLite query or scan is caught here. The adapter is
	// a dumb store (sanitization lives in the service), so persisting a
	// webhook URL alongside relay delivery is the right thing to exercise.
	want := &repository.ServerSettings{
		ServerMode:                    "relay",
		EventSubWebhookCallbackURL:    "https://replayvod.example/api/v1/webhook/callback",
		EventSubRelayIngestURL:        "https://relay.replayvod.com/u/AAAAAAAAAAAAAAAA",
		EventSubRelaySubscribeURL:     "wss://relay.replayvod.com/u/AAAAAAAAAAAAAAAA/subscribe",
		EventSubRelayLocalCallbackURL: "http://127.0.0.1:8080/api/v1/webhook/callback",
	}
	saved, err := adapter.UpsertServerSettings(ctx, want)
	if err != nil {
		t.Fatalf("UpsertServerSettings: %v", err)
	}
	if saved.ServerMode != want.ServerMode {
		t.Fatalf("ServerMode = %q, want %q", saved.ServerMode, want.ServerMode)
	}
	if saved.CreatedAt.IsZero() || saved.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not populated: created=%v updated=%v", saved.CreatedAt, saved.UpdatedAt)
	}

	reloaded, err := adapter.GetServerSettings(ctx)
	if err != nil {
		t.Fatalf("GetServerSettings after insert: %v", err)
	}
	if reloaded.EventSubWebhookCallbackURL != want.EventSubWebhookCallbackURL {
		t.Fatalf("WebhookCallbackURL = %q, want %q", reloaded.EventSubWebhookCallbackURL, want.EventSubWebhookCallbackURL)
	}
	if reloaded.EventSubRelayIngestURL != want.EventSubRelayIngestURL {
		t.Fatalf("RelayIngestURL = %q, want %q", reloaded.EventSubRelayIngestURL, want.EventSubRelayIngestURL)
	}
	if reloaded.EventSubRelaySubscribeURL != want.EventSubRelaySubscribeURL {
		t.Fatalf("RelaySubscribeURL = %q, want %q", reloaded.EventSubRelaySubscribeURL, want.EventSubRelaySubscribeURL)
	}
	if reloaded.EventSubRelayLocalCallbackURL != want.EventSubRelayLocalCallbackURL {
		t.Fatalf("RelayLocalCallbackURL = %q, want %q", reloaded.EventSubRelayLocalCallbackURL, want.EventSubRelayLocalCallbackURL)
	}

	updated, err := adapter.UpsertServerSettings(ctx, &repository.ServerSettings{
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
		t.Fatalf("updated WebhookCallbackURL = %q", updated.EventSubWebhookCallbackURL)
	}
	if updated.EventSubRelayIngestURL != "" {
		t.Fatalf("updated RelayIngestURL = %q, want empty", updated.EventSubRelayIngestURL)
	}
	if updated.EventSubRelaySubscribeURL != "" {
		t.Fatalf("updated RelaySubscribeURL = %q, want empty", updated.EventSubRelaySubscribeURL)
	}
	if updated.EventSubRelayLocalCallbackURL != "" {
		t.Fatalf("updated RelayLocalCallbackURL = %q, want empty", updated.EventSubRelayLocalCallbackURL)
	}

	var rowCount int
	if err := adapter.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM server_settings").Scan(&rowCount); err != nil {
		t.Fatalf("count server_settings rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("server_settings row count = %d, want 1", rowCount)
	}
}

// TestServerSettings_InsertStoresEmptyURLColumns pins the fresh-INSERT path with
// no URLs (the round-trip test only exercises insert-with-URLs then reset-to-
// empty): an off config persists and reads back with all four URL columns empty.
func TestServerSettings_InsertStoresEmptyURLColumns(t *testing.T) {
	ctx := context.Background()
	adapter := newTestAdapter(t)

	saved, err := adapter.UpsertServerSettings(ctx, &repository.ServerSettings{ServerMode: "off"})
	if err != nil {
		t.Fatalf("UpsertServerSettings(off): %v", err)
	}
	for _, c := range []struct {
		name, got string
	}{
		{"webhook", saved.EventSubWebhookCallbackURL},
		{"ingest", saved.EventSubRelayIngestURL},
		{"subscribe", saved.EventSubRelaySubscribeURL},
		{"local", saved.EventSubRelayLocalCallbackURL},
	} {
		if c.got != "" {
			t.Fatalf("fresh insert returned non-empty %s URL = %q", c.name, c.got)
		}
	}
	reloaded, err := adapter.GetServerSettings(ctx)
	if err != nil {
		t.Fatalf("GetServerSettings: %v", err)
	}
	if reloaded.ServerMode != "off" || reloaded.EventSubRelayIngestURL != "" {
		t.Fatalf("reloaded = %#v, want off with empty URLs", reloaded)
	}
}

// TestServerSettings_UpsertWrapsDriverError pins the upsert error wrap: a driver
// failure surfaces with the adapter's context prefix rather than a bare pgx/sql
// error, which is what operators see in logs.
func TestServerSettings_UpsertWrapsDriverError(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	adapter := New(db)
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	_, err := adapter.UpsertServerSettings(ctx, &repository.ServerSettings{ServerMode: "off"})
	if err == nil {
		t.Fatal("UpsertServerSettings on a closed db = nil, want a wrapped driver error")
	}
	if !strings.Contains(err.Error(), "sqlite upsert server settings") {
		t.Fatalf("error = %v, want the adapter context prefix", err)
	}
}

// TestServerSettings_UpsertPreservesCreatedAtAndAdvancesUpdatedAt pins the
// upsert's timestamp contract: the UPDATE branch must leave created_at untouched
// and bump updated_at. Both are easy to break (omit updated_at = datetime('now'),
// or accidentally clobber created_at), and a same-second round-trip would mask
// it, so we backdate the row and assert the UPDATE moves only updated_at.
func TestServerSettings_UpsertPreservesCreatedAtAndAdvancesUpdatedAt(t *testing.T) {
	ctx := context.Background()
	adapter := newTestAdapter(t)

	if _, err := adapter.UpsertServerSettings(ctx, &repository.ServerSettings{ServerMode: "poll"}); err != nil {
		t.Fatalf("seed upsert: %v", err)
	}
	// Backdate both timestamps so the next upsert's datetime('now') is
	// unambiguously later than created_at, without a real-time sleep.
	const backdated = "2000-01-01 00:00:00"
	old := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := adapter.db.ExecContext(ctx,
		"UPDATE server_settings SET created_at = ?, updated_at = ? WHERE id = 1", backdated, backdated); err != nil {
		t.Fatalf("backdate timestamps: %v", err)
	}

	updated, err := adapter.UpsertServerSettings(ctx, &repository.ServerSettings{ServerMode: "off"})
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

// TestSQLiteRoundtrip is the full-stack equivalent: every domain type the
// adapter touches writes and reads cleanly through the driver. Covers the
// NOT NULL timestamp path (streams.started_at), nullable timestamp
// (streams.ended_at), nullable bool (is_mature), nullable float/int/string
// (video duration/size/thumbnail), and the CAST-to-INTEGER/REAL statistics
// aggregation.
//
// Extending this is the pattern for every adapter-level test that follows.
func TestSQLiteRoundtrip(t *testing.T) {
	ctx := context.Background()
	adapter := newTestAdapter(t)

	email := "test@example.com"
	profile := "https://example.com/pic.png"
	if _, err := adapter.UpsertUser(ctx, &repository.User{
		ID:              "12345",
		Login:           "testuser",
		DisplayName:     "TestUser",
		Email:           &email,
		ProfileImageURL: &profile,
		Role:            "owner",
	}); err != nil {
		t.Fatalf("upsert user: %v", err)
	}

	lang := "en"
	desc := "A test channel"
	if _, err := adapter.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID:       "12345",
		BroadcasterLogin:    "testuser",
		BroadcasterName:     "TestUser",
		BroadcasterLanguage: &lang,
		Description:         &desc,
		ViewCount:           42,
	}); err != nil {
		t.Fatalf("upsert channel: %v", err)
	}

	// Stream: NOT NULL timestamp + nullable bool.
	isMature := true
	startedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	stream, err := adapter.UpsertStream(ctx, &repository.StreamInput{
		ID:            "stream-1",
		BroadcasterID: "12345",
		Type:          "live",
		Language:      "en",
		ViewerCount:   100,
		IsMature:      &isMature,
		StartedAt:     startedAt,
	})
	if err != nil {
		t.Fatalf("upsert stream: %v", err)
	}
	if !stream.StartedAt.Equal(startedAt) {
		t.Errorf("StartedAt round-trip: want %v got %v", startedAt, stream.StartedAt)
	}
	if stream.IsMature == nil || *stream.IsMature != true {
		t.Errorf("IsMature round-trip: got %v", stream.IsMature)
	}

	// Nullable timestamp through EndStream → GetStream.
	endedAt := time.Now().UTC().Truncate(time.Second)
	if err := adapter.EndStream(ctx, stream.ID, endedAt); err != nil {
		t.Fatalf("end stream: %v", err)
	}
	reloaded, err := adapter.GetStream(ctx, stream.ID)
	if err != nil {
		t.Fatalf("reload stream: %v", err)
	}
	if reloaded.EndedAt == nil {
		t.Fatalf("EndedAt nil after EndStream")
	}
	if !reloaded.EndedAt.Equal(endedAt) {
		t.Errorf("EndedAt round-trip: want %v got %v", endedAt, *reloaded.EndedAt)
	}

	// Video: *float64, *int64, *string on MarkVideoDone.
	streamID := stream.ID
	vid, err := adapter.CreateVideo(ctx, &repository.VideoInput{
		JobID:         "job-test-1",
		Filename:      "20260412-140500-testuser-abc12345",
		DisplayName:   "TestUser",
		Status:        repository.VideoStatusPending,
		Quality:       repository.QualityHigh,
		BroadcasterID: "12345",
		StreamID:      &streamID,
		ViewerCount:   100,
		Language:      "en",
	})
	if err != nil {
		t.Fatalf("create video: %v", err)
	}

	thumb := "thumbnails/20260412-140500-testuser-abc12345.jpg"
	selectedFPS := 59.94
	if err := adapter.UpdateVideoSelectedVariant(ctx, vid.ID, "1080", &selectedFPS); err != nil {
		t.Fatalf("set selected variant: %v", err)
	}
	if err := adapter.MarkVideoDone(ctx, vid.ID, 3600.5, 1_073_741_824, &thumb, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("mark done: %v", err)
	}
	done, err := adapter.GetVideo(ctx, vid.ID)
	if err != nil {
		t.Fatalf("reload video: %v", err)
	}
	if done.DurationSeconds == nil || *done.DurationSeconds != 3600.5 {
		t.Errorf("duration round-trip: got %v", done.DurationSeconds)
	}
	if done.SizeBytes == nil || *done.SizeBytes != 1_073_741_824 {
		t.Errorf("size round-trip: got %v", done.SizeBytes)
	}
	if done.Thumbnail == nil || *done.Thumbnail != thumb {
		t.Errorf("thumbnail round-trip: got %v", done.Thumbnail)
	}
	if done.SelectedQuality == nil || *done.SelectedQuality != "1080" {
		t.Errorf("selected quality round-trip: got %v", done.SelectedQuality)
	}
	if done.SelectedFPS == nil || *done.SelectedFPS != selectedFPS {
		t.Errorf("selected fps round-trip: got %v", done.SelectedFPS)
	}
	if done.DownloadedAt == nil {
		t.Errorf("DownloadedAt nil after MarkVideoDone")
	}

	// Statistics: explicit CAST in SQL should produce int64 and float64,
	// never interface{}. If sqlc ever stops inferring these correctly this
	// test fails loudly.
	totals, err := adapter.VideoStatsTotals(ctx, "")
	if err != nil {
		t.Fatalf("stats totals: %v", err)
	}
	if totals.Total != 1 || totals.TotalSize != 1_073_741_824 || totals.TotalDuration != 3600.5 {
		t.Errorf("stats: %+v", *totals)
	}
}

// TestListVideos_ColumnRoundtrip pins every video domain field exposed by
// the sqlc ListVideos query. Inserting a row with every non-null/nullable
// field populated and reading it back catches schema and mapper drift.
func TestListVideos_ColumnRoundtrip(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	if _, err := a.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-rt", BroadcasterLogin: "bc", BroadcasterName: "BC",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	// Seed a stream so VideoInput.StreamID can point at it (there's
	// an FK on videos.stream_id → streams.id; leaving it nil would
	// skip a column this test is trying to assert on).
	if _, err := a.UpsertStream(ctx, &repository.StreamInput{
		ID: "stream-rt", BroadcasterID: "bc-rt", Type: "live", Language: "en",
		ViewerCount: 1, StartedAt: time.Now().UTC().Truncate(time.Second),
	}); err != nil {
		t.Fatalf("seed stream: %v", err)
	}
	streamID := "stream-rt"
	retentionWindow := int64(72)
	in := &repository.VideoInput{
		JobID:                "job-rt",
		Filename:             "filename-rt",
		DisplayName:          "Display Name RT",
		Status:               repository.VideoStatusDone,
		Quality:              repository.QualityHigh,
		BroadcasterID:        "bc-rt",
		StreamID:             &streamID,
		ViewerCount:          1234,
		Language:             "en",
		RecordingType:        repository.RecordingTypeVideo,
		ForceH264:            true,
		RetentionWindowHours: &retentionWindow,
	}
	v, err := a.CreateVideo(ctx, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	selectedFPS := 60.0
	if err := a.UpdateVideoSelectedVariant(ctx, v.ID, "1080", &selectedFPS); err != nil {
		t.Fatalf("set selected variant: %v", err)
	}
	thumb := "thumbnails/thumb.jpg"
	if err := a.MarkVideoDone(ctx, v.ID, 3600.5, 1_073_741_824, &thumb, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("mark done: %v", err)
	}

	rows, err := a.ListVideos(ctx, repository.ListVideosOpts{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	got := rows[0]

	// Every domain field must round-trip. If a column is added to the
	// videos migration, extend repository.Video accordingly, then add
	// a matching assertion here.
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"ID", got.ID, v.ID},
		{"JobID", got.JobID, in.JobID},
		{"Filename", got.Filename, in.Filename},
		{"DisplayName", got.DisplayName, in.DisplayName},
		{"Status", got.Status, repository.VideoStatusDone},
		{"Quality", got.Quality, in.Quality},
		{"SelectedQuality", derefString(got.SelectedQuality), "1080"},
		{"SelectedFPS", derefFloat64(got.SelectedFPS), selectedFPS},
		{"BroadcasterID", got.BroadcasterID, in.BroadcasterID},
		{"StreamID", derefString(got.StreamID), streamID},
		{"ViewerCount", got.ViewerCount, in.ViewerCount},
		{"Language", got.Language, in.Language},
		{"DurationSeconds", derefFloat64(got.DurationSeconds), 3600.5},
		{"SizeBytes", derefInt64(got.SizeBytes), int64(1_073_741_824)},
		{"Thumbnail", derefString(got.Thumbnail), thumb},
		{"RecordingType", got.RecordingType, in.RecordingType},
		{"ForceH264", got.ForceH264, in.ForceH264},
		{"RetentionWindowHours", derefInt64(got.RetentionWindowHours), retentionWindow},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
	if got.StartDownloadAt.IsZero() {
		t.Error("StartDownloadAt: zero value — default datetime('now') not applied")
	}
	if got.DownloadedAt == nil || got.DownloadedAt.IsZero() {
		t.Error("DownloadedAt: nil — MarkVideoDone should have stamped it")
	}
}

// TestSearchChannels_ColumnRoundtrip pins every channel field exposed by
// the generated SearchChannels query. Every populatable field on Channel
// must flow through.
func TestSearchChannels_ColumnRoundtrip(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	lang := "en"
	profile := "https://example.com/profile.png"
	offline := "https://example.com/offline.png"
	desc := "Channel description with context."
	btype := "partner"
	in := &repository.Channel{
		BroadcasterID:       "bc-rt",
		BroadcasterLogin:    "login_rt",
		BroadcasterName:     "Display RT",
		BroadcasterLanguage: &lang,
		ProfileImageURL:     &profile,
		OfflineImageURL:     &offline,
		Description:         &desc,
		BroadcasterType:     &btype,
		ViewCount:           9876,
	}
	if _, err := a.UpsertChannel(ctx, in); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rows, err := a.SearchChannels(ctx, "login_rt", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	got := rows[0]

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"BroadcasterID", got.BroadcasterID, in.BroadcasterID},
		{"BroadcasterLogin", got.BroadcasterLogin, in.BroadcasterLogin},
		{"BroadcasterName", got.BroadcasterName, in.BroadcasterName},
		{"BroadcasterLanguage", derefString(got.BroadcasterLanguage), lang},
		{"ProfileImageURL", derefString(got.ProfileImageURL), profile},
		{"OfflineImageURL", derefString(got.OfflineImageURL), offline},
		{"Description", derefString(got.Description), desc},
		{"BroadcasterType", derefString(got.BroadcasterType), btype},
		{"ViewCount", got.ViewCount, in.ViewCount},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt: zero — default datetime('now') not applied")
	}
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func derefFloat64(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}
func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func TestSearchVideos(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	for _, ch := range []repository.Channel{
		{BroadcasterID: "bc-title", BroadcasterLogin: "titlecaster", BroadcasterName: "Title Caster"},
		{BroadcasterID: "bc-channel", BroadcasterLogin: "neoncaster", BroadcasterName: "Neon Caster"},
		{BroadcasterID: "bc-category", BroadcasterLogin: "categorycaster", BroadcasterName: "Category Caster"},
	} {
		channel := ch
		if _, err := a.UpsertChannel(ctx, &channel); err != nil {
			t.Fatalf("seed channel %s: %v", ch.BroadcasterID, err)
		}
	}
	for _, cat := range []repository.Category{
		{ID: "cat-neon", Name: "Neon Game"},
		{ID: "cat-other", Name: "Other Game"},
	} {
		category := cat
		if _, err := a.UpsertCategory(ctx, &category); err != nil {
			t.Fatalf("seed category %s: %v", cat.ID, err)
		}
	}

	base := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	seeds := []struct {
		jobID         string
		title         string
		displayName   string
		broadcasterID string
		minute        int
		categoryID    string
		historyTitle  string
	}{
		{"job-title", "Neon Run", "Title Caster", "bc-title", 1, "cat-other", ""},
		{"job-title-history", "Opening Soon", "Title Caster", "bc-title", 2, "cat-other", "Neon Finale"},
		{"job-channel", "Different", "Neon Caster", "bc-channel", 3, "cat-other", ""},
		{"job-category", "Different", "Category Caster", "bc-category", 4, "cat-neon", ""},
		{"job-substring", "Late Neon Mix", "Title Caster", "bc-title", 5, "cat-other", ""},
	}
	for _, s := range seeds {
		v, err := a.CreateVideo(ctx, &repository.VideoInput{
			JobID:         s.jobID,
			Filename:      s.jobID,
			DisplayName:   s.displayName,
			Title:         s.title,
			Status:        repository.VideoStatusDone,
			Quality:       repository.QualityHigh,
			BroadcasterID: s.broadcasterID,
			Language:      "en",
			RecordingType: repository.RecordingTypeVideo,
		})
		if err != nil {
			t.Fatalf("create %s: %v", s.jobID, err)
		}
		if s.categoryID != "" {
			if err := a.LinkVideoCategory(ctx, v.ID, s.categoryID); err != nil {
				t.Fatalf("link category %s: %v", s.jobID, err)
			}
		}
		if s.historyTitle != "" {
			title, err := a.UpsertTitle(ctx, s.historyTitle)
			if err != nil {
				t.Fatalf("upsert history title %s: %v", s.jobID, err)
			}
			if err := a.LinkVideoTitle(ctx, v.ID, title.ID); err != nil {
				t.Fatalf("link history title %s: %v", s.jobID, err)
			}
		}
		startedAt := base.Add(time.Duration(s.minute) * time.Minute)
		if _, err := a.db.ExecContext(ctx, "UPDATE videos SET start_download_at = ? WHERE id = ?", sqlitetype.Format(startedAt), v.ID); err != nil {
			t.Fatalf("override start_download_at %s: %v", s.jobID, err)
		}
	}

	t.Run("title and title-history rank before channel/category matches", func(t *testing.T) {
		got, err := a.SearchVideos(ctx, "neon", 10)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		jobs := videoJobIDs(got)
		if len(jobs) < 5 {
			t.Fatalf("expected all seeded matches, got %v", jobs)
		}
		wantFirst := map[string]bool{"job-title": true, "job-title-history": true}
		if !wantFirst[jobs[0]] || !wantFirst[jobs[1]] {
			t.Fatalf("title matches should rank first, got %v", jobs)
		}
	})

	t.Run("query matches broadcaster metadata", func(t *testing.T) {
		got, err := a.SearchVideos(ctx, "neoncaster", 10)
		if err != nil {
			t.Fatalf("search channel: %v", err)
		}
		if len(got) == 0 || got[0].JobID != "job-channel" {
			t.Fatalf("expected job-channel first, got %v", videoJobIDs(got))
		}
	})

	t.Run("query matches linked category", func(t *testing.T) {
		got, err := a.SearchVideos(ctx, "Neon Game", 10)
		if err != nil {
			t.Fatalf("search category: %v", err)
		}
		if len(got) == 0 || got[0].JobID != "job-category" {
			t.Fatalf("expected job-category first, got %v", videoJobIDs(got))
		}
	})

	t.Run("limit caps result rows", func(t *testing.T) {
		got, err := a.SearchVideos(ctx, "neon", 2)
		if err != nil {
			t.Fatalf("search limit: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("limit=2 should return 2 rows, got %d", len(got))
		}
	})

	t.Run("soft-deleted videos are excluded", func(t *testing.T) {
		if err := a.SoftDeleteVideo(ctx, gotVideoID(t, ctx, a, "job-title"), repository.DeletionKindManual); err != nil {
			t.Fatalf("soft delete: %v", err)
		}
		got, err := a.SearchVideos(ctx, "Neon Run", 10)
		if err != nil {
			t.Fatalf("search deleted: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("soft-deleted title should not return, got %v", videoJobIDs(got))
		}
	})
}

func videoJobIDs(videos []repository.Video) []string {
	out := make([]string, len(videos))
	for i, v := range videos {
		out[i] = v.JobID
	}
	return out
}

func gotVideoID(t *testing.T, ctx context.Context, a *SQLiteAdapter, jobID string) int64 {
	t.Helper()
	v, err := a.GetVideoByJobID(ctx, jobID)
	if err != nil {
		t.Fatalf("get %s: %v", jobID, err)
	}
	return v.ID
}

// TestSearchChannels pins the ranking + filter contract. Split into
// subtests so an assertion failure in one case (e.g., rank order)
// doesn't mask the others. Covers the generated SearchChannels path where
// SQLite parameters are reused across WHERE and CASE ranking branches.
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

	t.Run("prefix beats substring, alphabetical within prefix", func(t *testing.T) {
		got, err := a.SearchChannels(ctx, "sh", 10)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		want := []string{"shoal", "shroud", "ashotoftoast"}
		assertLogins(t, got, want)
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

	t.Run("empty query returns all, alphabetically", func(t *testing.T) {
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

	t.Run("LIKE wildcard chars in query match literally if present in data", func(t *testing.T) {
		// % and _ are LIKE wildcards. We don't escape them, so a query
		// containing "%" matches ANY character sequence at that position,
		// not a literal "%". The "percent_tester" login contains a
		// literal "_" — a search for "%tester" returns it via wildcard
		// expansion. Document the behavior so a future "escape LIKE
		// metacharacters" refactor has a test to update.
		got, err := a.SearchChannels(ctx, "%tester", 10)
		if err != nil {
			t.Fatalf("search wildcard: %v", err)
		}
		// "%tester" pattern: anything ending in "tester". percent_tester
		// matches (its login ends in "tester").
		foundPercent := false
		for _, c := range got {
			if c.BroadcasterLogin == "percent_tester" {
				foundPercent = true
			}
		}
		if !foundPercent {
			t.Errorf("expected percent_tester in wildcard result, got %v", loginsOf(got))
		}
	})

	t.Run("query matches display name as well as login", func(t *testing.T) {
		// Name "100% tester" has "100" in it; login doesn't. Searching
		// "100" should return the row via the name match branch.
		got, err := a.SearchChannels(ctx, "100", 10)
		if err != nil {
			t.Fatalf("search by name: %v", err)
		}
		if len(got) != 1 || got[0].BroadcasterID != "5" {
			t.Errorf("expected 1 row matched via display name, got %v", loginsOf(got))
		}
	})
}

func loginsOf(channels []repository.Channel) []string {
	out := make([]string, len(channels))
	for i, c := range channels {
		out[i] = c.BroadcasterLogin
	}
	return out
}

func assertLogins(t *testing.T, got []repository.Channel, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("row count: want %d (%v) got %d (%v)", len(want), want, len(got), loginsOf(got))
	}
	for i, w := range want {
		if got[i].BroadcasterLogin != w {
			t.Errorf("row %d: want %s got %s", i, w, got[i].BroadcasterLogin)
		}
	}
}

// TestSearchCategories pins the ranking + filter contract for the
// category combobox. Mirrors TestSearchChannels so the two pickers
// (schedule form broadcaster + category) share an assertion shape.
// Exercises the generated SearchCategories query specifically: the query
// parameter is reused across WHERE and CASE, matching SearchChannels.
func TestSearchCategories(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	seed := []repository.Category{
		{ID: "1", Name: "Valorant"},
		{ID: "2", Name: "Valheim"},
		{ID: "3", Name: "The Legend of Valor"},
		{ID: "4", Name: "Celeste"},
		{ID: "5", Name: "50% Off"}, // exercises LIKE wildcard edge case
		{ID: "6", Name: "Échecs"},
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

	t.Run("LIKE wildcard chars in query are not escaped", func(t *testing.T) {
		// Same documented behavior as SearchChannels: '%' is a LIKE
		// wildcard. "50%" as query matches "50% Off" via the escape-
		// free substring expansion ('%' + '50%' + '%' = '%50%%') —
		// SQLite's LIKE treats the literal '%' in the data as any
		// character, which happens to also match the literal '%'.
		got, err := a.SearchCategories(ctx, "50%", 10)
		if err != nil {
			t.Fatalf("search wildcard: %v", err)
		}
		found := false
		for _, c := range got {
			if c.Name == "50% Off" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected '50%% Off' in result, got %v", namesOf(got))
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

// TestSearchCategories_ColumnRoundtrip pins every category field exposed by
// the generated SearchCategories query, same pattern as channels and videos.
// A schema migration that adds a category field should extend this test.
func TestSearchCategories_ColumnRoundtrip(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	art := "https://cdn.example.com/g-rt-{width}x{height}.jpg"
	igdb := "igdb-rt"
	in := &repository.Category{
		ID:        "cat-rt",
		Name:      "Roundtrip",
		BoxArtURL: &art,
		IGDBID:    &igdb,
	}
	if _, err := a.UpsertCategory(ctx, in); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rows, err := a.SearchCategories(ctx, "Roundtrip", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	got := rows[0]

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"ID", got.ID, in.ID},
		{"Name", got.Name, in.Name},
		{"BoxArtURL", derefString(got.BoxArtURL), art},
		{"IGDBID", derefString(got.IGDBID), igdb},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt: zero — default datetime('now') not applied")
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt: zero — default not applied")
	}
}

func TestListPrimaryCategoriesForVideos_HardFailsMalformedFirstSeenAt(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	if _, err := a.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-primary-bad", BroadcasterLogin: "primary-bad", BroadcasterName: "Primary Bad",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	cat := repository.Category{ID: "cat-bad-time", Name: "Bad Time"}
	if _, err := a.UpsertCategory(ctx, &cat); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	video, err := a.CreateVideo(ctx, &repository.VideoInput{
		JobID:         "job-primary-bad-time",
		Filename:      "primary-bad-time",
		DisplayName:   "Primary Bad",
		Status:        repository.VideoStatusPending,
		Quality:       repository.QualityHigh,
		BroadcasterID: "bc-primary-bad",
		Language:      "en",
	})
	if err != nil {
		t.Fatalf("create video: %v", err)
	}
	if err := a.UpsertVideoCategorySpan(ctx, video.ID, cat.ID, time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("span category: %v", err)
	}
	if _, err := a.db.ExecContext(ctx, "UPDATE video_category_spans SET started_at = ? WHERE video_id = ?", "not-a-timestamp", video.ID); err != nil {
		t.Fatalf("corrupt span started_at: %v", err)
	}

	_, err = a.ListPrimaryCategoriesForVideos(ctx, []int64{video.ID})
	if err == nil {
		t.Fatal("ListPrimaryCategoriesForVideos succeeded with malformed first_seen_at; want scan error")
	}
	if !strings.Contains(err.Error(), "first_seen_at") || !strings.Contains(err.Error(), "unparseable") {
		t.Fatalf("ListPrimaryCategoriesForVideos error = %v, want first_seen_at unparseable error", err)
	}
}

func TestListPrimaryCategoriesForVideos_IgnoresMalformedDiscardedFirstSeenAt(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	if _, err := a.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-primary-discarded-bad", BroadcasterLogin: "primary-discarded-bad", BroadcasterName: "Primary Discarded Bad",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	primary := repository.Category{ID: "cat-primary-good-time", Name: "Primary Good"}
	if _, err := a.UpsertCategory(ctx, &primary); err != nil {
		t.Fatalf("seed primary category: %v", err)
	}
	decoy := repository.Category{ID: "cat-discarded-bad-time", Name: "Discarded Bad"}
	if _, err := a.UpsertCategory(ctx, &decoy); err != nil {
		t.Fatalf("seed decoy category: %v", err)
	}
	video, err := a.CreateVideo(ctx, &repository.VideoInput{
		JobID:         "job-primary-discarded-bad-time",
		Filename:      "primary-discarded-bad-time",
		DisplayName:   "Primary Discarded Bad",
		Status:        repository.VideoStatusPending,
		Quality:       repository.QualityHigh,
		BroadcasterID: "bc-primary-discarded-bad",
		Language:      "en",
	})
	if err != nil {
		t.Fatalf("create video: %v", err)
	}
	base := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	if _, err := a.db.ExecContext(ctx, `
INSERT INTO video_category_spans (video_id, category_id, started_at, ended_at, duration_seconds)
VALUES (?, ?, ?, ?, ?)`,
		video.ID, primary.ID, sqlitetype.Format(base), sqlitetype.Format(base.Add(time.Minute)), 120.0); err != nil {
		t.Fatalf("insert primary span: %v", err)
	}
	if _, err := a.db.ExecContext(ctx, `
INSERT INTO video_category_spans (video_id, category_id, started_at, ended_at, duration_seconds)
VALUES (?, ?, ?, ?, ?)`,
		video.ID, decoy.ID, "not-a-timestamp", sqlitetype.Format(base.Add(time.Minute)), 1.0); err != nil {
		t.Fatalf("insert discarded span: %v", err)
	}

	got, err := a.ListPrimaryCategoriesForVideos(ctx, []int64{video.ID})
	if err != nil {
		t.Fatalf("ListPrimaryCategoriesForVideos returned discarded-row first_seen_at error: %v", err)
	}
	if cat, ok := got[video.ID]; !ok || cat.ID != primary.ID {
		t.Fatalf("primary category = %+v, want %s", cat, primary.ID)
	}
}
