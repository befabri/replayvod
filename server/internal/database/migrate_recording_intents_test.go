package database_test

import "testing"

func TestRecordingIntentsUpgradeEnforcesOwnershipAndKeepsHistory(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := t.Context()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "058")); err != nil {
				t.Fatal(err)
			}
			before := snapshotMigrationTables(t, h.db, seedExistingInstallation(t, h.db, "058"))
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "059")); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			execMigrationSQL(t, h.db, `INSERT INTO recording_intents
(id,broadcaster_id,params,wait_seconds,status,current_job_id)
VALUES('first','channel','{}',60,'active','job-1')`)
			assertCount(t, h.db, `SELECT COUNT(*) FROM recording_intents
WHERE id='first' AND stop_requested=FALSE AND last_stream_id='' AND wait_until IS NULL AND created_at IS NOT NULL`, 1)
			for _, status := range []string{"active", "waiting"} {
				if _, err := h.db.ExecContext(ctx, "UPDATE recording_intents SET status=$1 WHERE id='first'", status); err != nil {
					t.Fatal(err)
				}
				if _, err := h.db.ExecContext(ctx, `INSERT INTO recording_intents
(id,broadcaster_id,params,wait_seconds,status,current_job_id)
VALUES('second','channel','{}',60,'active','job-2')`); err == nil {
					t.Fatalf("%s intent did not reserve its channel", status)
				}
			}
			execMigrationSQL(t, h.db, `
UPDATE recording_intents SET status='stopped' WHERE id='first';
INSERT INTO recording_intents(id,broadcaster_id,params,wait_seconds,status,current_job_id)
VALUES('second','channel','{}',60,'waiting','job-2');
INSERT INTO videos(id,job_id,filename,display_name,broadcaster_id,status) VALUES
(73,'job-3','recording-3','Recording 3','channel','DONE'),
(74,'job-4','recording-4','Recording 4','channel','DONE');
INSERT INTO recording_intent_videos(intent_id,video_id,position,stream_id) VALUES
('first',71,0,NULL),('first',72,1,NULL),('second',73,0,'stream-1');
`)
			for _, tc := range []struct {
				name, statement string
			}{
				{"nonpositive wait", "UPDATE recording_intents SET wait_seconds=0 WHERE id='second'"},
				{"unknown status", "UPDATE recording_intents SET status='paused' WHERE id='second'"},
				{"reactivate historical intent", "UPDATE recording_intents SET status='active' WHERE id='first'"},
				{"video in another intent", "INSERT INTO recording_intent_videos VALUES('second',71,1,'stream-2')"},
				{"repeated position", "INSERT INTO recording_intent_videos VALUES('second',74,0,'stream-2')"},
				{"repeated stream", "INSERT INTO recording_intent_videos VALUES('second',74,1,'stream-1')"},
				{"missing video", "INSERT INTO recording_intent_videos VALUES('second',999,1,'stream-2')"},
				{"missing intent", "INSERT INTO recording_intent_videos VALUES('missing',74,1,'stream-2')"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if _, err := h.db.ExecContext(ctx, tc.statement); err == nil {
						t.Fatal("accepted invalid intent ownership")
					}
				})
			}
			assertCount(t, h.db, "SELECT COUNT(*) FROM recording_intent_videos", 3)
			assertCount(t, h.db, "SELECT COUNT(*) FROM recording_intents WHERE id='second' AND status='waiting' AND wait_seconds=60", 1)
			execMigrationSQL(t, h.db, `
UPDATE recording_intents SET status='expired' WHERE id='second';
INSERT INTO recording_intents(id,broadcaster_id,params,wait_seconds,status,current_job_id)
VALUES('third','channel','{}',60,'active','job-4');
DELETE FROM videos WHERE id=71;
`)
			assertCount(t, h.db, "SELECT COUNT(*) FROM recording_intent_videos WHERE intent_id='first' AND video_id=72", 1)
			assertCount(t, h.db, "SELECT COUNT(*) FROM recording_intents WHERE id='first' AND status='stopped'", 1)
			execMigrationSQL(t, h.db, "DELETE FROM recording_intents WHERE id='first'")
			assertCount(t, h.db, "SELECT COUNT(*) FROM recording_intent_videos WHERE intent_id='first'", 0)
			assertCount(t, h.db, "SELECT COUNT(*) FROM videos WHERE id=72", 1)
			remaining := snapshotMigrationTables(t, h.db, []string{"videos", "jobs", "channels"})
			rollbackMigration(t, h, "059_recording_intents")
			assertMigrationTablesUnchanged(t, h.db, remaining)
		})
	}
}
