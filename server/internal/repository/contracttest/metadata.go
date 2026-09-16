package contracttest

import (
	"context"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// SeedMetadataVideo creates a RUNNING recording owned by metadata-execution and
// returns its ID.
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
	if _, err := repo.CreateJob(ctx, &repository.JobInput{ID: jobID, VideoID: video.ID, BroadcasterID: broadcasterID}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetJobExecution(ctx, jobID, "metadata-execution", true); err != nil {
		t.Fatal(err)
	}
	return video.ID
}

func testVideoMetadataChangeRoundTripsMediaOffset(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	videoID := SeedMetadataVideo(t, ctx, repo, "meta-b1", "meta-job-1")
	offset := 37.25
	occurredAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	if _, err := repo.RecordVideoMetadataChange(ctx, repository.VideoMetadataChangeInput{
		VideoID: videoID,
		JobID:   "meta-job-1", ExecutionID: "metadata-execution",
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

// testVideoMetadataChangesProjectPartialObservations pins that an
// observation carrying only a title, or only a category, lists with the
// other side absent rather than failing on the empty join.
func testVideoMetadataChangesProjectPartialObservations(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	videoID := SeedMetadataVideo(t, ctx, repo, "meta-partial", "meta-partial-job")
	at := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if _, err := repo.RecordVideoMetadataChange(ctx, repository.VideoMetadataChangeInput{JobID: "meta-partial-job", ExecutionID: "metadata-execution", VideoID: videoID, OccurredAt: at, Title: "Title only"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordVideoMetadataChange(ctx, repository.VideoMetadataChangeInput{JobID: "meta-partial-job", ExecutionID: "metadata-execution", VideoID: videoID, OccurredAt: at.Add(time.Minute), CategoryID: "game-partial", CategoryName: "Category only"}); err != nil {
		t.Fatal(err)
	}
	events, err := repo.ListVideoMetadataChanges(ctx, videoID)
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %+v, %v", events, err)
	}
	if events[0].Title == nil || events[0].Title.Name != "Title only" || events[0].Category != nil {
		t.Fatalf("title-only event = %+v", events[0])
	}
	if events[1].Title != nil || events[1].Category == nil || events[1].Category.ID != "game-partial" {
		t.Fatalf("category-only event = %+v", events[1])
	}
}
