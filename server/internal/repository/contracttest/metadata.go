package contracttest

import (
	"context"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// SeedMetadataVideo upserts a channel and a running video for it, returning the
// video id. Exported because the SQLite adapter reuses it for its remaining
// backend-specific left-join projection test.
func SeedMetadataVideo(t *testing.T, ctx context.Context, repo repository.Repository, broadcasterID, jobID string) int64 {
	t.Helper()
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID:    broadcasterID,
		BroadcasterLogin: broadcasterID,
		BroadcasterName:  broadcasterID,
	}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	video, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID:         jobID,
		Filename:      jobID,
		DisplayName:   broadcasterID,
		Status:        repository.VideoStatusRunning,
		Quality:       repository.QualityHigh,
		BroadcasterID: broadcasterID,
		Language:      "en",
		RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("CreateVideo: %v", err)
	}
	return video.ID
}

// testVideoMetadataChangeRoundTripsMediaOffset pins that a recorded metadata
// change round-trips its media offset, title, and category through
// RecordVideoMetadataChange -> ListVideoMetadataChanges.
func testVideoMetadataChangeRoundTripsMediaOffset(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	videoID := SeedMetadataVideo(t, ctx, repo, "meta-b1", "meta-job-1")
	offset := 37.25
	occurredAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	if _, err := repo.RecordVideoMetadataChange(ctx, repository.VideoMetadataChangeInput{
		VideoID:            videoID,
		OccurredAt:         occurredAt,
		MediaOffsetSeconds: &offset,
		Title:              "New title",
		CategoryID:         "game-1",
		CategoryName:       "Game One",
	}); err != nil {
		t.Fatalf("RecordVideoMetadataChange: %v", err)
	}

	events, err := repo.ListVideoMetadataChanges(ctx, videoID)
	if err != nil {
		t.Fatalf("ListVideoMetadataChanges: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].MediaOffsetSeconds == nil || *events[0].MediaOffsetSeconds != offset {
		t.Fatalf("MediaOffsetSeconds = %v, want %v", events[0].MediaOffsetSeconds, offset)
	}
	if events[0].Title == nil || events[0].Title.Name != "New title" {
		t.Fatalf("Title = %+v, want New title", events[0].Title)
	}
	if events[0].Category == nil || events[0].Category.ID != "game-1" || events[0].Category.Name != "Game One" {
		t.Fatalf("Category = %+v, want game-1/Game One", events[0].Category)
	}
}
