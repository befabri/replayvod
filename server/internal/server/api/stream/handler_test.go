package stream

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/befabri/replayvod/server/internal/twitch"
)

// TestToFollowedStreamResponse_TagsNilNormalized pins the review-fix:
// Helix omitting the tags field leaves twitch.Stream.Tags == nil, but
// a nil slice JSON-marshals to null. Frontends that iterate the array
// would have to null-check. The converter normalizes nil → []string{}
// so the wire shape is consistent.
func TestToFollowedStreamResponse_TagsNilNormalized(t *testing.T) {
	f := FollowedStream{Stream: twitch.Stream{ID: "s-1", UserID: "bc-1", Tags: nil}}
	got := toFollowedStreamResponse(&f)
	if got.Tags == nil {
		t.Fatal("Tags nil after normalization")
	}
	if len(got.Tags) != 0 {
		t.Errorf("expected empty Tags, got %v", got.Tags)
	}

	// The JSON shape is what the frontend sees; verify explicitly.
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), `"tags":null`) {
		t.Errorf(`expected "tags":[] in JSON, got %s`, raw)
	}
	// tags field should be omitted (omitempty) when empty, since we set
	// Tags: []string{} which is falsy for omitempty on slices only if
	// nil — []string{} with omitempty still omits. Document either way.
	if strings.Contains(string(raw), `"tags":null`) {
		t.Errorf(`tags should never be null, got %s`, raw)
	}
}

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

func TestIsTwitchViewerAuthError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "401", err: &twitch.HelixError{Status: http.StatusUnauthorized}, want: true},
		{name: "403", err: &twitch.HelixError{Status: http.StatusForbidden}, want: true},
		{name: "500", err: &twitch.HelixError{Status: http.StatusInternalServerError}, want: false},
		{name: "wrapped 401", err: errors.New("wrap: " + (&twitch.HelixError{Status: http.StatusUnauthorized}).Error()), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := twitch.IsUserAuthError(tt.err)
			if got != tt.want {
				t.Fatalf("twitch.IsUserAuthError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}

	wrapped := errors.Join(errors.New("context"), &twitch.HelixError{Status: http.StatusUnauthorized})
	if !twitch.IsUserAuthError(wrapped) {
		t.Fatal("joined helix 401 should be treated as auth error")
	}
}
