package database_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/befabri/replayvod/server/internal/database"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
	"github.com/befabri/replayvod/server/migrations"
)

type migrationDB struct {
	db      *sql.DB
	repo    repository.Repository
	files   fs.FS
	migrate func(context.Context, fs.FS) error
}

func newMigrationDB(t *testing.T, backend string) migrationDB {
	t.Helper()
	if backend == "postgres" {
		pool := testdb.NewUnmigratedPGPool(t)
		// Assertions deliberately query several schema versions in one process.
		// Describe each query afresh: SELECT * changes shape across migrations,
		// whereas a real application restart starts with an empty plan cache.
		config := pool.Config().ConnConfig.Copy()
		config.DefaultQueryExecMode = pgx.QueryExecModeDescribeExec
		db := stdlib.OpenDB(*config)
		t.Cleanup(func() { _ = db.Close() })
		return migrationDB{db, pgadapter.New(pool), migrations.Postgres(), func(ctx context.Context, files fs.FS) error {
			return database.MigratePostgres(ctx, pool, files)
		}}
	}
	db := newUnmigratedSQLiteDB(t)
	return migrationDB{db, sqliteadapter.New(db), migrations.SQLite(), func(ctx context.Context, files fs.FS) error {
		return database.MigrateSQLite(ctx, db, files)
	}}
}

func migrationsThrough(t *testing.T, files fs.FS, version string) fstest.MapFS {
	t.Helper()
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		t.Fatal(err)
	}
	selected := fstest.MapFS{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") || entry.Name()[:3] > version {
			continue
		}
		content, err := fs.ReadFile(files, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		selected[entry.Name()] = &fstest.MapFile{Data: content}
	}
	return selected
}

func TestMigrationsExistingInstallations(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		for _, version := range []string{"021", "040", "044", "045"} {
			t.Run(backend+"/from_"+version, func(t *testing.T) {
				h := newMigrationDB(t, backend)
				ctx := context.Background()
				if err := h.migrate(ctx, migrationsThrough(t, h.files, version)); err != nil {
					t.Fatal(err)
				}
				tables := seedExistingInstallation(t, h.db, version)
				before := snapshotMigrationTables(t, h.db, tables)
				ledger := snapshotMigrationTables(t, h.db, []string{"schema_migrations"})

				if err := h.migrate(ctx, h.files); err != nil {
					t.Fatalf("upgrade installed database: %v", err)
				}
				assertMigrationTablesUnchanged(t, h.db, before)
				if err := h.migrate(ctx, h.files); err != nil {
					t.Fatalf("restart upgraded database: %v", err)
				}
				assertMigrationTablesUnchanged(t, h.db, before)
				assertMigrationLedgerPreserved(t, h.db, ledger["schema_migrations"])
				assertCount(t, h.db, "SELECT COUNT(*) FROM schema_migrations", len(migrationsThrough(t, h.files, "999")))

				schedule, err := h.repo.GetSchedule(ctx, 41)
				if err != nil {
					t.Fatalf("read old schedule using current adapter: %v", err)
				}
				if schedule.RecordingType != "video" || schedule.ForceH264 || schedule.TriggerCount != 17 || schedule.IsDisabled {
					t.Fatalf("old schedule behavior changed: %+v", schedule)
				}
				if version < "045" {
					assertCount(t, h.db, "SELECT COUNT(*) FROM download_schedules WHERE requested_from IS NOT NULL", 0)
					assertCount(t, h.db, "SELECT COUNT(*) FROM schedule_requests", 0)
					assertCount(t, h.db, "SELECT COUNT(*) FROM invites", 0)
				}
				if backend == "sqlite" {
					assertCount(t, h.db, "SELECT COUNT(*) FROM pragma_foreign_key_check", 0)
					assertCount(t, h.db, "PRAGMA foreign_keys", 1)
					assertSQLiteIndexColumns(t, ctx, h.db, "idx_schedule_requests_created", []string{"created_at", "id"})
					assertSQLiteIndexColumns(t, ctx, h.db, "idx_schedule_requests_user_created", []string{"requested_by", "created_at", "id"})
				} else {
					assertCount(t, h.db, `SELECT COUNT(*) FROM pg_indexes WHERE schemaname = 'public'
						AND tablename = 'schedule_requests' AND indexname IN ('idx_schedule_requests_created', 'idx_schedule_requests_user_created')`, 2)
				}
			})
		}
	}
}

