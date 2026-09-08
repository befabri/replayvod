package database_test

import (
	"context"
	"fmt"
	"testing"
)

func TestMigrationsRecordingQualityPreservesRowsAndRollback(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := context.Background()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "050")); err != nil {
				t.Fatal(err)
			}
			tables := seedExistingInstallation(t, h.db, "050")
			for id := 73; id <= 75; id++ {
				execMigrationSQL(t, h.db, fmt.Sprintf(`INSERT INTO videos (id, job_id, filename, display_name, broadcaster_id, quality)
					VALUES (%d, 'quality-%d', 'quality-%d', 'Quality é日本語', 'channel', 'LOW')`, id, id, id))
			}
			for id := 43; id <= 45; id++ {
				execMigrationSQL(t, h.db, fmt.Sprintf(`INSERT INTO channels (broadcaster_id, broadcaster_login, broadcaster_name)
					VALUES ('quality-%d', 'quality-%d', 'Quality channel');
					INSERT INTO download_schedules (id, broadcaster_id, requested_by, quality)
					VALUES (%d, 'quality-%d', 'admin', 'MEDIUM')`, id, id, id, id))
			}
			before := snapshotMigrationTables(t, h.db, tables)
			up := migrationsThrough(t, h.files, "051")
			if err := h.migrate(ctx, up); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)

			// Keep all five choices in distinct rows through rollback. Otherwise
			// a blanket UPDATE to HIGH could pass while erasing legacy choices.
			for table, firstID := range map[string]int{"videos": 71, "download_schedules": 41} {
				for i, quality := range []string{"LOW", "MEDIUM", "HIGH", "1440", "BEST"} {
					execMigrationSQL(t, h.db, fmt.Sprintf("UPDATE %s SET quality = '%s' WHERE id = %d", table, quality, firstID+i))
				}
				assertRejected(t, h, fmt.Sprintf("UPDATE %s SET quality = 'invalid' WHERE id = %d", table, firstID))
			}
			written := snapshotMigrationTables(t, h.db, tables)
			if err := h.migrate(ctx, up); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, written)
			for table, firstID := range map[string]int64{"videos": 71, "download_schedules": 41} {
				expectMigrationValue(t, written, table, firstID+3, "quality", "HIGH")
				expectMigrationValue(t, written, table, firstID+4, "quality", "HIGH")
			}
			rollbackMigration(t, h, "051_recording_quality")
			assertMigrationTablesUnchanged(t, h.db, written)
			for table, id := range map[string]int{"videos": 71, "download_schedules": 41} {
				for _, quality := range []string{"1440", "BEST"} {
					assertRejected(t, h, fmt.Sprintf("UPDATE %s SET quality = '%s' WHERE id = %d", table, quality, id))
				}
			}
			if err := h.migrate(ctx, up); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, written)
			for table, id := range map[string]int{"videos": 71, "download_schedules": 41} {
				for _, quality := range []string{"1440", "BEST"} {
					execMigrationSQL(t, h.db, fmt.Sprintf("UPDATE %s SET quality = '%s' WHERE id = %d", table, quality, id))
				}
			}
		})
	}
}
