package database_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestJobStopRequestedUpgradeKeepsStopsOutsideCheckpointWrites(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := t.Context()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "063")); err != nil {
				t.Fatal(err)
			}
			seedExistingInstallation(t, h.db, "063")
			execMigrationSQL(t, h.db, `UPDATE jobs SET execution_id='current-owner',accepts_metadata=TRUE,
resume_state='{"stage":"DOWNLOAD","current_part_index":2,"part_started":true,"part_bytes":123}' WHERE id='job-2'`)
			before := snapshotMigrationTables(t, h.db, []string{"jobs", "videos"})
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "064")); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			assertCount(t, h.db, "SELECT COUNT(*) FROM jobs WHERE id='job-2' AND stop_requested=FALSE AND accepts_metadata=TRUE AND execution_id='current-owner'", 1)
			execMigrationSQL(t, h.db, `UPDATE jobs SET stop_requested=TRUE,accepts_metadata=FALSE
WHERE id='job-2' AND execution_id='current-owner'`)
			var checkpoint string
			if err := h.db.QueryRowContext(ctx, "SELECT resume_state FROM jobs WHERE id='job-2'").Scan(&checkpoint); err != nil {
				t.Fatal(err)
			}
			var saved struct {
				Stage     string `json:"stage"`
				PartBytes int64  `json:"part_bytes"`
			}
			if err := json.Unmarshal([]byte(checkpoint), &saved); err != nil {
				t.Fatal(err)
			}
			if saved.Stage != "DOWNLOAD" || saved.PartBytes != 123 {
				t.Fatalf("stop changed the recovery checkpoint: %s", checkpoint)
			}

			// A checkpoint captured before cancellation must not clear the stop.
			execMigrationSQL(t, h.db, `UPDATE jobs SET
resume_state='{"stage":"DOWNLOAD","current_part_index":2,"part_started":true,"part_bytes":456}'
WHERE id='job-2' AND execution_id='current-owner'`)
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "064")); err != nil {
				t.Fatal(err)
			}
			assertCount(t, h.db, `SELECT COUNT(*) FROM jobs WHERE id='job-2'
AND stop_requested=TRUE AND accepts_metadata=FALSE AND execution_id='current-owner' AND status='RUNNING'`, 1)
			execMigrationSQL(t, h.db, `INSERT INTO jobs(id,video_id,broadcaster_id,status)
VALUES('new-attempt',71,'channel','PENDING')`)
			assertCount(t, h.db, "SELECT COUNT(*) FROM jobs WHERE id='new-attempt' AND stop_requested=FALSE", 1)
			before["jobs"] = readMigrationTable(t, h.db, "jobs", strings.Join(before["jobs"].columns, ","))
			rollbackMigration(t, h, "064_job_stop_requested")
			assertMigrationTablesUnchanged(t, h.db, before)
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "064")); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			assertCount(t, h.db, "SELECT COUNT(*) FROM jobs WHERE stop_requested=FALSE", 2)
		})
	}
}
