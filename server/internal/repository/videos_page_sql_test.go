package repository

import (
	"strings"
	"testing"
	"time"
)

func strptr(s string) *string { return &s }

func TestBuildListVideosPageQuery_PlaceholdersMatchArgs(t *testing.T) {
	dur := 100.0
	sz := int64(2000)
	cursor := &VideoListPageCursor{
		StartDownloadAt: time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC),
		ID:              5,
		SortNumber:      &dur,
		SortInt:         &sz,
		SortText:        strptr("chan"),
	}
	base := ListVideosOpts{
		Status:             "DONE",
		Quality:            "1080p60",
		BroadcasterID:      "bc-1",
		Language:           "en",
		DurationMinSeconds: &dur,
		DurationMaxSeconds: &dur,
		SizeMinBytes:       &sz,
		SizeMaxBytes:       &sz,
		Window:             "this_week",
		IncompleteOnly:     true,
		Limit:              24,
	}
	pg := VideoPageDialect{Postgres: true, FormatTime: func(t time.Time) any { return t }}
	sqlite := VideoPageDialect{Postgres: false, FormatTime: func(t time.Time) any { return t.String() }}

	sorts := []struct{ sort, order string }{
		{"created_at", "desc"}, {"created_at", "asc"},
		{"history_when", "desc"}, {"history_when", "asc"},
		{"channel", "asc"}, {"channel", "desc"},
		{"duration", "asc"}, {"duration", "desc"},
		{"size", "asc"}, {"size", "desc"},
	}
	for _, cur := range []*VideoListPageCursor{cursor, nil} {
		for _, s := range sorts {
			opts := base
			opts.Sort, opts.Order = s.sort, s.order
			name := s.sort + "-" + s.order
			if cur == nil {
				name += "/nil-cursor"
			}
			t.Run(name, func(t *testing.T) {
				q, args := BuildListVideosPageQuery(opts, cur, sqlite)
				if c := strings.Count(q, "?"); c != len(args) {
					t.Fatalf("sqlite: %d placeholders but %d args:\n%s", c, len(args), q)
				}
				q2, args2 := BuildListVideosPageQuery(opts, cur, pg)
				if c := strings.Count(q2, "$"); c != len(args2) {
					t.Fatalf("pg: %d placeholders but %d args:\n%s", c, len(args2), q2)
				}
			})
		}
	}
}

func TestBuildListVideosPageQuery_HistoryContracts(t *testing.T) {
	opts := ListVideosOpts{
		Sort:         "history_when",
		Order:        "desc",
		TerminalOnly: true,
		Scope:        "all",
		Limit:        10,
	}
	q, _ := BuildListVideosPageQuery(opts, nil, VideoPageDialect{Postgres: true, FormatTime: func(t time.Time) any { return t }})

	if !strings.Contains(q, "status IN ('DONE', 'FAILED')") {
		t.Fatalf("terminal-only predicate missing:\n%s", q)
	}
	if !strings.Contains(q, "COALESCE(deleted_at, downloaded_at, start_download_at) DESC") {
		t.Fatalf("history_when sort missing:\n%s", q)
	}
	if strings.Contains(q, "deleted_at IS NULL") || strings.Contains(q, "deleted_at IS NOT NULL") {
		t.Fatalf("scope=all should not add a tombstone predicate:\n%s", q)
	}
}

func TestBuildListVideosPageQuery_DialectFragments(t *testing.T) {
	opts := ListVideosOpts{Status: "DONE", Window: "this_week", IncompleteOnly: true, Limit: 10}
	pg, _ := BuildListVideosPageQuery(opts, nil, VideoPageDialect{Postgres: true, FormatTime: func(t time.Time) any { return t }})
	sq, _ := BuildListVideosPageQuery(opts, nil, VideoPageDialect{Postgres: false, FormatTime: func(t time.Time) any { return t }})

	if !strings.Contains(pg, "now() - interval '7 days'") {
		t.Errorf("pg window predicate missing now()-interval:\n%s", pg)
	}
	if !strings.Contains(sq, "datetime('now', '-7 days')") {
		t.Errorf("sqlite window predicate missing datetime():\n%s", sq)
	}
	if !strings.Contains(pg, "NOT $") || !strings.Contains(pg, "::boolean") {
		t.Errorf("pg incomplete-only should be NOT $N::boolean:\n%s", pg)
	}
	if !strings.Contains(sq, "truncated = 1") {
		t.Errorf("sqlite incomplete-only should compare truncated = 1:\n%s", sq)
	}
	if !strings.Contains(pg, "::timestamptz") {
		t.Errorf("pg should cast timestamp binds:\n%s", pg)
	}
	if strings.Contains(sq, "::") {
		t.Errorf("sqlite should emit no :: casts:\n%s", sq)
	}
}
