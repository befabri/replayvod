package contracttest

import (
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testUserLookupAndWhitelist(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	for _, u := range []repository.User{
		{ID: "alice", Login: "alice-login", DisplayName: "Alice", Role: "admin"},
		{ID: "bob", Login: "bob-login", DisplayName: "Bob", Role: "viewer"},
	} {
		if _, err := repo.UpsertUser(ctx, &u); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.GetUser(ctx, "alice")
	if err != nil || got.Login != "alice-login" || got.DisplayName != "Alice" || got.Role != "admin" {
		t.Fatalf("user = %+v, %v", got, err)
	}
	if _, err := repo.GetUser(ctx, "nobody"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing user: %v", err)
	}
	users, err := repo.ListUsers(ctx)
	if err != nil || len(users) != 2 {
		t.Fatalf("users = %+v, %v", users, err)
	}
	seen := map[string]bool{}
	for _, u := range users {
		seen[u.ID] = true
	}
	if !seen["alice"] || !seen["bob"] {
		t.Fatalf("users = %+v", users)
	}
	// The bootstrap seed adds the owner on every start, so a repeat must be
	// accepted rather than rejected as a duplicate.
	for _, id := range []string{"w-1", "w-2", "w-1"} {
		if err := repo.AddToWhitelist(ctx, id); err != nil {
			t.Fatalf("add %s: %v", id, err)
		}
	}
	if err := repo.RemoveFromWhitelist(ctx, "w-1"); err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveFromWhitelist(ctx, "w-1"); err != nil {
		t.Fatalf("removing an absent entry: %v", err)
	}
	if ok, err := repo.IsWhitelisted(ctx, "w-1"); err != nil || ok {
		t.Fatalf("removed entry still whitelisted: %v, %v", ok, err)
	}
	if ok, err := repo.IsWhitelisted(ctx, "w-2"); err != nil || !ok {
		t.Fatalf("remaining entry lost: %v, %v", ok, err)
	}
	entries, err := repo.ListWhitelist(ctx)
	if err != nil || len(entries) != 1 || entries[0].TwitchUserID != "w-2" || entries[0].AddedAt.IsZero() {
		t.Fatalf("whitelist = %+v, %v", entries, err)
	}
}

func testUserFollowsAndUnfollow(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "viewer", "chan-b")
	for _, id := range []string{"chan-a", "chan-c"} {
		if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: id, BroadcasterLogin: id, BroadcasterName: id}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	follow := func(broadcasterID string, followed bool) {
		t.Helper()
		if err := repo.UpsertUserFollow(ctx, &repository.UserFollow{UserID: "viewer", BroadcasterID: broadcasterID, FollowedAt: now, Followed: followed}); err != nil {
			t.Fatalf("follow %s: %v", broadcasterID, err)
		}
	}
	follow("chan-b", true)
	follow("chan-a", true)
	follow("chan-c", false)
	follows, err := repo.ListUserFollows(ctx, "viewer")
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, loginsOf(follows), []string{"chan-a", "chan-b"})
	if err := repo.UnfollowChannel(ctx, "viewer", "chan-a"); err != nil {
		t.Fatal(err)
	}
	if err := repo.UnfollowChannel(ctx, "viewer", "never-followed"); err != nil {
		t.Fatalf("unfollowing an unknown channel: %v", err)
	}
	follows, err = repo.ListUserFollows(ctx, "viewer")
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, loginsOf(follows), []string{"chan-b"})
	follow("chan-a", true)
	follows, err = repo.ListUserFollows(ctx, "viewer")
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, loginsOf(follows), []string{"chan-a", "chan-b"})
	if follows, err := repo.ListUserFollows(ctx, "nobody"); err != nil || len(follows) != 0 {
		t.Fatalf("follows of an unknown user = %+v, %v", follows, err)
	}
}

// testUserUpsertKeepsAssignedRole is the regression gate for the upsert
// leaving role out of its update: a returning user keeps the role an admin
// assigned rather than the default the Twitch sync passes on every login.
func testUserUpsertKeepsAssignedRole(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	email, profile := "test@example.com", "https://example.com/pic.png"
	created, err := repo.UpsertUser(ctx, &repository.User{ID: "12345", Login: "testuser", DisplayName: "TestUser", Email: &email, ProfileImageURL: &profile, Role: "viewer"})
	if err != nil || created.CreatedAt.IsZero() {
		t.Fatalf("created user = %+v, %v", created, err)
	}
	got, err := repo.GetUser(ctx, "12345")
	if err != nil || got.Login != "testuser" || got.Role != "viewer" || got.Email == nil || *got.Email != email || got.ProfileImageURL == nil || *got.ProfileImageURL != profile {
		t.Fatalf("user = %+v, %v", got, err)
	}
	updated, err := repo.UpsertUser(ctx, &repository.User{ID: "12345", Login: "testuser", DisplayName: "Renamed", Email: &email, ProfileImageURL: &profile, Role: "viewer"})
	if err != nil || updated.DisplayName != "Renamed" || !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("re-upserted user = %+v, %v; created %v", updated, err, created.CreatedAt)
	}
	if err := repo.UpdateUserRole(ctx, "12345", "admin"); err != nil {
		t.Fatal(err)
	}
	promoted, err := repo.UpsertUser(ctx, &repository.User{ID: "12345", Login: "testuser", DisplayName: "Renamed", Email: &email, ProfileImageURL: &profile, Role: "viewer"})
	if err != nil || promoted.Role != "admin" {
		t.Fatalf("upsert clobbered the assigned role: %+v, %v", promoted, err)
	}
}

// testUserReleaseLoginFreesRenamedAccount checks that a login Twitch has
// reassigned moves off the stale account without touching anything else, so
// the new owner can save it despite the unique login.
func testUserReleaseLoginFreesRenamedAccount(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	stale := &repository.User{ID: "renamed", Login: "streamer", DisplayName: "Old Streamer", Role: "admin"}
	bystander := &repository.User{ID: "bystander", Login: "bystander", DisplayName: "Bystander", Role: "viewer"}
	for _, u := range []*repository.User{stale, bystander} {
		if _, err := repo.UpsertUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.ReleaseUserLogin(ctx, "streamer", "new-owner"); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := repo.UpsertUser(ctx, &repository.User{ID: "new-owner", Login: "streamer", DisplayName: "Streamer", Role: "viewer"}); err != nil {
		t.Fatalf("save the login's new owner: %v", err)
	}
	got, err := repo.GetUser(ctx, "renamed")
	if err != nil || got.Login != "~renamed" || got.DisplayName != "Old Streamer" || got.Role != "admin" {
		t.Fatalf("released account = %+v, %v", got, err)
	}
	if got, err := repo.GetUser(ctx, "bystander"); err != nil || got.Login != "bystander" {
		t.Fatalf("unrelated account = %+v, %v", got, err)
	}
	// The owner's own row keeps its login, and releasing an unheld login is a no-op.
	for _, login := range []string{"streamer", "nobody-has-this"} {
		if err := repo.ReleaseUserLogin(ctx, login, "new-owner"); err != nil {
			t.Fatalf("release %s: %v", login, err)
		}
	}
	if got, err := repo.GetUser(ctx, "new-owner"); err != nil || got.Login != "streamer" {
		t.Fatalf("owner = %+v, %v", got, err)
	}
}
