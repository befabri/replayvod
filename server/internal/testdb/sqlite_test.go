package testdb

import (
	"io/fs"
	"testing"

	"github.com/befabri/replayvod/server/migrations"
)

func TestClonedSQLiteFixturesRemainIsolatedAndEnforceForeignKeys(t *testing.T) {
	first, second := NewSQLiteDB(t), NewSQLiteDB(t)
	if _, err := first.Exec("INSERT INTO channels(broadcaster_id,broadcaster_login,broadcaster_name) VALUES ('only-first','first','First')"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := second.QueryRow("SELECT COUNT(*) FROM channels").Scan(&count); err != nil || count != 0 {
		t.Fatalf("fixture data leaked: count=%d error=%v", count, err)
	}
	if _, err := second.Exec("INSERT INTO videos(job_id,filename,display_name,broadcaster_id) VALUES ('invalid','invalid','Invalid','missing-channel')"); err == nil {
		t.Fatal("cloned fixture lost foreign key enforcement")
	}
	applied, err := fs.Glob(migrations.SQLite(), "*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := second.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil || count != len(applied) {
		t.Fatalf("cloned migration ledger: count=%d want=%d error=%v", count, len(applied), err)
	}
}

func TestParallelSQLiteFixturesRemainIsolated(t *testing.T) {
	for range 8 {
		t.Run("clone", func(t *testing.T) {
			t.Parallel()
			db := NewSQLiteDB(t)
			if _, err := db.Exec("INSERT INTO channels(broadcaster_id,broadcaster_login,broadcaster_name) VALUES ('shared-id','channel','Channel')"); err != nil {
				t.Fatalf("fixture shared another clone's row: %v", err)
			}
		})
	}
}
