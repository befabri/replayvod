package schedule

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/internal/twitch"
)

// TestProcess_StreamOffline_EndsLastActiveStream confirms the Phase 5
// follow-up from the spec: on stream.offline, find the broadcaster's
// most recent active stream row and stamp ended_at. Idempotent on
// retries — the second call finds the already-ended row and returns
// without error.
func TestProcess_StreamOffline_EndsLastActiveStream(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewSQLiteDB(t)
	repo := sqliteadapter.New(sqlitegen.New(db))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := NewEventProcessor(repo, nil, nil, nil, log)

	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID: "b-off", BroadcasterLogin: "b", BroadcasterName: "b",
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	stream, err := repo.UpsertStream(ctx, &repository.StreamInput{
		ID: "s-1", BroadcasterID: "b-off", Type: "live", Language: "en",
		ViewerCount: 100, StartedAt: time.Now().UTC().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("seed stream: %v", err)
	}

	n := &twitch.EventSubNotification{
		MessageType: twitch.MsgTypeNotification,
		Event: twitch.StreamOfflineEvent{
			BroadcasterUserID:    "b-off",
			BroadcasterUserLogin: "b",
			BroadcasterUserName:  "b",
		},
	}
	if err := p.Process(ctx, n); err != nil {
		t.Fatalf("process: %v", err)
	}

	got, err := repo.GetStream(ctx, stream.ID)
	if err != nil {
		t.Fatalf("get stream: %v", err)
	}
	if got.EndedAt == nil {
		t.Fatal("EndedAt must be set after stream.offline")
	}

	// Idempotency: re-processing the same offline event must not error
	// and must not re-stamp ended_at backwards.
	firstEnd := *got.EndedAt
	time.Sleep(10 * time.Millisecond)
	if err := p.Process(ctx, n); err != nil {
		t.Fatalf("re-process: %v", err)
	}
	got2, _ := repo.GetStream(ctx, stream.ID)
	if got2.EndedAt == nil || !got2.EndedAt.Equal(firstEnd) {
		t.Errorf("ended_at must be stable across retries: first %v second %v", firstEnd, got2.EndedAt)
	}
}

// TestHighestQuality_PicksHighRankAndBreaksTiesByID pins the winner-
// selection rule from .docs/spec/eventsub.md: on a stream.online match
// the processor must trigger ONE download at the highest-quality matching
// schedule's quality. Without this, two matching schedules race on the
// downloader's busy-check — whichever Start() call wins decides the
// quality, and if the lower-quality one wins the VOD gets recorded at
// the wrong setting.
//
// The ID-based tie-break keeps the choice deterministic across retries
// of the same event.
func TestHighestQuality_PicksHighRankAndBreaksTiesByID(t *testing.T) {
	cases := []struct {
		name   string
		input  []*repository.DownloadSchedule
		wantID int64
	}{
		{
			name: "single",
			input: []*repository.DownloadSchedule{
				{ID: 7, Quality: repository.QualityMedium},
			},
			wantID: 7,
		},
		{
			name: "HIGH beats MEDIUM beats LOW regardless of order",
			input: []*repository.DownloadSchedule{
				{ID: 3, Quality: repository.QualityLow},
				{ID: 1, Quality: repository.QualityHigh},
				{ID: 2, Quality: repository.QualityMedium},
			},
			wantID: 1,
		},
		{
			name: "ties on HIGH break to lower ID",
			input: []*repository.DownloadSchedule{
				{ID: 42, Quality: repository.QualityHigh},
				{ID: 7, Quality: repository.QualityHigh},
				{ID: 100, Quality: repository.QualityHigh},
			},
			wantID: 7,
		},
		{
			name: "unknown quality sorts below known values",
			input: []*repository.DownloadSchedule{
				{ID: 5, Quality: "BOGUS"},
				{ID: 9, Quality: repository.QualityLow},
			},
			wantID: 9,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := highestQuality(tc.input)
			if got.ID != tc.wantID {
				t.Errorf("got ID=%d, want %d (quality=%q)", got.ID, tc.wantID, got.Quality)
			}
		})
	}
}
