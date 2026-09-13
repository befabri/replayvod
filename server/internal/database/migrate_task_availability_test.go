package database_test

import (
	"strings"
	"testing"
)

func TestTaskAvailabilityUpgradePreservesOperatorStateAndHistory(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := t.Context()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "062")); err != nil {
				t.Fatal(err)
			}
			execMigrationSQL(t, h.db, `INSERT INTO tasks
(name,description,interval_seconds,is_enabled,last_run_at,last_duration_ms,last_status,last_error,next_run_at,execution_id,created_at,updated_at) VALUES
('paused','Operator paused',300,FALSE,'2025-01-01 00:00:00',123,'success',NULL,'2099-01-01 00:00:00','completed-owner','2024-01-01 00:00:00','2025-01-01 00:00:00'),
('running','Interrupted process',60,TRUE,'2025-01-02 00:00:00',456,'running',NULL,'2025-01-02 00:01:00','running-owner','2024-01-01 00:00:00','2025-01-02 00:00:00'),
('failed','Last failure',600,TRUE,'2025-01-03 00:00:00',789,'failed','preserved failure','2099-01-03 00:00:00','failed-owner','2024-01-01 00:00:00','2025-01-03 00:00:00'),
('manual','Queued once',0,FALSE,NULL,0,'pending',NULL,'2099-01-04 00:00:00','','2024-01-01 00:00:00','2025-01-04 00:00:00')`)
			before := snapshotMigrationTables(t, h.db, []string{"tasks"})
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "063")); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			assertCount(t, h.db, "SELECT COUNT(*) FROM tasks WHERE is_available=FALSE", 4)

			// Process registration must be independent of the operator's pause.
			execMigrationSQL(t, h.db, "UPDATE tasks SET is_available=TRUE WHERE name IN ('paused','manual','running')")
			assertMigrationTablesUnchanged(t, h.db, before)
			assertCount(t, h.db, "SELECT COUNT(*) FROM tasks WHERE is_available=TRUE AND is_enabled=FALSE", 2)
			execMigrationSQL(t, h.db, "UPDATE tasks SET is_available=FALSE WHERE name='running'")
			assertMigrationTablesUnchanged(t, h.db, before)
			assertCount(t, h.db, "SELECT COUNT(*) FROM tasks WHERE name='running' AND is_available=FALSE AND is_enabled=TRUE AND execution_id='running-owner'", 1)
			execMigrationSQL(t, h.db, "INSERT INTO tasks(name) VALUES('new-task')")
			assertCount(t, h.db, "SELECT COUNT(*) FROM tasks WHERE name='new-task' AND is_available=FALSE AND is_enabled=TRUE", 1)
			before["tasks"] = readMigrationTable(t, h.db, "tasks", strings.Join(before["tasks"].columns, ","))
			rollbackMigration(t, h, "063_task_availability")
			assertMigrationTablesUnchanged(t, h.db, before)
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "063")); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			assertCount(t, h.db, "SELECT COUNT(*) FROM tasks WHERE is_available=FALSE", 5)
		})
	}
}
