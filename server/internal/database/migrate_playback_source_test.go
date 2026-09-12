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

func TestMigrationsVideoSourcePreservesHistoryAndRollback(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := context.Background()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "051")); err != nil {
				t.Fatal(err)
			}
			tables := seedExistingInstallation(t, h.db, "051")
			execMigrationSQL(t, h.db, `INSERT INTO jobs (id, video_id, broadcaster_id, status, resume_state) VALUES ('job-1', 71, 'channel', 'DONE', '{}')`)
			tables = append(tables, "jobs")
			before := snapshotMigrationTables(t, h.db, tables)
			up := migrationsThrough(t, h.files, "052")
			if err := h.migrate(ctx, up); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			assertCount(t, h.db, `SELECT COUNT(*) FROM videos WHERE source = 'live' AND twitch_video_id IS NULL AND broadcast_at IS NULL`, 2)
			assertRejected(t, h, `UPDATE videos SET source = 'unknown' WHERE id = 71`)
			execMigrationSQL(t, h.db, `UPDATE videos SET source = 'vod', twitch_video_id = '123', broadcast_at = '2025-01-01 00:00:00' WHERE id = 71`)
			assertRejected(t, h, `UPDATE videos SET source = 'vod', twitch_video_id = '123' WHERE id = 72`)
			assertCount(t, h.db, `SELECT COUNT(*) FROM jobs WHERE attempt = 1`, 1)
			assertCount(t, h.db, `SELECT COUNT(*) FROM videos WHERE next_retry_at IS NULL`, 2)
			// The one-row-per-VOD rule keeps a failed archive held only while a
			// retry is scheduled for it.
			execMigrationSQL(t, h.db, `UPDATE videos SET status = 'FAILED', next_retry_at = '2026-01-01 00:00:00' WHERE id = 71`)
			assertRejected(t, h, `INSERT INTO videos (job_id, filename, display_name, broadcaster_id, status, source, twitch_video_id) VALUES ('job-dup', 'dup', 'Dup', 'channel', 'PENDING', 'vod', '123')`)
			execMigrationSQL(t, h.db, `UPDATE videos SET next_retry_at = NULL WHERE id = 71`)
			execMigrationSQL(t, h.db, `INSERT INTO videos (job_id, filename, display_name, broadcaster_id, status, source, twitch_video_id) VALUES ('job-dup', 'dup', 'Dup', 'channel', 'PENDING', 'vod', '123')`)
			execMigrationSQL(t, h.db, `DELETE FROM videos WHERE job_id = 'job-dup'`)
			execMigrationSQL(t, h.db, `UPDATE videos SET status = 'DONE' WHERE id = 71`)
			written := snapshotMigrationTables(t, h.db, tables)
			if err := h.migrate(ctx, up); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, written)
			rollbackMigration(t, h, "052_videos_source")
			// Only the new archive metadata is discarded; all legacy columns,
			// media parts, history, and foreign-key dependents survive.
			assertMigrationTablesUnchanged(t, h.db, before)
			if err := h.migrate(ctx, up); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			assertCount(t, h.db, `SELECT COUNT(*) FROM videos WHERE source = 'live' AND twitch_video_id IS NULL AND broadcast_at IS NULL`, 2)
			execMigrationSQL(t, h.db, `UPDATE videos SET source = 'vod', twitch_video_id = '123' WHERE id = 71`)
			assertRejected(t, h, `UPDATE videos SET source = 'vod', twitch_video_id = '123' WHERE id = 72`)
			// The repository reads the newest schema, so the domain mapping is
			// checked once every later migration is applied.
			if err := h.migrate(ctx, h.files); err != nil {
				t.Fatal(err)
			}
			video, err := h.repo.GetVideo(ctx, 71)
			if err != nil || video.Source != repository.VideoSourceVOD || video.TwitchVideoID == nil || *video.TwitchVideoID != "123" {
				t.Fatalf("archived video 71: %+v, %v", video, err)
			}
			video, err = h.repo.GetVideo(ctx, 72)
			if err != nil || video.Source != repository.VideoSourceLive || video.TwitchVideoID != nil || video.BroadcastAt != nil {
				t.Fatalf("historical video 72: %+v, %v", video, err)
			}
		})
	}
}
