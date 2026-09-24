package stream

import (
	"testing"

	"github.com/befabri/replayvod/server/internal/twitch"
)

// TestToFollowedStreamResponse_PassThrough verifies the field mapping
// isn't reshuffled by an editor accident. Every field the frontend
// reads comes from the Helix Stream; if the converter ever drops one,
// this test catches it at the source.
func TestToFollowedStreamResponse_PassThrough(t *testing.T) {
	profile := "https://example.com/avatar.png"
	in := FollowedStream{
		Stream: twitch.Stream{
			ID:           "s-1",
			UserID:       "bc-1",
			UserLogin:    "login",
			UserName:     "Name",
			GameID:       "g-1",
			GameName:     "Game",
			Type:         "live",
			Title:        "Title",
			Language:     "en",
			ViewerCount:  1234,
			ThumbnailURL: "https://example.com/thumb.jpg",
			Tags:         []string{"tag1", "tag2"},
		},
		ProfileImageURL: &profile,
	}
	got := toFollowedStreamResponse(&in)
	if got.ProfileImageURL == nil || *got.ProfileImageURL != profile {
		t.Errorf("ProfileImageURL: got %v, want %q", got.ProfileImageURL, profile)
	}

	if got.StreamID != "s-1" {
		t.Errorf("StreamID: %q", got.StreamID)
	}
	if got.BroadcasterID != "bc-1" {
		t.Errorf("BroadcasterID: %q", got.BroadcasterID)
	}
	if got.BroadcasterLogin != "login" {
		t.Errorf("BroadcasterLogin: %q", got.BroadcasterLogin)
	}
	if got.BroadcasterName != "Name" {
		t.Errorf("BroadcasterName: %q", got.BroadcasterName)
	}
	if got.GameID != "g-1" || got.GameName != "Game" {
		t.Errorf("game fields: %+v", got)
	}
	if got.Title != "Title" || got.Language != "en" {
		t.Errorf("title/language: %+v", got)
	}
	if got.ViewerCount != 1234 {
		t.Errorf("ViewerCount: %d", got.ViewerCount)
	}
	if got.ThumbnailURL != "https://example.com/thumb.jpg" {
		t.Errorf("ThumbnailURL: %q", got.ThumbnailURL)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "tag1" || got.Tags[1] != "tag2" {
		t.Errorf("Tags: %v", got.Tags)
	}
}