func TestMigrationsInviteRollbackPreservesExistingData(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := context.Background()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "044")); err != nil {
				t.Fatal(err)
			}
			before := snapshotMigrationTables(t, h.db, seedExistingInstallation(t, h.db, "044"))
			if err := h.migrate(ctx, h.files); err != nil {
				t.Fatal(err)
			}
			seedInviteMigrationData(t, h.db)
			// Rollback must preserve pre-upgrade data, including
			// retired request history.
			for _, version := range []string{"046_schedule_request_pagination", "045_invites"} {
				rollbackMigration(t, h, version)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			if err := h.migrate(ctx, h.files); err != nil {
				t.Fatalf("upgrade after rollback: %v", err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			assertCount(t, h.db, "SELECT COUNT(*) FROM schedule_requests", 0)
			assertCount(t, h.db, "SELECT COUNT(*) FROM invites", 0)
		})
	}
}

func TestMigrationsFailedUpgradeCanRetry(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := context.Background()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "044")); err != nil {
				t.Fatal(err)
			}
			tables := append(seedExistingInstallation(t, h.db, "044"), "schema_migrations")
			before := snapshotMigrationTables(t, h.db, tables)
			broken := migrationsThrough(t, h.files, "999")
			broken["045_invites.up.sql"].Data = append(broken["045_invites.up.sql"].Data,
				[]byte("\nINSERT INTO missing_migration_target VALUES (1);")...)
			if err := h.migrate(ctx, broken); err == nil || !strings.Contains(err.Error(), "045_invites") {
				t.Fatalf("failed migration = %v, want error identifying 045_invites", err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			for _, query := range []string{"SELECT requested_from FROM download_schedules", "SELECT * FROM invites", "SELECT * FROM schedule_requests"} {
				rows, err := h.db.QueryContext(ctx, query)
				if err == nil {
					rows.Close()
					t.Fatalf("failed migration left schema changes: %s", query)
				}
			}
			if err := h.migrate(ctx, h.files); err != nil {
				t.Fatalf("retry after failed migration: %v", err)
			}
			delete(before, "schema_migrations")
			assertMigrationTablesUnchanged(t, h.db, before)
		})
	}
}

func TestMigrationsPreviouslyAppliedInviteDraft(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := context.Background()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "044")); err != nil {
				t.Fatal(err)
			}
			seedExistingInstallation(t, h.db, "044")
			// Already-applied migrations must remain skipped even
			// when their SQL changes.
			draft := migrationsThrough(t, h.files, "045")
			draft["045_invites.up.sql"].Data = append(draft["045_invites.up.sql"].Data, []byte("\nDROP TABLE IF EXISTS video_requests;")...)
			if err := h.migrate(ctx, draft); err != nil {
				t.Fatal(err)
			}
			seedInviteMigrationData(t, h.db)
			before := snapshotMigrationTables(t, h.db, []string{"users", "download_schedules", "invites", "schedule_requests"})
			if err := h.migrate(ctx, h.files); err != nil {
				t.Fatalf("upgrade previously applied draft: %v", err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			for _, version := range []string{"046_schedule_request_pagination", "045_invites"} {
				rollbackMigration(t, h, version)
			}
			// Deleted history requires a backup; downgrade can only
			// restore the table.
			assertCount(t, h.db, "SELECT COUNT(*) FROM video_requests", 0)
			if err := h.migrate(ctx, h.files); err != nil {
				t.Fatalf("upgrade after draft rollback: %v", err)
			}
		})
	}
}

