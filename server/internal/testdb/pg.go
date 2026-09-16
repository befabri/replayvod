// Package testdb provides database fixtures for tests.
//
// PG: boot one postgres:17-alpine container per test package via SetupPG
// in TestMain. Each test calls NewPGPool(t) to get a fresh migrated DB
// inside that container, cloned from a template that is migrated once per
// package. Contributor setup requirement: Docker running.
//
// SQLite: NewSQLiteDB(t) returns a per-test tempfile DB with migrations
// applied. No Docker needed for SQLite tests.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/befabri/replayvod/server/internal/database"
	"github.com/befabri/replayvod/server/migrations"
)

// sharedPG is set by SetupPG and consumed by NewPGPool. It is a package-level
// singleton because go test runs each package in its own process, so there is
// no cross-package leak risk from the global state.
var sharedPG *sharedPostgres

type sharedPostgres struct {
	container *postgres.PostgresContainer
	// adminConnStr is a DSN pointing at the container's default "postgres"
	// database. We connect to this to issue CREATE DATABASE / DROP DATABASE.
	adminConnStr string
	// templateOnce migrates templateName the first time a test asks for a
	// migrated database; every later test clones it instead of replaying
	// the migrations.
	templateOnce sync.Once
	templateErr  error
}

// templateName is the migrated database that NewPGPool clones. Postgres
// refuses to clone a database with open sessions, so nothing connects to it
// after the migration pool is closed.
const templateName = "test_template"

// SetupPG runs m with a shared PostgreSQL 17 container and releases it afterward.
// Call it from TestMain with Docker running:
//
//	func TestMain(m *testing.M) {
//		os.Exit(testdb.SetupPG(m))
//	}
func SetupPG(m *testing.M) int {
	ctx := context.Background()

	ctr, err := postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase("postgres"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		log.Printf("testdb: start postgres container: %v", err)
		return 1
	}

	adminConnStr, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = testcontainers.TerminateContainer(ctr)
		log.Printf("testdb: get connection string: %v", err)
		return 1
	}

	sharedPG = &sharedPostgres{container: ctr, adminConnStr: adminConnStr}
	// Defer termination immediately so a panic inside m.Run() doesn't leak
	// the container. sharedPG is cleared in the same defer to keep the two
	// lifecycles linked.
	defer func() {
		sharedPG = nil
		if err := testcontainers.TerminateContainer(ctr); err != nil {
			log.Printf("testdb: terminate container: %v", err)
		}
	}()

	// Suppress Info-level migration noise during tests; warnings and errors
	// still surface.
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prevLogger)

	return m.Run()
}

// NewUnmigratedPGPool creates a fresh database inside the shared container and
// returns a pgxpool scoped to it without applying migrations. It is intended for
// migration tests that need to construct a legacy schema by hand. Most tests
// should use NewPGPool instead.
func NewUnmigratedPGPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return newPGPool(t, "")
}

// newPGPool creates a uniquely named database in the shared container, cloned
// from template when one is given, and returns a pool scoped to it. The
// database is dropped on t.Cleanup.
func newPGPool(t *testing.T, template string) *pgxpool.Pool {
	t.Helper()
	if sharedPG == nil {
		t.Fatal("testdb: SetupPG must be called from TestMain")
	}
	ctx := context.Background()

	// Unique per-test DB name. 16 hex chars = 64 bits of entropy, plenty
	// to avoid collisions within a single test run even under parallelism.
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatalf("testdb: random: %v", err)
	}
	dbName := "test_" + hex.EncodeToString(raw[:])

	admin, err := pgxpool.New(ctx, sharedPG.adminConnStr)
	if err != nil {
		t.Fatalf("testdb: open admin pool: %v", err)
	}
	defer admin.Close()

	// %q would use Go escaping; Postgres identifiers need double-quote
	// wrapping. Names are hex or the fixed template, so no escaping hazards.
	create := fmt.Sprintf(`CREATE DATABASE "%s"`, dbName)
	if template != "" {
		create += fmt.Sprintf(` TEMPLATE "%s"`, template)
	}
	if err := createDatabase(ctx, admin, create); err != nil {
		t.Fatalf("testdb: create database %s: %v", dbName, err)
	}

	pool, err := pgxpool.New(ctx, withDBName(sharedPG.adminConnStr, dbName))
	if err != nil {
		_, _ = admin.Exec(ctx, fmt.Sprintf(`DROP DATABASE "%s" WITH (FORCE)`, dbName))
		t.Fatalf("testdb: open test pool: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		cleanupCtx := context.Background()
		a, err := pgxpool.New(cleanupCtx, sharedPG.adminConnStr)
		if err != nil {
			return // shared PG was shut down — nothing to clean up
		}
		defer a.Close()
		_, _ = a.Exec(cleanupCtx, fmt.Sprintf(`DROP DATABASE "%s" WITH (FORCE)`, dbName))
	})

	return pool
}

// NewPGPool creates a fresh migrated database inside the shared container
// and returns a pgxpool scoped to it. The DB is dropped on t.Cleanup. SetupPG
// must have been called from the package's TestMain.
func NewPGPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if sharedPG == nil {
		t.Fatal("testdb.NewPGPool: SetupPG must be called from TestMain")
	}
	sharedPG.templateOnce.Do(func() { sharedPG.templateErr = migrateTemplate(sharedPG.adminConnStr) })
	if sharedPG.templateErr != nil {
		t.Fatalf("testdb: migrate template: %v", sharedPG.templateErr)
	}
	return newPGPool(t, templateName)
}

// objectInUse is the Postgres error for cloning a database that still has a
// session open. The template's migration pool releases its sessions after
// Close returns, so the first clones may briefly see it.
const objectInUse = "55006"

// createDatabase runs stmt, retrying for a short while when the template is
// still in use.
func createDatabase(ctx context.Context, admin *pgxpool.Pool, stmt string) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := admin.Exec(ctx, stmt)
		var pgErr *pgconn.PgError
		if err == nil || !errors.As(err, &pgErr) || pgErr.Code != objectInUse || time.Now().After(deadline) {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// migrateTemplate creates templateName and applies every migration to it,
// closing its pool so later clones find no open session.
func migrateTemplate(adminConnStr string) error {
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, adminConnStr)
	if err != nil {
		return fmt.Errorf("open admin pool: %w", err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, templateName)); err != nil {
		return fmt.Errorf("create database %s: %w", templateName, err)
	}
	pool, err := pgxpool.New(ctx, withDBName(adminConnStr, templateName))
	if err != nil {
		return fmt.Errorf("open template pool: %w", err)
	}
	defer pool.Close()
	if err := database.MigratePostgres(ctx, pool, migrations.Postgres()); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// withDBName replaces the database path segment in a Postgres URL.
func withDBName(connStr, dbName string) string {
	u, err := url.Parse(connStr)
	if err != nil {
		return connStr
	}
	u.Path = "/" + dbName
	return u.String()
}
