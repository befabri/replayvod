package sqliteadapter

import (
	"context"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/contracttest"
)

// TestListVideoMetadataChanges_AllowsLeftJoinNullTimestampProjections is
// SQLite-specific: it pins that the left-joined title/category projections
// survive NULL timestamp columns under SQLite's scan path, where a malformed
// or NULL sqlitetype.Time projection would otherwise hard-fail.
func TestListVideoMetadataChanges_AllowsLeftJoinNullTimestampProjections(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	videoID := contracttest.SeedMetadataVideo(t, ctx, a, "sqlite-meta-left-join", "sqlite-meta-left-join-job")
	at := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	if _, err := a.RecordVideoMetadataChange(ctx, repository.VideoMetadataChangeInput{
		VideoID:    videoID,
		OccurredAt: at,
		Title:      "Title only",
	}); err != nil {
		t.Fatalf("RecordVideoMetadataChange title-only: %v", err)
	}
	if _, err := a.RecordVideoMetadataChange(ctx, repository.VideoMetadataChangeInput{
		VideoID:      videoID,
		OccurredAt:   at.Add(time.Minute),
		CategoryID:   "game-left-join",
		CategoryName: "Category only",
	}); err != nil {
		t.Fatalf("RecordVideoMetadataChange category-only: %v", err)
	}

	events, err := a.ListVideoMetadataChanges(ctx, videoID)
	if err != nil {
		t.Fatalf("ListVideoMetadataChanges: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].Title == nil || events[0].Title.Name != "Title only" || events[0].Category != nil {
		t.Fatalf("title-only event = %+v", events[0])
	}
	if events[1].Title != nil || events[1].Category == nil || events[1].Category.ID != "game-left-join" {
		t.Fatalf("category-only event = %+v", events[1])
	}
}
