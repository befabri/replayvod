package main

import "testing"

func TestPascalCase(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"user_id", "UserID"},
		{"created_at", "CreatedAt"},
		{"profile_image_url", "ProfileImageURL"},
		{"broadcaster_id", "BroadcasterID"},
		{"box_art_url", "BoxArtURL"},
		{"igdb_id", "IGDBID"},
		{"get-users", "GetUsers"},
		{"create-eventsub-subscription", "CreateEventSubSubscription"},
		{"get-channel-information", "GetChannelInformation"},
		{"id", "ID"},
		{"", ""},
	}
	for _, c := range cases {
		got := PascalCase(c.in)
		if got != c.want {
			t.Errorf("PascalCase(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestCamelCase(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"user_id", "userID"},
		{"profile_image_url", "profileImageURL"},
		{"id", "id"},
	}
	for _, c := range cases {
		got := CamelCase(c.in)
		if got != c.want {
			t.Errorf("CamelCase(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}
