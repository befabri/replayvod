package database_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/befabri/replayvod/server/internal/database"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/migrations"
)

var expectedServerSettingsColumns = []string{
	"id",
	"server_mode",
	"eventsub_webhook_callback_url",
	"eventsub_relay_ingest_url",
	"eventsub_relay_subscribe_url",
	"eventsub_relay_local_callback_url",
	"created_at",
	"updated_at",
	"hmac_secret",
	"recording_webhook_enabled",
	"recording_webhook_url",
	"recording_webhook_secret",
	"recording_webhook_events",
	"playback_cache_enabled",
	"playback_cache_max_percent",
	"playback_cache_auto_generate",
	"schedules_paused",
}

func TestMain(m *testing.M) {
	os.Exit(testdb.SetupPG(m))
}

func TestPostgresMigrationsFreshServerSettingsShape(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPGPool(t)

	assertServerSettingsColumns(t, ctx, pool, expectedServerSettingsColumns)
	assertPostgresServerSettingsUniqueID(t, ctx, pool)
	assertPostgresServerSettingsSingletonIDCheck(t, ctx, pool)
	assertPostgresIndexColumns(t, ctx, pool, "video_categories", "idx_video_categories_category_id_video_id", []string{"category_id", "video_id"})

	adapter := pgadapter.New(pool)
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

func TestPostgresMigrationsIdempotent(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPGPool(t)

	if err := database.MigratePostgres(ctx, pool, migrations.Postgres()); err != nil {
		t.Fatalf("second migration run: %v", err)
	}

	assertServerSettingsColumns(t, ctx, pool, expectedServerSettingsColumns)
	assertPostgresServerSettingsUniqueID(t, ctx, pool)
	assertPostgresServerSettingsSingletonIDCheck(t, ctx, pool)
	assertPostgresIndexColumns(t, ctx, pool, "video_categories", "idx_video_categories_category_id_video_id", []string{"category_id", "video_id"})
}

func assertServerSettingsColumns(t *testing.T, ctx context.Context, pool *pgxpool.Pool, want []string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'server_settings'
	`)
	if err != nil {
		t.Fatalf("list server_settings columns: %v", err)
	}
	defer rows.Close()

	got := map[string]bool{}
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("scan server_settings column: %v", err)
		}
		got[column] = true
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

func assertPostgresServerSettingsUniqueID(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var hasUnique bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_index i
			JOIN pg_class t ON t.oid = i.indrelid
			JOIN pg_namespace n ON n.oid = t.relnamespace
			WHERE n.nspname = current_schema()
			  AND t.relname = 'server_settings'
			  AND i.indisunique
			  AND i.indisvalid
			  AND i.indpred IS NULL
			  AND i.indnkeyatts = 1
			  AND (
				  SELECT COUNT(*)
				  FROM unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord)
				  JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
				  WHERE k.ord <= i.indnkeyatts
				    AND a.attname = 'id'
			  ) = 1
		)
	`).Scan(&hasUnique); err != nil {
		t.Fatalf("check server_settings unique id: %v", err)
	}
	if !hasUnique {
		t.Fatal("server_settings.id has no unique index or primary key")
	}
}

func assertPostgresServerSettingsSingletonIDCheck(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin singleton check assertion tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // assertion transaction is always discarded

	if _, err := tx.Exec(ctx, `INSERT INTO server_settings (id) VALUES (2)`); err == nil {
		t.Fatal("server_settings allowed id=2; want CHECK (id = 1)")
	}
}

func assertPostgresIndexColumns(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tableName, indexName string, want []string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_class idx ON idx.oid = i.indexrelid
		JOIN pg_class t ON t.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		JOIN unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord) ON true
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
		WHERE n.nspname = current_schema()
		  AND t.relname = $1
		  AND idx.relname = $2
		  AND i.indisvalid
		  AND k.ord <= i.indnkeyatts
		ORDER BY k.ord
	`, tableName, indexName)
	if err != nil {
		t.Fatalf("inspect postgres index %s: %v", indexName, err)
	}
	defer rows.Close()

	got := []string{}
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("scan postgres index %s: %v", indexName, err)
		}
		got = append(got, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate postgres index %s: %v", indexName, err)
	}
	if len(got) == 0 {
		t.Fatalf("postgres index %s does not exist", indexName)
	}
	if len(got) != len(want) {
		t.Fatalf("postgres index %s columns = %v, want %v", indexName, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("postgres index %s columns = %v, want %v", indexName, got, want)
		}
	}
}
