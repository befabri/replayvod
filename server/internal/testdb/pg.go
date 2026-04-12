// Package testdb provides database fixtures for tests.
//
// PG: boot one postgres:16-alpine container per test package via SetupPG
// in TestMain. Each test calls NewPGPool(t) to get a fresh migrated DB
// inside that container. Contributor setup requirement: Docker running.
//
// SQLite: NewSQLiteDB(t) returns a per-test tempfile DB with migrations
// applied. No Docker needed for SQLite tests.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/url"
	"testing"

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
}

// SetupPG boots a postgres:16-alpine container, runs the test suite, and
// terminates the container. Intended to be called from TestMain:
//
//	func TestMain(m *testing.M) {
//		os.Exit(testdb.SetupPG(m))
//	}
//
// Only Docker is required on the contributor's machine. First invocation
// pulls the image (~40 MB, ~10s); subsequent runs reuse.
func SetupPG(m *testing.M) int {
	ctx := context.Background()

	ctr, err := postgres.Run(ctx,
		"postgres:16-alpine",
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

	// Suppress Info-level migration noise during tests: each test runs ~12
	// migrations, that's a lot of "Applying migration" lines in -v output.
	// Warnings and errors still surface.
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prevLogger)

	return m.Run()
}

// NewPGPool creates a fresh database inside the shared container, applies
// migrations, and returns a pgxpool scoped to it. The DB is dropped on
// t.Cleanup. SetupPG must have been called from the package's TestMain.
func NewPGPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if sharedPG == nil {
		t.Fatal("testdb.NewPGPool: SetupPG must be called from TestMain")
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
	// wrapping. dbName is hex — no escaping hazards.
	if _, err := admin.Exec(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, dbName)); err != nil {
		t.Fatalf("testdb: create database %s: %v", dbName, err)
	}

	pool, err := pgxpool.New(ctx, withDBName(sharedPG.adminConnStr, dbName))
	if err != nil {
		_, _ = admin.Exec(ctx, fmt.Sprintf(`DROP DATABASE "%s" WITH (FORCE)`, dbName))
		t.Fatalf("testdb: open test pool: %v", err)
	}

	if err := database.MigratePostgres(ctx, pool, migrations.Postgres()); err != nil {
		pool.Close()
		_, _ = admin.Exec(ctx, fmt.Sprintf(`DROP DATABASE "%s" WITH (FORCE)`, dbName))
		t.Fatalf("testdb: apply migrations: %v", err)
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

// withDBName replaces the database path segment in a Postgres URL.
func withDBName(connStr, dbName string) string {
	u, err := url.Parse(connStr)
	if err != nil {
		return connStr
	}
	u.Path = "/" + dbName
	return u.String()
}
