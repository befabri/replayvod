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
