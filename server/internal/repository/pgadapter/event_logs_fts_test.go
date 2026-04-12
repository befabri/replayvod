package pgadapter

import (
	"context"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

// TestSearchEventLogs_RankingAndNegation pins the tsquery semantics the
// dashboard relies on: phrase match beats individual-token match, and
// `-term` excludes rows containing that token.
func TestSearchEventLogs_RankingAndNegation(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	seed := []repository.EventLogInput{
		{Domain: "download", EventType: "failed", Severity: "error",
			Message: "download failed: auth token refresh exhausted"},
		{Domain: "download", EventType: "succeeded", Severity: "info",
			Message: "download succeeded in 42 seconds"},
		{Domain: "auth", EventType: "refresh", Severity: "warn",
			Message: "token refresh retried twice before succeeding"},
		{Domain: "schedule", EventType: "triggered", Severity: "info",
			Message: "scheduled download triggered for channel foo"},
	}
	for i := range seed {
		if _, err := a.CreateEventLog(ctx, &seed[i]); err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}

	t.Run("phrase match outranks individual tokens", func(t *testing.T) {
		results, total, err := a.SearchEventLogs(ctx, `"download failed"`, 10, 0)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if total < 1 {
			t.Fatalf("expected at least 1 match, got %d", total)
		}
		if len(results) == 0 || results[0].EventType != "failed" {
			t.Fatalf("expected 'download failed' row first, got %+v", results)
		}
	})

	t.Run("negation excludes matching rows", func(t *testing.T) {
		results, _, err := a.SearchEventLogs(ctx, "download -failed", 10, 0)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		for _, r := range results {
			if r.EventType == "failed" {
				t.Fatalf("negation should have excluded failed row: %+v", r)
			}
		}
		// Still want to have returned something relevant.
		if len(results) == 0 {
			t.Fatalf("expected at least one download-but-not-failed match")
		}
	})

	t.Run("boolean OR unions token matches", func(t *testing.T) {
		_, total, err := a.SearchEventLogs(ctx, "auth OR schedule", 10, 0)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		// Two rows mention auth/schedule in message or domain/event_type.
		if total < 2 {
			t.Fatalf("expected ≥2 OR matches, got %d", total)
		}
	})

	t.Run("empty query returns zero, not all", func(t *testing.T) {
		results, total, err := a.SearchEventLogs(ctx, "", 10, 0)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if total != 0 || len(results) != 0 {
			t.Fatalf("empty query must not match: got total=%d results=%d", total, len(results))
		}
	})

	t.Run("pagination slices the ranked result set", func(t *testing.T) {
		all, total, err := a.SearchEventLogs(ctx, "download", 10, 0)
		if err != nil {
			t.Fatalf("full: %v", err)
		}
		if total < 2 {
			t.Fatalf("need at least 2 matches for pagination test, got %d", total)
		}
		page, _, err := a.SearchEventLogs(ctx, "download", 1, 1)
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		if len(page) != 1 {
			t.Fatalf("expected page size 1, got %d", len(page))
		}
		if page[0].ID != all[1].ID {
			t.Fatalf("offset 1 should return all[1]: want id=%d got id=%d", all[1].ID, page[0].ID)
		}
	})
}

// TestSearchEventLogs_InterfaceAssertion pins the type-assert contract:
// services use `repo.(repository.FullTextSearcher)` to detect support,
// and only the PG adapter should satisfy it.
func TestSearchEventLogs_InterfaceAssertion(t *testing.T) {
	var r repository.Repository = newTestAdapter(t)
	if _, ok := r.(repository.FullTextSearcher); !ok {
		t.Fatalf("PGAdapter must satisfy FullTextSearcher")
	}
}
