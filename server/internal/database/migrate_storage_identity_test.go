package database_test

import (
	"context"
	"testing"
)

func TestMigrationsStorageIdentityPreservesSettings(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := context.Background()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "053")); err != nil {
				t.Fatal(err)
			}
			tables := seedExistingInstallation(t, h.db, "053")
			execMigrationSQL(t, h.db, `UPDATE server_settings SET created_at = '2024-01-01 00:00:00', updated_at = '2025-01-01 00:00:00'`)
			before := snapshotMigrationTables(t, h.db, tables)
			up := migrationsThrough(t, h.files, "054")
			if err := h.migrate(ctx, up); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			settings, err := h.repo.GetServerSettings(ctx)
			if err != nil || settings.StorageID != "" || settings.StorageScanCursor != 0 {
				t.Fatalf("upgraded settings = %+v, %v; want empty storage id and cursor 0", settings, err)
			}
			if _, err := h.repo.SetStorageID(ctx, "cafe"); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			if err := h.repo.SetStorageScanCursor(ctx, 71); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			// Downgrading drops only the two storage columns; every other setting
			// the previous schema knew survives.
			rollbackMigration(t, h, "054_storage_identity")
			assertMigrationTablesUnchanged(t, h.db, before)
			if err := h.migrate(ctx, up); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			settings, err = h.repo.GetServerSettings(ctx)
			if err != nil || settings.StorageID != "" || settings.StorageScanCursor != 0 {
				t.Fatalf("re-upgraded settings = %+v, %v; want defaults after rollback", settings, err)
			}
		})
	}
}
