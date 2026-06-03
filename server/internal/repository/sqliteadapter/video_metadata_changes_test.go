package sqliteadapter

import (
	"context"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func TestRecordVideoMetadataChange_RoundTripsMediaOffset(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	videoID := seedMetadataVideo(t, ctx, a, "sqlite-meta-b1", "sqlite-meta-job-1")
	offset := 37.25
	occurredAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	if _, err := a.RecordVideoMetadataChange(ctx, repository.VideoMetadataChangeInput{
		VideoID:            videoID,
		OccurredAt:         occurredAt,
		MediaOffsetSeconds: &offset,
		Title:              "New title",
		CategoryID:         "game-1",
		CategoryName:       "Game One",
	}); err != nil {
		t.Fatalf("RecordVideoMetadataChange: %v", err)
	}

	events, err := a.ListVideoMetadataChanges(ctx, videoID)
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

func TestListVideoMetadataChanges_AllowsLeftJoinNullTimestampProjections(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	videoID := seedMetadataVideo(t, ctx, a, "sqlite-meta-left-join", "sqlite-meta-left-join-job")
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

func seedMetadataVideo(t *testing.T, ctx context.Context, a *SQLiteAdapter, broadcasterID, jobID string) int64 {
	t.Helper()
	if _, err := a.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID:    broadcasterID,
		BroadcasterLogin: broadcasterID,
		BroadcasterName:  broadcasterID,
	}); err != nil {
		t.Fatalf("UpsertChannel: %v", err)
	}
	video, err := a.CreateVideo(ctx, &repository.VideoInput{
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
