package database_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/befabri/replayvod/server/internal/database"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/migrations"
)

func TestSQLiteMigrationsFreshServerSettingsShape(t *testing.T) {
	ctx := context.Background()
	db := newUnmigratedSQLiteDB(t)

	if err := database.MigrateSQLite(ctx, db, migrations.SQLite()); err != nil {
		t.Fatalf("fresh sqlite migrations: %v", err)
	}

	assertSQLiteServerSettingsColumns(t, ctx, db, expectedServerSettingsColumns)
	assertSQLiteServerSettingsIDPrimaryKey(t, ctx, db)
	assertSQLiteServerSettingsSingletonIDCheck(t, ctx, db)
	assertSQLiteIndexColumns(t, ctx, db, "idx_video_categories_category_id_video_id", []string{"category_id", "video_id"})

	adapter := sqliteadapter.New(db)
	if _, err := adapter.GetServerSettings(ctx); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("GetServerSettings on fresh DB = %v, want ErrNotFound", err)
	}

	saved, err := adapter.UpsertServerSettings(ctx, &repository.ServerSettings{
		ServerMode:                    "relay",
		EventSubWebhookCallbackURL:    "https://replayvod.example/api/v1/webhook/callback",
		EventSubRelayIngestURL:        "https://relay.example/u/token",
		EventSubRelaySubscribeURL:     "wss://relay.example/u/token/subscribe",
		EventSubRelayLocalCallbackURL: "http://127.0.0.1:8080/api/v1/webhook/callback",
	})
	if err != nil {
		t.Fatalf("UpsertServerSettings after fresh migration: %v", err)
	}
	if saved.ServerMode != "relay" {
		t.Fatalf("ServerMode after upsert = %q, want relay", saved.ServerMode)
	}
}

func TestSQLiteMigrationsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := newUnmigratedSQLiteDB(t)

	if err := database.MigrateSQLite(ctx, db, migrations.SQLite()); err != nil {
		t.Fatalf("fresh sqlite migrations: %v", err)
	}
	if err := database.MigrateSQLite(ctx, db, migrations.SQLite()); err != nil {
		t.Fatalf("second sqlite migration run: %v", err)
	}

	assertSQLiteServerSettingsColumns(t, ctx, db, expectedServerSettingsColumns)
	assertSQLiteServerSettingsIDPrimaryKey(t, ctx, db)
	assertSQLiteServerSettingsSingletonIDCheck(t, ctx, db)
	assertSQLiteIndexColumns(t, ctx, db, "idx_video_categories_category_id_video_id", []string{"category_id", "video_id"})
}

func newUnmigratedSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.NewSQLiteDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func assertSQLiteServerSettingsColumns(t *testing.T, ctx context.Context, db *sql.DB, want []string) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `PRAGMA table_info("server_settings")`)
	if err != nil {
		t.Fatalf("list server_settings columns: %v", err)
	}
	defer rows.Close()

	got := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan server_settings column: %v", err)
		}
		got[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate server_settings columns: %v", err)
	}

	for _, column := range want {
		if !got[column] {
			t.Fatalf("server_settings missing column %q; got columns %#v", column, got)
		}
	}
}

func assertSQLiteServerSettingsIDPrimaryKey(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `PRAGMA table_info("server_settings")`)
	if err != nil {
		t.Fatalf("inspect server_settings columns: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan server_settings column: %v", err)
		}
		if name == "id" {
			if pk == 0 {
				t.Fatal("server_settings.id is not a primary key")
			}
			return
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate server_settings columns: %v", err)
	}
	t.Fatal("server_settings.id column not found")
}

func assertSQLiteServerSettingsSingletonIDCheck(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin singleton check assertion tx: %v", err)
	}
	defer tx.Rollback() //nolint:errcheck // assertion transaction is always discarded

	if _, err := tx.ExecContext(ctx, `INSERT INTO server_settings (id) VALUES (2)`); err == nil {
		t.Fatal("server_settings allowed id=2; want CHECK (id = 1)")
	}
}

func assertSQLiteIndexColumns(t *testing.T, ctx context.Context, db *sql.DB, indexName string, want []string) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `PRAGMA index_info("`+indexName+`")`)
	if err != nil {
		t.Fatalf("inspect sqlite index %s: %v", indexName, err)
	}
	defer rows.Close()

	got := []string{}
	for rows.Next() {
		var seqno, cid int
		var name string
		if err := rows.Scan(&seqno, &cid, &name); err != nil {
			t.Fatalf("scan sqlite index %s: %v", indexName, err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate sqlite index %s: %v", indexName, err)
	}
	if len(got) == 0 {
		t.Fatalf("sqlite index %s does not exist", indexName)
	}
	if len(got) != len(want) {
		t.Fatalf("sqlite index %s columns = %v, want %v", indexName, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sqlite index %s columns = %v, want %v", indexName, got, want)
		}
	}
}
