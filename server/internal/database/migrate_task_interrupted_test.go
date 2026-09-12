package database_test

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

func TestMigrationsTaskInterruptedPreservesRegistry(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			h := newMigrationDB(t, backend)
			ctx := context.Background()
			if err := h.migrate(ctx, migrationsThrough(t, h.files, "054")); err != nil {
				t.Fatal(err)
			}
			tables := seedExistingInstallation(t, h.db, "054")
			for i, status := range []string{"pending", "running", "success", "failed", "skipped"} {
				execMigrationSQL(t, h.db, fmt.Sprintf(`INSERT INTO tasks
					(name, description, interval_seconds, is_enabled, last_run_at, last_duration_ms,
					 last_status, last_error, next_run_at, created_at, updated_at)
					VALUES ('%s', 'Registry é日本語', %d, %t, '2025-01-02 03:04:05', %d,
					 '%s', 'saved diagnostic', '2025-02-03 04:05:06', '2024-01-01 00:00:00', '2025-01-01 00:00:00')`,
					status, i*60, i%2 == 0, i+42, status))
			}
			execMigrationSQL(t, h.db, `INSERT INTO tasks (name) VALUES ('never-run')`)
			tables = append(tables, "tasks")
			before := snapshotMigrationTables(t, h.db, tables)
			indexes := taskIndexDefinitions(t, h, backend)
			if len(indexes) != 2 {
				t.Fatalf("initial task indexes = %v, want both scheduling indexes", indexes)
			}
			assertShape := func() {
				t.Helper()
				if got := taskIndexDefinitions(t, h, backend); !reflect.DeepEqual(got, indexes) {
					t.Errorf("task indexes changed: before=%v after=%v", indexes, got)
				}
				assertRejected(t, h, `UPDATE tasks SET last_status='unknown' WHERE name='pending'`)
				assertRejected(t, h, `UPDATE tasks SET interval_seconds=-1 WHERE name='pending'`)
				assertRejected(t, h, `INSERT INTO tasks (name) VALUES ('pending')`)
			}
			up := migrationsThrough(t, h.files, "055")
			if err := h.migrate(ctx, up); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, before)
			assertShape()
			execMigrationSQL(t, h.db, `INSERT INTO tasks (name,interval_seconds,last_status,last_duration_ms) VALUES ('interrupted-test',3600,'interrupted',42)`)
			written := snapshotMigrationTables(t, h.db, tables)
			tasks := written["tasks"]
			for _, row := range tasks.rows {
				if row[slices.Index(tasks.columns, "name")] == "interrupted-test" {
					row[slices.Index(tasks.columns, "last_status")] = "skipped"
				}
			}
			rollbackMigration(t, h, "055_task_interrupted")
			assertMigrationTablesUnchanged(t, h.db, written)
			assertShape()
			assertRejected(t, h, `UPDATE tasks SET last_status='interrupted' WHERE name='interrupted-test'`)
			if err := h.migrate(ctx, up); err != nil {
				t.Fatal(err)
			}
			assertMigrationTablesUnchanged(t, h.db, written)
			assertShape()
			execMigrationSQL(t, h.db, `UPDATE tasks SET last_status='interrupted' WHERE name='interrupted-test'`)
		})
	}
}

func taskIndexDefinitions(t *testing.T, h migrationDB, backend string) map[string]string {
	t.Helper()
	query := `SELECT name, sql FROM sqlite_schema WHERE type = 'index' AND tbl_name = 'tasks' AND sql IS NOT NULL`
	if backend == "postgres" {
		query = `SELECT indexname, indexdef FROM pg_indexes WHERE schemaname = 'public' AND tablename = 'tasks' AND indexname <> 'tasks_pkey'`
	}
	rows, err := h.db.QueryContext(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	indexes := map[string]string{}
	for rows.Next() {
		var name, definition string
		if err := rows.Scan(&name, &definition); err != nil {
			t.Fatal(err)
		}
		indexes[name] = definition
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return indexes
}
