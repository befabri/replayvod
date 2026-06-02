package pgadapter

import (
	"context"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func TestRecordVideoMetadataChange_RoundTripsMediaOffset(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	videoID := seedMetadataVideo(t, ctx, a, "pg-meta-b1", "pg-meta-job-1")
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

func seedMetadataVideo(t *testing.T, ctx context.Context, a *PGAdapter, broadcasterID, jobID string) int64 {
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
