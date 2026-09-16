package contracttest

import (
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testEventLogListingAndCounts(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	for _, in := range []repository.EventLogInput{
		{Domain: "recording", EventType: "start", Severity: repository.EventLogSeverityInfo, Message: "started"},
		{Domain: "recording", EventType: "stop", Severity: repository.EventLogSeverityWarn, Message: "stopped early"},
		{Domain: "auth", EventType: "login", Severity: repository.EventLogSeverityError, Message: "denied"},
	} {
		if _, err := repo.CreateEventLog(ctx, &in); err != nil {
			t.Fatal(err)
		}
	}
	for domain, want := range map[string]int64{"recording": 2, "auth": 1, "storage": 0} {
		if n, err := repo.CountEventLogsByDomain(ctx, domain); err != nil || n != want {
			t.Fatalf("count of %s = %d, %v; want %d", domain, n, err, want)
		}
	}
	byDomain, err := repo.ListEventLogsByDomain(ctx, "recording", 10, 0)
	if err != nil || len(byDomain) != 2 {
		t.Fatalf("recording logs = %+v, %v", byDomain, err)
	}
	for _, e := range byDomain {
		if e.Domain != "recording" {
			t.Fatalf("recording logs = %+v", byDomain)
		}
	}
	if bySeverity, err := repo.ListEventLogsBySeverity(ctx, repository.EventLogSeverityError, 10, 0); err != nil || len(bySeverity) != 1 || bySeverity[0].Domain != "auth" || bySeverity[0].Message != "denied" {
		t.Fatalf("error logs = %+v, %v", bySeverity, err)
	}
	if all, err := repo.ListEventLogs(ctx, 10, 0); err != nil || len(all) != 3 {
		t.Fatalf("logs = %+v, %v", all, err)
	}
	if page, err := repo.ListEventLogs(ctx, 2, 0); err != nil || len(page) != 2 {
		t.Fatalf("first page = %+v, %v", page, err)
	}
	if page, err := repo.ListEventLogs(ctx, 2, 2); err != nil || len(page) != 1 {
		t.Fatalf("last page = %+v, %v", page, err)
	}
}

func testFetchLogListingByType(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	for _, fetchType := range []string{"helix", "helix", "eventsub"} {
		if err := repo.CreateFetchLog(ctx, &repository.FetchLogInput{FetchType: fetchType, Status: 200, DurationMs: 3}); err != nil {
			t.Fatal(err)
		}
	}
	helix, err := repo.ListFetchLogsByType(ctx, "helix", 10, 0)
	if err != nil || len(helix) != 2 {
		t.Fatalf("helix logs = %+v, %v", helix, err)
	}
	for _, l := range helix {
		if l.FetchType != "helix" || l.Status != 200 || l.DurationMs != 3 || l.FetchedAt.IsZero() {
			t.Fatalf("helix logs = %+v", helix)
		}
	}
	if page, err := repo.ListFetchLogsByType(ctx, "helix", 1, 1); err != nil || len(page) != 1 {
		t.Fatalf("second helix page = %+v, %v", page, err)
	}
	if eventsub, err := repo.ListFetchLogsByType(ctx, "eventsub", 10, 0); err != nil || len(eventsub) != 1 {
		t.Fatalf("eventsub logs = %+v, %v", eventsub, err)
	}
	if none, err := repo.ListFetchLogsByType(ctx, "none", 10, 0); err != nil || len(none) != 0 {
		t.Fatalf("logs of an unknown type = %+v, %v", none, err)
	}
	if n, err := repo.CountFetchLogsByType(ctx, "helix"); err != nil || n != 2 {
		t.Fatalf("helix count = %d, %v", n, err)
	}
}
