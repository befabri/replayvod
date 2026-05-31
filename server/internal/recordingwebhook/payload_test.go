package recordingwebhook

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// testSignURL is a deterministic stand-in for the real videodownload signer, so
// payload tests can assert the per-part URL wiring without HMAC noise.
func testSignURL(videoID int64, partIndex int32, _ *time.Time) string {
	return fmt.Sprintf("https://app.example/dl/%d/%d", videoID, partIndex)
}

func sampleVideo() *repository.Video {
	dur := 3600.0
	size := int64(1024)
	ended := time.Date(2026, 5, 30, 13, 0, 0, 0, time.UTC)
	return &repository.Video{
		ID:              42,
		Status:          repository.VideoStatusDone,
		CompletionKind:  repository.CompletionKindComplete,
		Truncated:       false,
		BroadcasterID:   "555",
		Title:           "Speedrunning",
		StartDownloadAt: time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
		DownloadedAt:    &ended,
		DurationSeconds: &dur,
		SizeBytes:       &size,
	}
}

func TestBuildPayload_completedIncludesPerPartDownloadURLsAndDecoration(t *testing.T) {
	store := &fakeRepo{
		video: sampleVideo(),
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4", SizeBytes: 600, DurationSeconds: 1800},
			{PartIndex: 2, Filename: "vod-42-02.mp4", SizeBytes: 424, DurationSeconds: 1800},
		},
		channel:    &repository.Channel{BroadcasterLogin: "speedy", BroadcasterName: "Speedy"},
		categories: map[int64]repository.Category{42: {ID: "12", Name: "Celeste"}},
	}

	p, err := buildPayload(context.Background(), store, testSignURL, EventCompleted, 42, nil, true)
	if err != nil {
		t.Fatalf("buildPayload: %v", err)
	}
	if p.Version != PayloadVersion {
		t.Fatalf("version = %d, want %d", p.Version, PayloadVersion)
	}
	if p.Event != EventCompleted || p.VideoID != 42 || p.Status != repository.VideoStatusDone {
		t.Fatalf("core fields wrong: %+v", p)
	}
	if p.BroadcasterLogin != "speedy" || p.BroadcasterName != "Speedy" {
		t.Fatalf("broadcaster decoration missing: %+v", p)
	}
	if p.Category != "Celeste" {
		t.Fatalf("category = %q, want Celeste", p.Category)
	}
	if p.Title != "Speedrunning" {
		t.Fatalf("title = %q", p.Title)
	}
	// Every part gets its own signed download URL, not just the first.
	if len(p.Parts) != 2 || p.Parts[0].Path != "videos/vod-42-01.mp4" || p.Parts[1].SizeBytes != 424 {
		t.Fatalf("parts wrong: %+v", p.Parts)
	}
	if p.Parts[0].DownloadURL != "https://app.example/dl/42/1" || p.Parts[1].DownloadURL != "https://app.example/dl/42/2" {
		t.Fatalf("per-part download URLs wrong: %+v", p.Parts)
	}
}

func TestBuildPayload_failedCarriesErrorAndNoParts(t *testing.T) {
	v := sampleVideo()
	v.Status = repository.VideoStatusFailed
	v.DownloadedAt = nil
	v.DurationSeconds = nil
	v.SizeBytes = nil
	errMsg := "auth failed"
	v.Error = &errMsg

	store := &fakeRepo{video: v}
	p, err := buildPayload(context.Background(), store, testSignURL, EventFailed, 42, nil, true)
	if err != nil {
		t.Fatalf("buildPayload: %v", err)
	}
	if p.Error == nil || *p.Error != "auth failed" {
		t.Fatalf("error field = %v, want \"auth failed\"", p.Error)
	}
	if p.EndedAt != nil {
		t.Fatal("ended_at should be nil for a failure that never finalized")
	}
	if len(p.Parts) != 0 {
		t.Fatalf("expected no parts, got %v", p.Parts)
	}
}

// TestBuildPayload_failedOmitsDownloadURLs guards that a recording.failed
// payload never advertises signed download URLs: the signed route serves only
// DONE videos, so a download_url on a FAILED (even partial) recording would
// 404. The signer is provided, yet no part may carry a URL.
func TestBuildPayload_failedOmitsDownloadURLs(t *testing.T) {
	v := sampleVideo()
	v.Status = repository.VideoStatusFailed
	v.CompletionKind = repository.CompletionKindPartial // finalized parts exist
	store := &fakeRepo{
		video: v,
		parts: []repository.VideoPart{
			{PartIndex: 1, Filename: "vod-42-01.mp4", SizeBytes: 600, DurationSeconds: 1800},
		},
	}
	p, err := buildPayload(context.Background(), store, testSignURL, EventFailed, 42, nil, true)
	if err != nil {
		t.Fatalf("buildPayload: %v", err)
	}
	if len(p.Parts) != 1 {
		t.Fatalf("partial failure should still list its parts, got %d", len(p.Parts))
	}
	if p.Parts[0].DownloadURL != "" {
		t.Fatalf("recording.failed must not carry a download_url, got %q", p.Parts[0].DownloadURL)
	}
	// The storage-relative path is still present for a co-located consumer.
	if p.Parts[0].Path != "videos/vod-42-01.mp4" {
		t.Fatalf("path = %q", p.Parts[0].Path)
	}
}

