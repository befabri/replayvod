package sqliteadapter

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/befabri/replayvod/server/internal/database"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

// migrationsDir returns the absolute path to server/migrations/sqlite,
// resolved from this test file's location so the test is invariant to the
// caller's cwd (go test from /, from the package dir, from CI, all work).
func migrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file = .../server/internal/repository/sqliteadapter/adapter_test.go
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations", "sqlite")
}

// openTestSQLite returns a fresh :memory: SQLite DB with all migrations
// applied. Using file::memory: plus cache=shared keeps the DB alive across
// multiple connections in the same test, but default MaxOpenConns=1 (set by
// database.NewSQLiteDB) means we don't actually need that — a simple
// connection-pooled :memory: is enough.
//
// The caller should use t.Cleanup for Close rather than defer: some test
// failures skip the defer path.
func openTestSQLite(t *testing.T) *sql.DB {
	t.Helper()
	// A fresh file in t.TempDir gives us a clean slate per test without the
	// cache=shared dance. modernc.org/sqlite supports :memory: but per-conn
	// isolation there is tricky; tempfile is simpler and fast enough.
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.NewSQLiteDB(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := database.MigrateSQLite(context.Background(), db, migrationsDir(t)); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// newTestAdapter wires up a fresh adapter against a fresh migrated DB.
func newTestAdapter(t *testing.T) *SQLiteAdapter {
	t.Helper()
	db := openTestSQLite(t)
	return New(sqlitegen.New(db))
}

// TestTimeRoundtrip exercises parseTime/formatTime directly. No DB involved —
// this is the cheapest regression signal if someone accidentally changes the
// format string.
//
// SQLite's "datetime('now')" default emits a second-precision TEXT with a
// space separator (`YYYY-MM-DD HH:MM:SS`). formatTime must produce exactly
// that layout so Go writes round-trip through the default-NOW columns, and
// parseTime must accept it back. Nanoseconds are dropped on the floor — that
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
			got := parseTime(formatTime(want))
			if !got.Equal(want) {
				t.Errorf("roundtrip mismatch: want %v, got %v (via %q)", want, got, formatTime(want))
			}
		})
	}
}

// TestTimeRoundtrip_DropsSubsecond documents that we intentionally discard
// sub-second precision. If we ever want to preserve it, this test will fail
// and force an explicit decision rather than a silent change.
func TestTimeRoundtrip_DropsSubsecond(t *testing.T) {
	withNs := time.Date(2026, 4, 12, 15, 30, 45, 123_456_789, time.UTC)
	got := parseTime(formatTime(withNs))
	want := withNs.Truncate(time.Second)
	if !got.Equal(want) {
		t.Errorf("expected truncation to second precision: want %v, got %v", want, got)
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
	if err := adapter.MarkVideoDone(ctx, vid.ID, 3600.5, 1_073_741_824, &thumb); err != nil {
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
