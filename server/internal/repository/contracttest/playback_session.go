package contracttest

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

func testTwitchPlaybackSession(t *testing.T, h Harness) {
	ctx, repo := context.Background(), h.Repo()
	if _, err := repo.GetTwitchPlaybackSession(ctx); !errors.Is(err, repository.ErrNotFound) {
		t.Fatal("missing connection should be not found")
	}
	row := &repository.TwitchPlaybackSession{TwitchUserID: "123", TwitchLogin: "first", EncryptedToken: []byte("opaque-ciphertext"), CheckedAt: 100, ExpiresAt: 200}
	if err := repo.SaveTwitchPlaybackSession(ctx, row); err != nil {
		t.Fatal(err)
	}
	row.NeedsReconnect = true
	row.CheckedAt = 150
	if err := repo.UpdateTwitchPlaybackSessionValidation(ctx, row); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetTwitchPlaybackSession(ctx)
	if err != nil || !got.NeedsReconnect || got.CheckedAt != 150 || got.ExpiresAt != 200 {
		t.Fatalf("validation not persisted: %+v %v", got, err)
	}
	// An older successful check finishing after rejection cannot revive a
	// credential. Only an explicit validated replacement clears this state.
	row.NeedsReconnect = false
	if err := repo.UpdateTwitchPlaybackSessionValidation(ctx, row); err != nil {
		t.Fatal(err)
	}
	got, err = repo.GetTwitchPlaybackSession(ctx)
	if err != nil || !got.NeedsReconnect {
		t.Fatal("stale success revived rejected session")
	}
	replacement := &repository.TwitchPlaybackSession{TwitchUserID: "456", TwitchLogin: "second", EncryptedToken: []byte("different-ciphertext"), CheckedAt: 300}
	if err := repo.SaveTwitchPlaybackSession(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateTwitchPlaybackSessionValidation(ctx, row); err != nil {
		t.Fatal(err)
	}
	got, err = repo.GetTwitchPlaybackSession(ctx)
	if err != nil || got.NeedsReconnect || got.TwitchLogin != "second" || !bytes.Equal(got.EncryptedToken, replacement.EncryptedToken) || got.CheckedAt != 300 {
		t.Fatalf("stale validation changed replacement: %+v %v", got, err)
	}
	if err := repo.DeleteTwitchPlaybackSession(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateTwitchPlaybackSessionValidation(ctx, row); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetTwitchPlaybackSession(ctx); !errors.Is(err, repository.ErrNotFound) {
		t.Fatal("stale validation resurrected disconnected session")
	}
}
