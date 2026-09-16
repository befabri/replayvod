package contracttest

import (
	"errors"
	"maps"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testChannelLookupAndDelete(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "viewer", "beta")
	description := "plays chess"
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "alpha", BroadcasterLogin: "alpha", BroadcasterName: "Alpha", Description: &description, ViewCount: 7}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetChannel(ctx, "alpha")
	if err != nil || got.BroadcasterLogin != "alpha" || got.BroadcasterName != "Alpha" || got.Description == nil || *got.Description != description || got.ViewCount != 7 {
		t.Fatalf("channel = %+v, %v", got, err)
	}
	if byLogin, err := repo.GetChannelByLogin(ctx, "alpha"); err != nil || byLogin.BroadcasterID != "alpha" {
		t.Fatalf("channel by login = %+v, %v", byLogin, err)
	}
	if _, err := repo.GetChannel(ctx, "missing"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing channel: %v", err)
	}
	if _, err := repo.GetChannelByLogin(ctx, "missing"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("missing login: %v", err)
	}
	channels, err := repo.ListChannels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, loginsOf(channels), []string{"alpha", "beta"})
	if _, err := repo.SetChannelFavorite(ctx, "viewer", "alpha", true); err != nil {
		t.Fatal(err)
	}
	state, err := repo.GetChannelUserState(ctx, "viewer", "alpha")
	if err != nil || !state.Favorite || state.UserID != "viewer" || state.BroadcasterID != "alpha" {
		t.Fatalf("channel state = %+v, %v", state, err)
	}
	if _, err := repo.GetChannelUserState(ctx, "viewer", "beta"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("state without a row: %v", err)
	}
	if err := repo.DeleteChannel(ctx, "alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetChannel(ctx, "alpha"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("deleted channel survived: %v", err)
	}
	if _, err := repo.GetChannelUserState(ctx, "viewer", "alpha"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("channel state outlived its channel: %v", err)
	}
	channels, err = repo.ListChannels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, loginsOf(channels), []string{"beta"})
	if err := repo.DeleteChannel(ctx, "alpha"); err != nil {
		t.Fatalf("deleting a missing channel: %v", err)
	}
}

func testListChannelUserStatesForChannels(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "viewer", "alpha")
	SeedUserChannel(t, ctx, repo, "other", "beta")
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "gamma", BroadcasterLogin: "gamma", BroadcasterName: "gamma"}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct {
		user, channel string
		favorite      bool
	}{{"viewer", "alpha", true}, {"viewer", "beta", false}, {"other", "gamma", true}} {
		if _, err := repo.SetChannelFavorite(ctx, s.user, s.channel, s.favorite); err != nil {
			t.Fatal(err)
		}
	}
	favorites := func(userID string, ids ...string) map[string]bool {
		t.Helper()
		states, err := repo.ListChannelUserStatesForChannels(ctx, userID, ids)
		if err != nil {
			t.Fatal(err)
		}
		out := make(map[string]bool, len(states))
		for _, s := range states {
			if s.UserID != userID || s.CreatedAt.IsZero() || s.UpdatedAt.IsZero() {
				t.Fatalf("state %+v belongs to another user or lacks its timestamps", s)
			}
			out[s.BroadcasterID] = s.Favorite
		}
		if len(out) != len(states) {
			t.Fatalf("a channel repeats in %+v", states)
		}
		return out
	}
	if got := favorites("viewer"); len(got) != 0 {
		t.Fatalf("states for no channels = %v", got)
	}
	if got := favorites("", "alpha"); len(got) != 0 {
		t.Fatalf("states for no user = %v", got)
	}
	if got := favorites("viewer", "alpha", "beta", "gamma", "missing"); !maps.Equal(got, map[string]bool{"alpha": true, "beta": false}) {
		t.Fatalf("viewer states = %v", got)
	}
	if got := favorites("other", "alpha", "beta", "gamma"); !maps.Equal(got, map[string]bool{"gamma": true}) {
		t.Fatalf("other states = %v", got)
	}
	if got := favorites("viewer", "beta", "beta"); !maps.Equal(got, map[string]bool{"beta": false}) {
		t.Fatalf("states for a repeated channel = %v", got)
	}
}
