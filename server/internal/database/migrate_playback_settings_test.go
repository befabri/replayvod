package database_test

import (
	"fmt"
	"testing"
)

func TestPlaybackSettingsMigrationPreservesLocaleAndConstrainsPreferences(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := t.Context()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "065")); err != nil {
				t.Fatal(err)
			}
			execMigrationSQL(t, h.db, `
INSERT INTO users(id,login,display_name) VALUES('viewer','viewer','Viewer');
INSERT INTO settings(user_id,timezone,datetime_format,language,created_at,updated_at)
VALUES('viewer','Europe/Paris','EU','fr','2025-01-01 00:00:00','2025-02-01 00:00:00');
`)
			before := snapshotMigrationTables(t, h.db, []string{"users", "settings"})
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "066")); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			assertCount(t, h.db, "SELECT count(*) FROM settings WHERE resume_min_seconds=5 AND resume_end_margin_seconds=30 AND resume_end_margin_percent=5", 1)
			for _, tc := range []struct {
				column string
				max    int
			}{
				{"resume_min_seconds", 600},
				{"resume_end_margin_seconds", 600},
				{"resume_end_margin_percent", 50},
			} {
				for _, invalid := range []string{"NULL", "0", fmt.Sprint(tc.max + 1)} {
					if _, err := h.db.ExecContext(ctx, "UPDATE settings SET "+tc.column+"="+invalid); err == nil {
						t.Fatalf("%s accepted %s", tc.column, invalid)
					}
				}
				for _, valid := range []int{1, tc.max} {
					execMigrationSQL(t, h.db, fmt.Sprintf("UPDATE settings SET %s=%d", tc.column, valid))
				}
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			rollbackMigration(t, h, "066_user_playback_settings")
			assertMigrationTablesUnchanged(t, h.db, before)
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "066")); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			assertCount(t, h.db, "SELECT count(*) FROM settings WHERE resume_min_seconds=5 AND resume_end_margin_seconds=30 AND resume_end_margin_percent=5", 1)
		})
	}
}