func TestMigrationsConcurrentStartup(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		for _, installed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/installed_%t", backend, installed), func(t *testing.T) {
				h := newMigrationDB(t, backend)
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if installed {
					if err := h.migrate(ctx, migrationsThrough(t, h.files, "044")); err != nil {
						t.Fatal(err)
					}
					seedExistingInstallation(t, h.db, "044")
				}
				const contenders = 4
				runners := make([]func(context.Context, fs.FS) error, contenders)
				for i := range runners {
					runners[i] = h.migrate
					if backend == "sqlite" {
						// Independent connections
						// prevent Go pool limits from
						// hiding database lock
						// contention.
						var seq int
						var name, path string
						if err := h.db.QueryRowContext(ctx, "PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
							t.Fatal(err)
						}
						other, err := database.NewSQLiteDB(path)
						if err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() { _ = other.Close() })
						runners[i] = func(ctx context.Context, files fs.FS) error { return database.MigrateSQLite(ctx, other, files) }
					}
				}
				start := make(chan struct{})
				errors := make(chan error, contenders)
				for _, run := range runners {
					go func() {
						<-start
						errors <- run(ctx, h.files)
					}()
				}
				close(start)
				for range contenders {
					if err := <-errors; err != nil {
						t.Errorf("concurrent startup: %v", err)
					}
				}
				assertCount(t, h.db, "SELECT COUNT(*) FROM schema_migrations", len(migrationsThrough(t, h.files, "999")))
				if installed {
					assertCount(t, h.db, "SELECT COUNT(*) FROM video_requests", 3)
				}
			})
		}
	}
}

func TestMigrationsCanceledUpgradeCanRetry(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			if err := h.migrate(context.Background(), fstest.MapFS{}); err != nil {
				t.Fatal(err)
			}
			content := "CREATE TABLE cancellation_probe (id INTEGER PRIMARY KEY);\n"
			if backend == "postgres" {
				content += "SELECT pg_sleep(10);"
			} else {
				content += `WITH RECURSIVE numbers(n) AS (VALUES(0) UNION ALL SELECT n+1 FROM numbers WHERE n < 1000000000)
					INSERT INTO cancellation_probe SELECT n FROM numbers;`
			}
			files := fstest.MapFS{"001_cancellation_probe.up.sql": &fstest.MapFile{Data: []byte(content)}}
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			err := h.migrate(ctx, files)
			if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "001_cancellation_probe") {
				t.Fatalf("interrupted migration = %v, want deadline error during migration", err)
			}
			assertCount(t, h.db, "SELECT COUNT(*) FROM schema_migrations", 0)
			// Retrying CREATE detects leaked locks or DDL left
			// behind by cancellation.
			files["001_cancellation_probe.up.sql"].Data = []byte("CREATE TABLE cancellation_probe (id INTEGER PRIMARY KEY);")
			retryCtx, retryCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer retryCancel()
			if err := h.migrate(retryCtx, files); err != nil {
				t.Fatalf("retry canceled migration: %v", err)
			}
			assertCount(t, h.db, "SELECT COUNT(*) FROM schema_migrations", 1)
			if backend == "sqlite" {
				assertCount(t, h.db, "PRAGMA foreign_keys", 1)
			}
		})
	}
}

