package system_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/server/api/system"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// TestSearchEventLogs_SQLiteFallback pins the fallback contract: on a
// backend that doesn't satisfy FullTextSearcher, SearchEventLogs still
// returns matching rows (substring, case-insensitive, across
// message/event_type/domain) but with Ranked=false so the UI knows not
// to render relevance scores.
func TestSearchEventLogs_SQLiteFallback(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(db)

	// sanity: SQLite adapter MUST NOT satisfy FullTextSearcher — the
	// whole point of the fallback is that this assertion fails.
	if _, ok := any(repo).(repository.FullTextSearcher); ok {
		t.Fatalf("SQLiteAdapter unexpectedly satisfies FullTextSearcher")
	}

	seed := []repository.EventLogInput{
		{Domain: "download", EventType: "failed", Severity: "error",
			Message: "download failed: network timeout"},
		{Domain: "auth", EventType: "refresh", Severity: "info",
			Message: "token refreshed"},
		{Domain: "schedule", EventType: "triggered", Severity: "info",
			Message: "triggered download for broadcaster foo"},
	}
	for i := range seed {
		if _, err := repo.CreateEventLog(ctx, &seed[i]); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	svc := system.New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))

	t.Run("message substring match", func(t *testing.T) {
		got, err := svc.SearchEventLogs(ctx, "timeout", 10, 0)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if got.Ranked {
			t.Fatalf("fallback path must set Ranked=false")
		}
		if got.Total != 1 || len(got.Results) != 1 {
			t.Fatalf("want 1 match, got total=%d len=%d", got.Total, len(got.Results))
		}
		if got.Results[0].EventType != "failed" {
			t.Fatalf("wrong row matched: %+v", got.Results[0])
		}
	})

	t.Run("case-insensitive domain match", func(t *testing.T) {
		got, err := svc.SearchEventLogs(ctx, "AUTH", 10, 0)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if got.Total != 1 {
			t.Fatalf("expected 1 auth match, got %d", got.Total)
		}
	})

	t.Run("empty query returns empty, not all rows", func(t *testing.T) {
		got, err := svc.SearchEventLogs(ctx, "", 10, 0)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if got.Total != 0 || len(got.Results) != 0 {
			t.Fatalf("empty query must not return rows: total=%d len=%d", got.Total, len(got.Results))
		}
	})

	t.Run("pagination slices matched set", func(t *testing.T) {
		// "download" hits 2 rows (event_type=download on row 0's message
		// + domain=download on row 0 + schedule message "triggered
		// download").
		all, err := svc.SearchEventLogs(ctx, "download", 10, 0)
		if err != nil {
			t.Fatalf("all: %v", err)
		}
		if all.Total < 2 {
			t.Fatalf("need ≥2 matches for pagination, got %d", all.Total)
		}
		page, err := svc.SearchEventLogs(ctx, "download", 1, 1)
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		if len(page.Results) != 1 {
			t.Fatalf("expected 1 row, got %d", len(page.Results))
		}
		if page.Results[0].ID != all.Results[1].ID {
			t.Fatalf("offset=1 should return all.Results[1]: want %d got %d",
				all.Results[1].ID, page.Results[0].ID)
		}
	})
}
