package database_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/befabri/replayvod/server/internal/database"
)

func TestSQLiteConnectionSettingsSurviveReplacement(t *testing.T) {
	// URI metacharacters must be treated as part of the configured filename.
	path := filepath.Join(t.TempDir(), "folder with spaces", "replay #100%.db")
	db, err := database.NewSQLiteDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, name := range []string{"initial connection", "replacement connection"} {
		t.Run(name, func(t *testing.T) {
			conn, err := db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			var mode string
			if err := conn.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&mode); err != nil {
				t.Fatal(err)
			}
			if mode != "wal" {
				t.Errorf("journal_mode = %s, want wal", mode)
			}
			for pragma, want := range map[string]int{"busy_timeout": 5000, "foreign_keys": 1} {
				var got int
				if err := conn.QueryRowContext(context.Background(), "PRAGMA "+pragma).Scan(&got); err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Errorf("%s = %d, want %d", pragma, got, want)
				}
			}
		})
		// Model a connection discarded after cancellation or a driver error.
		db.SetMaxIdleConns(0)
		db.SetMaxIdleConns(1)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database not created at the configured path: %v", err)
	}
}
