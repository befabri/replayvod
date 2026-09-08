package database_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

func TestMigrationsPlaybackSessionLifecycle(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := context.Background()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "049")); err != nil {
				t.Fatal(err)
			}
			tables := seedExistingInstallation(t, h.db, "049")
			before := snapshotMigrationTables(t, h.db, tables)
			up := migrationsThrough(t, h.files, "050")
			if err := h.migrate(ctx, up); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			if _, err := h.repo.GetTwitchPlaybackSession(ctx); !errors.Is(err, repository.ErrNotFound) {
				t.Fatalf("new playback table: %v, want no connection", err)
			}
			row := &repository.TwitchPlaybackSession{
				TwitchUserID: "123", TwitchLogin: "historical-session", EncryptedToken: []byte{0, 255, 1, 128},
				ExpiresAt: 2000000001, CheckedAt: 2000000000, NeedsReconnect: true,
			}
			if err := h.repo.SaveTwitchPlaybackSession(ctx, row); err != nil {
				t.Fatal(err)
			}
			assertRejected(t, h, `INSERT INTO twitch_playback_sessions SELECT 2, twitch_user_id, twitch_login, encrypted_token, expires_at, checked_at, needs_reconnect FROM twitch_playback_sessions`)
			written := snapshotMigrationTables(t, h.db, append(tables, "twitch_playback_sessions"))
			if err := h.migrate(ctx, up); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, written)
			got, err := h.repo.GetTwitchPlaybackSession(ctx)
			if err != nil || !bytes.Equal(got.EncryptedToken, row.EncryptedToken) {
				t.Fatalf("session after restart: %+v, %v", got, err)
			}
			rollbackMigration(t, h, "050_twitch_playback_sessions")
			assertMigrationTablesUnchanged(t, h.db, before)
			if err := h.migrate(ctx, up); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			if _, err := h.repo.GetTwitchPlaybackSession(ctx); !errors.Is(err, repository.ErrNotFound) {
				t.Fatalf("downgrade must discard the unsupported connection: %v", err)
			}
			if err := h.repo.SaveTwitchPlaybackSession(ctx, row); err != nil {
				t.Fatalf("reconnect after reupgrade: %v", err)
			}
		})
	}
}
