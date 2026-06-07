package contracttest

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// SeedUserChannel inserts a user and a channel sharing the given ids. Most
// schedule/subscription/playback fixtures FK into both, so tests seed them
// together. Exported because adapter packages reuse it for their remaining
// backend-specific tests.
func SeedUserChannel(t *testing.T, ctx context.Context, repo repository.Repository, userID, broadcasterID string) {
	t.Helper()
	if _, err := repo.UpsertUser(ctx, &repository.User{
		ID: userID, Login: userID, DisplayName: userID, Role: "viewer",
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID:    broadcasterID,
		BroadcasterLogin: broadcasterID,
		BroadcasterName:  broadcasterID,
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
}

// seedDonePlaybackVideo creates a finished recording with parts parts and
// returns its video id. Two-plus parts is what makes it eligible for a
// playback artifact.
func seedDonePlaybackVideo(t *testing.T, ctx context.Context, repo repository.Repository, jobID, filename, broadcasterID string, parts int) int64 {
	t.Helper()
	v, err := repo.CreateVideo(ctx, &repository.VideoInput{
		JobID: jobID, Filename: filename, DisplayName: broadcasterID,
		Status: repository.VideoStatusPending, Quality: repository.QualityHigh,
		BroadcasterID: broadcasterID, RecordingType: repository.RecordingTypeVideo,
	})
	if err != nil {
		t.Fatalf("CreateVideo: %v", err)
	}
	for i := 1; i <= parts; i++ {
		if _, err := repo.CreateVideoPart(ctx, &repository.VideoPartInput{
			VideoID: v.ID, PartIndex: int32(i), Filename: fmt.Sprintf("%s-part%02d.mp4", filename, i),
			Quality: "1080", Codec: repository.CodecH264, SegmentFormat: repository.SegmentFormatFMP4,
		}); err != nil {
			t.Fatalf("CreateVideoPart: %v", err)
		}
	}
	if err := repo.MarkVideoDone(ctx, v.ID, 60, 1024, nil, repository.CompletionKindComplete, false); err != nil {
		t.Fatalf("MarkVideoDone: %v", err)
	}
	return v.ID
}

// upsertReadyAsset upserts a ready playback asset for videoID with the given
// last-accessed timestamp, used to set up LRU-ordering scenarios.
func upsertReadyAsset(t *testing.T, ctx context.Context, repo repository.Repository, videoID int64, name string, lastAccessed time.Time) {
	t.Helper()
	mime := "video/mp4"
	dur, size := 60.0, int64(2048)
	if _, err := repo.UpsertVideoPlaybackAsset(ctx, &repository.VideoPlaybackAssetInput{
		VideoID: videoID, Status: repository.PlaybackAssetStatusReady,
		Filename: &name, MimeType: &mime, DurationSeconds: &dur, SizeBytes: &size,
		GeneratedAt: &lastAccessed, LastAccessedAt: &lastAccessed,
	}); err != nil {
		t.Fatalf("upsert ready asset: %v", err)
	}
}

// jsonEqual reports whether a and b marshal to the same JSON, used to compare
// payloads semantically (PG may reformat JSONB whitespace/key order).
func jsonEqual(a, b any) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}

// assertStringSlice fails the test unless got equals want element-for-element.
func assertStringSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("slice len = %d, want %d: got %v want %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slice[%d] = %q, want %q (got %v want %v)", i, got[i], want[i], got, want)
		}
	}
}
