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
	totals, err := adapter.VideoStatsTotals(ctx)
	if err != nil {
		t.Fatalf("stats totals: %v", err)
	}
	if totals.Total != 1 || totals.TotalSize != 1_073_741_824 || totals.TotalDuration != 3600.5 {
		t.Errorf("stats: %+v", *totals)
	}
}

// TestSQLiteAdapter_ErrNotFound asserts that sql.ErrNoRows gets translated
// to repository.ErrNotFound. Services at the tRPC boundary do
// errors.Is(err, repository.ErrNotFound) to map 404s; without translation
// they'd silently 500 on SQLite.
func TestSQLiteAdapter_ErrNotFound(t *testing.T) {
	ctx := context.Background()
	adapter := newTestAdapter(t)

	_, err := adapter.GetVideo(ctx, 999999)
	if err == nil {
		t.Fatal("expected error on missing video, got nil")
	}
	if !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("expected repository.ErrNotFound, got %v", err)
	}
}

// TestListVideos_ColumnRoundtrip pins every scan target against the
// SELECT in listVideosSQL. The hand-rolled SQL is outside sqlc's
// codegen reach (see queries/sqlite/videos.sql), so a schema migration
// that adds a column can silently drift from this scan and fail only
// at runtime. Inserting a row with every non-null/nullable field
// populated and reading it back proves the scan column order still
// matches — a mismatch shows up as a Scan error at test time, not in
// production. Keep adding assertions as videos grows columns.
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
	// a matching assertion here — the Scan in listVideosSQL must be
	// kept in sync at the same time.
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

// TestSearchChannels_ColumnRoundtrip pins the scan column order for
// the hand-rolled searchChannelsSQL the same way the videos variant
// does. Every populatable field on Channel must flow through.
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

// TestListVideos_SortDimensions verifies that the hand-rolled
// listVideosSQL actually responds to every sort_key variant — the
// earlier sqlc-generated version silently dropped @sort_key inside
// CASE expressions, so this test pins that the ?N positional reuse
// works end-to-end against modernc.org/sqlite.
func TestListVideos_SortDimensions(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	if _, err := a.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "bc-1", BroadcasterLogin: "bc", BroadcasterName: "BC",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}

	// Three videos picked so each sort dimension has a unique winner
	// and every pairwise order is distinguishable.
	//                           duration  size    display_name   created (idx)
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

	// SQLite's datetime('now') default is second-precision, so rapid
	// inserts tie on start_download_at. Override timestamps explicitly
	// so the created_at-asc/desc assertions test the sort direction
	// rather than the id-DESC tiebreaker.
	base := time.Now().UTC().Truncate(time.Second)
	for i, name := range []string{"Alpha", "Bravo", "Charlie"} {
		if _, err := a.db.ExecContext(ctx,
			"UPDATE videos SET start_download_at = ? WHERE id = ?",
			sqlitetype.Format(base.Add(time.Duration(i)*time.Minute)),
			ids[name],
		); err != nil {
			t.Fatalf("override start_download_at for %s: %v", name, err)
		}
	}

	cases := []struct {
		name      string
		opts      repository.ListVideosOpts
		wantOrder []string // display names in expected order
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

func seedVideoListFilterFixture(t *testing.T, ctx context.Context, a *SQLiteAdapter) {
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
		if _, err := a.db.ExecContext(ctx, "UPDATE videos SET start_download_at = ? WHERE id = ?", sqlitetype.Format(startedAt), v.ID); err != nil {
			t.Fatalf("override start_download_at %s: %v", s.jobID, err)
		}
	}
}

func collectChannelPageLogins(t *testing.T, ctx context.Context, a *SQLiteAdapter, limit int, sort string, liveOnly bool) []string {
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

func seedVideoListPageFixture(t *testing.T, ctx context.Context, a *SQLiteAdapter) {
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
		if _, err := a.db.ExecContext(ctx, "UPDATE videos SET start_download_at = ? WHERE id = ?", sqlitetype.Format(s.startedAt), v.ID); err != nil {
			t.Fatalf("override start_download_at %s: %v", s.jobID, err)
		}
	}
}

