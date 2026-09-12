package database_test

import (
	"context"
	"testing"
)

func TestMigrationsStorageRestoreCursorPreservesSettings(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := context.Background()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "055")); err != nil {
				t.Fatal(err)
			}
			tables := seedExistingInstallation(t, h.db, "055")
			execMigrationSQL(t, h.db, "UPDATE server_settings SET storage_id = 'saved-volume', storage_scan_cursor = 42, created_at = '2024-01-01 00:00:00', updated_at = '2025-01-01 00:00:00'")
			before := snapshotMigrationTables(t, h.db, tables)
			up := migrationsThrough(t, h.files, "056")
			for range 2 {
				if err := h.migrate(ctx, up); err != nil {
					t.Fatal(err)
				}
				assertMigrationTablesUnchanged(t, h.db, before)
				settings, err := h.repo.GetServerSettings(ctx)
				if err != nil || settings.StorageRestoreCursor != nil || settings.StorageScanCursor != 42 || settings.StorageID != "saved-volume" {
					t.Fatalf("upgraded settings = %+v, %v", settings, err)
				}
				cursor := int64(128)
				if err := h.repo.SetStorageRestoreCursor(ctx, &cursor); err != nil {
					t.Fatal(err)
				}
				assertMigrationTablesUnchanged(t, h.db, before)
				settings, err = h.repo.GetServerSettings(ctx)
				if err != nil || settings.StorageRestoreCursor == nil || *settings.StorageRestoreCursor != 128 {
					t.Fatalf("saved restore cursor = %+v, %v", settings, err)
				}
				rollbackMigration(t, h, "056_storage_restore_cursor")
				assertMigrationTablesUnchanged(t, h.db, before)
			}
		})
	}
}