func seedExistingInstallation(t *testing.T, db *sql.DB, version string) []string {
	t.Helper()
	execMigrationSQL(t, db, `
		INSERT INTO users (id, login, display_name, role) VALUES
			('owner', 'owner', 'Owner', 'owner'), ('admin', 'admin', 'Admin', 'admin'), ('viewer', 'viewer', 'Viewer', 'viewer');
		INSERT INTO whitelist (twitch_user_id) VALUES ('viewer'), ('not-yet-registered');
		INSERT INTO channels (broadcaster_id, broadcaster_login, broadcaster_name) VALUES ('channel', 'channel', 'Channel');
		INSERT INTO categories (id, name) VALUES ('game', 'Game');
		INSERT INTO tags (id, name) VALUES (31, 'English');
		INSERT INTO titles (id, name) VALUES (51, 'Existing recording title');
		INSERT INTO videos (id, job_id, filename, display_name, broadcaster_id, status) VALUES
			(71, 'job-1', 'recording-1', 'Recording 1', 'channel', 'DONE'),
			(72, 'job-2', 'recording-2', 'Recording 2', 'channel', 'RUNNING');
		INSERT INTO video_parts (id, video_id, part_index, filename, quality, codec, segment_format, start_media_seq, end_media_seq)
			VALUES (81, 71, 0, 'recording-1-part0', 'HIGH', 'h264', 'ts', 0, 30);
		INSERT INTO video_titles (video_id, title_id) VALUES (71, 51);
		INSERT INTO video_categories (video_id, category_id) VALUES (71, 'game');
		INSERT INTO video_tags (video_id, tag_id) VALUES (71, 31);
		INSERT INTO video_requests (video_id, user_id, requested_at) VALUES
			(71, 'viewer', '2025-02-03 04:05:06'), (72, 'viewer', '2025-02-03 04:05:06'),
			(71, 'admin', '2025-02-04 01:02:03');
		INSERT INTO download_schedules (id, broadcaster_id, requested_by, quality,
			has_min_viewers, min_viewers, has_categories, has_tags, is_delete_rediff, time_before_delete,
			last_triggered_at, trigger_count, created_at, updated_at)
			VALUES (41, 'channel', 'admin', 'MEDIUM', TRUE, 250, TRUE, TRUE, TRUE, 48,
				'2025-02-03 04:05:06', 17, '2024-01-01 00:00:00', '2025-01-01 00:00:00');
		INSERT INTO download_schedules (id, broadcaster_id, requested_by, quality, is_disabled)
			VALUES (42, 'channel', 'viewer', 'LOW', TRUE);
		INSERT INTO download_schedule_categories (schedule_id, category_id) VALUES (41, 'game');
		INSERT INTO download_schedule_tags (schedule_id, tag_id) VALUES (41, 31);
	`)
	if _, err := db.ExecContext(context.Background(), `INSERT INTO sessions (hashed_id, user_id, encrypted_tokens, expires_at)
		VALUES ('session-hash', 'viewer', $1, '2099-01-01 00:00:00')`, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	tables := []string{"users", "whitelist", "sessions", "channels", "categories", "tags", "titles", "videos", "video_parts",
		"video_titles", "video_categories", "video_tags", "video_requests", "download_schedules", "download_schedule_categories", "download_schedule_tags"}
	if version >= "040" {
		execMigrationSQL(t, db, `INSERT INTO server_settings (id, server_mode, hmac_secret, playback_cache_enabled, playback_cache_max_percent)
			VALUES (1, 'poll', 'existing-hmac-secret', TRUE, 25)`)
		tables = append(tables, "server_settings")
	}
	if version >= "044" {
		execMigrationSQL(t, db, `
			INSERT INTO video_user_states (user_id, video_id, watch_later, last_position_seconds, last_progress_at_ms)
				VALUES ('viewer', 71, TRUE, 123.5, 1234567890);
			INSERT INTO channel_user_states (user_id, broadcaster_id, favorite) VALUES ('viewer', 'channel', TRUE);
			UPDATE download_schedules SET recording_type = 'audio', force_h264 = TRUE WHERE id = 42;
			UPDATE server_settings SET schedules_paused = TRUE;
		`)
		tables = append(tables, "video_user_states", "channel_user_states")
	}
	if version >= "045" {
		seedInviteMigrationData(t, db)
		tables = append(tables, "invites", "schedule_requests")
	}
	return tables
}

func seedInviteMigrationData(t *testing.T, db *sql.DB) {
	t.Helper()
	execMigrationSQL(t, db, `
		INSERT INTO invites (token_hash, role, created_by, expires_at, note)
			VALUES ('pending-hash', 'viewer', 'owner', '2099-01-01 00:00:00', 'Existing invitation');
		INSERT INTO invites (token_hash, role, created_by, expires_at, redeemed_by, redeemed_at)
			VALUES ('redeemed-hash', 'admin', 'owner', '2025-02-01 00:00:00', 'admin', '2025-01-01 00:00:00');
		INSERT INTO schedule_requests (broadcaster_id, requested_by, status, decided_by, decided_at, schedule_id, created_at)
			VALUES ('channel', 'viewer', 'APPROVED', 'admin', '2025-02-01 00:00:00', 41, '2025-01-01 00:00:00');
		INSERT INTO schedule_requests (broadcaster_id, requested_by, status, decided_by, decided_at, created_at)
			VALUES ('channel', 'viewer', 'REJECTED', 'admin', '2025-02-01 00:00:00', '2025-01-01 00:00:00');
		INSERT INTO schedule_requests (broadcaster_id, requested_by, note, created_at)
			VALUES ('channel', 'viewer', 'Please record', '2025-01-01 00:00:00');
		UPDATE download_schedules SET requested_from = 'viewer' WHERE id = 41;
	`)
}

func execMigrationSQL(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), statement); err != nil {
		t.Fatal(err)
	}
}

