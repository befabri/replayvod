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
	got, err := repo.GetUserByLogin(ctx, "alice-login")
	if err != nil || got.ID != "alice" || got.DisplayName != "Alice" || got.Role != "admin" {
		t.Fatalf("user by login = %+v, %v", got, err)
	}
	if _, err := repo.GetUserByLogin(ctx, "nobody"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing login: %v", err)
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