func TestBuildPayload_completedCapsDownloadURLToRetentionDeadline(t *testing.T) {
	v := sampleVideo()
	retentionHours := int64(2)
	v.RetentionWindowHours = &retentionHours
	store := &fakeRepo{
		video: v,
		parts: []repository.VideoPart{{PartIndex: 1, Filename: "vod-42-01.mp4"}},
	}
	var gotDeadline *time.Time
	signURL := func(videoID int64, partIndex int32, notAfter *time.Time) string {
		if notAfter != nil {
			copied := *notAfter
			gotDeadline = &copied
		}
		return fmt.Sprintf("https://app.example/dl/%d/%d", videoID, partIndex)
	}

	p, err := buildPayload(context.Background(), store, signURL, EventCompleted, 42, nil, true)
	if err != nil {
		t.Fatalf("buildPayload: %v", err)
	}
	if len(p.Parts) != 1 || p.Parts[0].DownloadURL == "" {
		t.Fatalf("expected capped download URL, got %+v", p.Parts)
	}
	wantDeadline := v.DownloadedAt.Add(2 * time.Hour)
	if gotDeadline == nil || !gotDeadline.Equal(wantDeadline) {
		t.Fatalf("download deadline = %v, want %v", gotDeadline, wantDeadline)
	}
}

func TestBuildPayload_completedDoesNotCapDownloadURLWhenRetentionCapDisabled(t *testing.T) {
	v := sampleVideo()
	retentionHours := int64(2)
	v.RetentionWindowHours = &retentionHours
	store := &fakeRepo{
		video: v,
		parts: []repository.VideoPart{{PartIndex: 1, Filename: "vod-42-01.mp4"}},
	}
	calls := 0
	signURL := func(videoID int64, partIndex int32, notAfter *time.Time) string {
		calls++
		if notAfter != nil {
			t.Fatalf("notAfter = %v, want nil when retention cap is disabled", notAfter)
		}
		return fmt.Sprintf("https://app.example/dl/%d/%d", videoID, partIndex)
	}

	p, err := buildPayload(context.Background(), store, signURL, EventCompleted, 42, nil, false)
	if err != nil {
		t.Fatalf("buildPayload: %v", err)
	}
	if calls != 1 {
		t.Fatalf("signer calls = %d, want 1", calls)
	}
	if len(p.Parts) != 1 || p.Parts[0].DownloadURL != "https://app.example/dl/42/1" {
		t.Fatalf("expected uncapped download URL, got %+v", p.Parts)
	}
}

func TestBuildPayload_completedDeletedVideoOmitsDownloadURLs(t *testing.T) {
	v := sampleVideo()
	deletedAt := time.Date(2026, 5, 30, 14, 0, 0, 0, time.UTC)
	v.DeletedAt = &deletedAt
	store := &fakeRepo{
		video: v,
		parts: []repository.VideoPart{{PartIndex: 1, Filename: "vod-42-01.mp4"}},
	}
	calls := 0
	signURL := func(videoID int64, partIndex int32, notAfter *time.Time) string {
		calls++
		return fmt.Sprintf("https://app.example/dl/%d/%d", videoID, partIndex)
	}

	p, err := buildPayload(context.Background(), store, signURL, EventCompleted, 42, nil, true)
	if err != nil {
		t.Fatalf("buildPayload: %v", err)
	}
	if calls != 0 {
		t.Fatalf("signer called %d times for a deleted video, want 0", calls)
	}
	if len(p.Parts) != 1 || p.Parts[0].DownloadURL != "" {
		t.Fatalf("deleted video must not carry a download URL, got %+v", p.Parts)
	}
}

func TestBuildPayload_nilSignerOmitsDownloadURLs(t *testing.T) {
	store := &fakeRepo{
		video: sampleVideo(),
		parts: []repository.VideoPart{{PartIndex: 1, Filename: "vod-42-01.mp4"}},
	}
	// A nil signer (signed URLs disabled or no resolvable origin) leaves the
	// part's download URL empty; the storage path is still present.
	p, err := buildPayload(context.Background(), store, nil, EventCompleted, 42, nil, true)
	if err != nil {
		t.Fatalf("buildPayload: %v", err)
	}
	if len(p.Parts) != 1 || p.Parts[0].DownloadURL != "" {
		t.Fatalf("expected no download URL, got %+v", p.Parts)
	}
	if p.Parts[0].Path != "videos/vod-42-01.mp4" {
		t.Fatalf("path = %q", p.Parts[0].Path)
	}
}

func TestBuildPayload_missingChannelIsBestEffort(t *testing.T) {
	store := &fakeRepo{
		video:      sampleVideo(),
		channelErr: repository.ErrNotFound,
	}
	p, err := buildPayload(context.Background(), store, nil, EventCompleted, 42, nil, true)
	if err != nil {
		t.Fatalf("a missing channel must not abandon the delivery: %v", err)
	}
	if p.BroadcasterLogin != "" || p.BroadcasterName != "" {
		t.Fatalf("expected empty broadcaster decoration, got %+v", p)
	}
}

func TestBuildPayload_videoLoadErrorPropagates(t *testing.T) {
	store := &fakeRepo{videoErr: repository.ErrNotFound}
	if _, err := buildPayload(context.Background(), store, nil, EventCompleted, 7, nil, true); err == nil {
		t.Fatal("a missing video must abandon the delivery")
	}
}