type migrationTableSnapshot struct {
	columns []string
	rows    [][]any
}

func snapshotMigrationTables(t *testing.T, db *sql.DB, tables []string) map[string]migrationTableSnapshot {
	t.Helper()
	result := map[string]migrationTableSnapshot{}
	for _, table := range tables {
		result[table] = readMigrationTable(t, db, table, "*")
	}
	return result
}

func readMigrationTable(t *testing.T, db *sql.DB, table, columns string) migrationTableSnapshot {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), "SELECT "+columns+" FROM "+table+" ORDER BY 1, 2")
	if err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := migrationTableSnapshot{columns: names}
	for rows.Next() {
		values := make([]any, len(names))
		pointers := make([]any, len(names))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		snapshot.rows = append(snapshot.rows, values)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertMigrationTablesUnchanged(t *testing.T, db *sql.DB, before map[string]migrationTableSnapshot) {
	t.Helper()
	for table, snapshot := range before {
		after := readMigrationTable(t, db, table, strings.Join(snapshot.columns, ","))
		if !reflect.DeepEqual(snapshot, after) {
			t.Errorf("migration changed existing %s data:\nbefore: %v\nafter:  %v", table, snapshot.rows, after.rows)
		}
	}
}

// expectMigrationValue changes one expected cell, keeping every other column
// and row in the preservation assertion. IDs in both SQL drivers are int64.
func expectMigrationValue(t *testing.T, snapshots map[string]migrationTableSnapshot, table string, id int64, column string, value any) {
	t.Helper()
	snapshot, ok := snapshots[table]
	if !ok {
		t.Fatalf("no snapshot for %s", table)
	}
	idColumn, valueColumn := -1, -1
	for i, name := range snapshot.columns {
		if name == "id" {
			idColumn = i
		}
		if name == column {
			valueColumn = i
		}
	}
	if idColumn < 0 || valueColumn < 0 {
		t.Fatalf("snapshot %s lacks id or %s", table, column)
	}
	for _, row := range snapshot.rows {
		if row[idColumn] == id {
			row[valueColumn] = value
			return
		}
	}
	t.Fatalf("snapshot %s has no row %d", table, id)
}

func assertMigrationLedgerPreserved(t *testing.T, db *sql.DB, before migrationTableSnapshot) {
	t.Helper()
	after := readMigrationTable(t, db, "schema_migrations", "version,applied_at")
	if len(after.rows) < len(before.rows) || !reflect.DeepEqual(after.rows[:len(before.rows)], before.rows) {
		t.Errorf("old migration versions or timestamps changed: before %v, after %v", before.rows, after.rows)
	}
}

func assertCount(t *testing.T, db *sql.DB, query string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), query).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Errorf("%s = %d, want %d", query, count, want)
	}
}

func rollbackMigration(t *testing.T, h migrationDB, version string) {
	t.Helper()
	content, err := fs.ReadFile(h.files, version+".down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := h.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit
	if _, err := tx.ExecContext(context.Background(), string(content)); err != nil {
		t.Fatalf("rollback %s: %v", version, err)
	}
	if _, err := tx.ExecContext(context.Background(), "DELETE FROM schema_migrations WHERE version = $1", version); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