func collectVideoListPageJobIDs(t *testing.T, ctx context.Context, a *SQLiteAdapter, opts repository.ListVideosOpts) []string {
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

// TestSearchChannels pins the ranking + filter contract. Split into
// subtests so an assertion failure in one case (e.g., rank order)
// doesn't mask the others. Covers the hand-rolled searchChannelsSQL
// path where ?1 is reused across WHERE and CASE — the specific sqlc
// SQLite limitation that forced hand-rolling.
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
// Exercises the hand-rolled searchCategoriesSQL specifically — ?1 is
// reused across WHERE and CASE, the same sqlc-SQLite limitation that
// forced SearchChannels to hand-roll.
func TestSearchCategories(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	seed := []repository.Category{
		{ID: "1", Name: "Valorant"},
		{ID: "2", Name: "Valheim"},
		{ID: "3", Name: "The Legend of Valor"},
		{ID: "4", Name: "Celeste"},
		{ID: "5", Name: "50% Off"}, // exercises LIKE wildcard edge case
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
}

// TestSearchCategories_ColumnRoundtrip pins scan column order for the
// hand-rolled searchCategoriesSQL, same pattern as the channels and
// videos variants. A schema migration that adds a column to
// categories needs to update this SQL + scan in the same commit;
// this test trips on mismatch.
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

// TestUpsertCategory_PreservesBoxArt pins the data-loss fix: when a
// caller upserts a category with only (id, name) — the streammeta
// Hydrator's webhook path — any box_art_url / igdb_id the category
// already has must survive. The SQL uses ifnull(excluded.*,
// categories.*) to get this right; without that fix, every stream.online
// for a category would wipe the art that the sync task had filled.
func TestUpsertCategory_PreservesBoxArt(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	// Seed with box art, as if the categoryart sync task had filled
	// it from /helix/games.
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

	// Webhook path: only id + name. Must update the name (Twitch may
	// rename a game) but MUST NOT wipe box_art_url or igdb_id.
	if _, err := a.UpsertCategory(ctx, &repository.Category{
		ID:   "g-1",
		Name: "New Name",
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

// TestUpdateCategoryBoxArt pins the explicit "refresh art" setter —
// the path categoryart.Service uses to write values pulled from
// /helix/games into the mirror.
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
// Exercises the sqlc.slice() IN (?) dynamic placeholder expansion.
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

// TestListChannelsByIDs exercises the sqlc.slice() path specifically —
// SQLite's IN (?) with dynamic placeholder expansion. Includes edge
// cases that would otherwise silently drift on refactor.
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
		gotIDs := map[string]bool{}
		for _, c := range got {
			gotIDs[c.BroadcasterID] = true
		}
		if !gotIDs["1"] || !gotIDs["3"] {
			t.Errorf("expected ids 1 and 3, got %v", gotIDs)
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
		// Caller passes the same ID twice; IN (?, ?) returns the row
		// once because SQL set semantics dedupe on the right side of IN.
		got, err := a.ListChannelsByIDs(ctx, []string{"1", "1", "2"})
		if err != nil {
			t.Fatalf("by dup ids: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("duplicates should collapse, got %d rows", len(got))
		}
	})
}

// TestListLatestLivePerChannel_OnePerBroadcaster confirms the
// ROW_NUMBER() window function picks exactly the most recent stream
// per broadcaster and that the outer ORDER BY sorts them newest-first
// globally. Also verifies the channel join returns display metadata.
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
	// Channel A: older + newer. Newest-per-channel should pick s-a-new.
	// Channel B: one stream, older than A's newest.
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
	// Newest-first: s-a-new (-30m) before s-b-1 (-2h).
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
	if saved.RecordingWebhookSecret != "" {
		t.Fatalf("config upsert must not set a secret, got %q", saved.RecordingWebhookSecret)
	}

	// Re-read through GetServerSettings (SELECT *) to confirm the columns are
	// readable by the path the config service and dispatcher actually use, and
	// that the enabled bool maps back from INTEGER 0/1.
	row, err := adapter.GetServerSettings(ctx)
	if err != nil {
		t.Fatalf("GetServerSettings: %v", err)
	}
	if !row.RecordingWebhookEnabled || row.RecordingWebhookURL != "https://hooks.example/recordings" {
		t.Fatalf("GetServerSettings did not reflect webhook config: %+v", row)
	}

	// Disabling round-trips the bool the other way.
	disabled, err := adapter.UpsertRecordingWebhookConfig(ctx, false, "", "")
	if err != nil {
		t.Fatalf("UpsertRecordingWebhookConfig (disable): %v", err)
	}
	if disabled.RecordingWebhookEnabled {
		t.Fatal("enabled should round-trip as false")
	}
}

// TestRecordingWebhookSecret_EnsureIsCASSetIsUnconditional pins the two secret
// writes against the SQLite adapter: ensure seeds only an empty slot, set
// rotates unconditionally, and a config save preserves whatever is stored.
func TestRecordingWebhookSecret_EnsureIsCASSetIsUnconditional(t *testing.T) {
	ctx := context.Background()
	adapter := newTestAdapter(t)

	if err := adapter.EnsureRecordingWebhookSecret(ctx, "first"); err != nil {
		t.Fatalf("EnsureRecordingWebhookSecret: %v", err)
	}
	if row, _ := adapter.GetServerSettings(ctx); row.RecordingWebhookSecret != "first" {
		t.Fatalf("ensure should seed an empty slot, got %q", row.RecordingWebhookSecret)
	}
	if err := adapter.EnsureRecordingWebhookSecret(ctx, "second"); err != nil {
		t.Fatalf("EnsureRecordingWebhookSecret (2): %v", err)
	}
	if row, _ := adapter.GetServerSettings(ctx); row.RecordingWebhookSecret != "first" {
		t.Fatalf("ensure must not overwrite an existing secret, got %q", row.RecordingWebhookSecret)
	}
	if err := adapter.SetRecordingWebhookSecret(ctx, "rotated"); err != nil {
		t.Fatalf("SetRecordingWebhookSecret: %v", err)
	}
	if row, _ := adapter.GetServerSettings(ctx); row.RecordingWebhookSecret != "rotated" {
		t.Fatalf("set should rotate, got %q", row.RecordingWebhookSecret)
	}
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

	// Saving server mode must not touch the webhook columns or the secret.
	if _, err := adapter.UpsertServerSettings(ctx, &repository.ServerSettings{ServerMode: "poll"}); err != nil {
		t.Fatalf("UpsertServerSettings: %v", err)
	}
	row, _ := adapter.GetServerSettings(ctx)
	if !row.RecordingWebhookEnabled || row.RecordingWebhookURL != "https://hooks.example/x" || row.RecordingWebhookSecret != "webhook-keep" {
		t.Fatalf("server-mode save clobbered webhook config: %+v", row)
	}

	// And saving the webhook config must not touch server mode or the secret.
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

// Crash recovery re-arms stale delivering rows without touching fresh or
// terminal deliveries.
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

		// The future cutoff makes time eligible; status must still filter rows.
		resetNow := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
		before := time.Now().UTC().Add(24 * time.Hour)
		if err := a.ResetStaleRecordingWebhookDeliveries(ctx, before, resetNow); err != nil {
			t.Fatalf("ResetStaleRecordingWebhookDeliveries: %v", err)
		}

		byID := deliveriesByID(t, a, ctx)
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
		if got := deliveriesByID(t, a, ctx)[fresh.ID].Status; got != repository.RecordingWebhookDeliveryDelivering {
			t.Fatalf("fresh delivering row status = %q, want delivering (not yet stale)", got)
		}
	})
}

func deliveriesByID(t *testing.T, a *SQLiteAdapter, ctx context.Context) map[int64]repository.RecordingWebhookDelivery {
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

// TestCreateClaimedRecordingWebhookDelivery_NotClaimable pins the SendTest
// double-delivery fix: a row created via CreateClaimedRecordingWebhookDelivery
// starts in 'delivering' (one attempt) and is therefore NOT picked up by the
// poller's claim, which only selects 'pending' — so the synchronous test send
// can't be re-delivered. A plain (pending) row is still claimable, by contrast.
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

	// Contrast: a pending row is claimable.
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
// cutoff are pruned, while recent terminal outcomes plus pending/delivering rows
// are kept so a queued, in-flight, or just-finished delivery is not lost.
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

	// delivered (terminal): create pending, claim, mark delivered.
	d1 := mkPending("recording.completed:1", 1)
	if _, err := adapter.ClaimDueRecordingWebhookDeliveries(ctx, now, 1); err != nil {
		t.Fatalf("claim d1: %v", err)
	}
	if err := adapter.MarkRecordingWebhookDeliveryDelivered(ctx, d1.ID, 200, now); err != nil {
		t.Fatalf("mark delivered: %v", err)
	}
	if _, err := adapter.db.ExecContext(ctx,
		"UPDATE recording_webhook_deliveries SET created_at = ?, updated_at = ?, delivered_at = ? WHERE id = ?",
		sqlitetype.Format(old), sqlitetype.Format(old), sqlitetype.Format(old), d1.ID); err != nil {
		t.Fatalf("backdate delivered row: %v", err)
	}
	// failed (terminal), created old but updated recently: must survive.
	d2 := mkPending("recording.completed:2", 2)
	if err := adapter.MarkRecordingWebhookDeliveryFinal(ctx, d2.ID, repository.RecordingWebhookDeliveryFailed, 500, "boom", now, now); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if _, err := adapter.db.ExecContext(ctx,
		"UPDATE recording_webhook_deliveries SET created_at = ? WHERE id = ?",
		sqlitetype.Format(old), d2.ID); err != nil {
		t.Fatalf("backdate failed row created_at: %v", err)
	}
	// pending (active) and delivering (active) — must survive.
	pending := mkPending("recording.completed:3", 3)
	if _, err := adapter.db.ExecContext(ctx,
		"UPDATE recording_webhook_deliveries SET created_at = ?, updated_at = ? WHERE id = ?",
		sqlitetype.Format(old), sqlitetype.Format(old), pending.ID); err != nil {
		t.Fatalf("backdate pending row: %v", err)
	}
	delivering, err := adapter.CreateClaimedRecordingWebhookDelivery(ctx, &repository.RecordingWebhookDeliveryInput{
		MessageID: "test:x", DedupeKey: "test:x", Event: "recording.test", Test: true, NextAttemptAt: now,
	})
	if err != nil {
		t.Fatalf("create claimed: %v", err)
	}
	if _, err := adapter.db.ExecContext(ctx,
		"UPDATE recording_webhook_deliveries SET created_at = ?, updated_at = ? WHERE id = ?",
		sqlitetype.Format(old), sqlitetype.Format(old), delivering.ID); err != nil {
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
// constraint: only failed/rejected rows re-queue (with a reset attempt budget);
// a delivered or missing row yields ErrNotFound and is left untouched, so a
// direct API caller can't reset a delivered/in-flight row into a duplicate send.
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

	// Delivered → not retryable.
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

	// Failed → retryable: back to pending, attempts and last_status reset.
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

	// Missing id → ErrNotFound.
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

func createWebhookOutboxVideo(t *testing.T, adapter *SQLiteAdapter, jobID string) *repository.Video {
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
