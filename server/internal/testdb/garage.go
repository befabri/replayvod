//go:build integration

// Package testdb's garage.go boots a single-node Garage cluster in a
// testcontainer and returns endpoint + keys ready to hand to
// storage.NewS3.
//
// Why Garage: MinIO's community edition has been hollowed out in 2025
// releases; Garage is the likelier production object-store choice for
// the homelab workload this project targets. No official
// testcontainers module exists, but Garage v2 ships a
// `server --single-node --default-bucket --default-access-key` mode
// that handles layout + bucket + key creation inline, so the helper
// stays minimal.
//
// Gated by the `integration` build tag so unit-only runs
// (`go test ./...` without `-tags integration`) don't pay for a
// container pull on every invocation.
package testdb

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Garage is a running Garage instance ready for storage.S3Options.
// BucketName is the default bucket Garage auto-creates from
// GARAGE_DEFAULT_BUCKET; callers get a fresh cluster per NewGarageBucket
// call, so there's no cross-test bleed to worry about.
type Garage struct {
	container  testcontainers.Container
	Endpoint   string
	AccessKey  string
	SecretKey  string
	Region     string
	BucketName string
}

// Single-node Garage config. Keys are deterministic for test
// reproducibility — these are NOT secure and must never ship in
// production. replication_factor=1 is the one constraint
// --single-node enforces; the rest are reasonable defaults copied
// from the Garage quickstart reference.
const garageConfigTOML = `
metadata_dir = "/var/lib/garage/meta"
data_dir     = "/var/lib/garage/data"
db_engine    = "sqlite"

replication_factor = 1

rpc_bind_addr   = "[::]:3901"
rpc_public_addr = "127.0.0.1:3901"
rpc_secret      = "1799bccfd7411eddcf9ebd1e242af28bd0e5c25e9c6b22e7068ddfdd62f6a5d2"

[s3_api]
s3_region     = "garage"
api_bind_addr = "[::]:3900"
root_domain   = ".s3.garage.localhost"

[s3_web]
bind_addr   = "[::]:3902"
root_domain = ".web.garage.localhost"
index       = "index.html"

[k2v_api]
api_bind_addr = "[::]:3904"

[admin]
api_bind_addr = "[::]:3903"
admin_token   = "hunter2"
metrics_token = "hunter2"
`

// NewGarageBucket boots a fresh Garage container with a per-test
// bucket and access key already provisioned via Garage's built-in
// --single-node / --default-bucket / --default-access-key flags.
// Cleanup on t.Cleanup.
//
// Tests paying for a full container per call is fine at the current
// test count; a shared-cluster + per-test-bucket variant is a natural
// extension when the count justifies it.
func NewGarageBucket(t *testing.T) *Garage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Per-test credentials keep tests parallel-safe even if a future
	// version shares the container — Garage's access-key namespace
	// uniqueness is the guardrail either way.
	bucket := "test-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	accessKey := "GK" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", ""))[:25]
	secretKey := strings.ToLower(strings.ReplaceAll(uuid.NewString(), "-", "")) +
		strings.ToLower(strings.ReplaceAll(uuid.NewString(), "-", ""))[:32]

	req := testcontainers.ContainerRequest{
		// Requires Garage v2.3.0+ for --single-node and --default-*
		// flags — confirmed by .reference/garage/doc/book/quick-start
		// ("For older versions of Garage (before v2.3.0): automatic
		// configuration using --single-node and --default-bucket is
		// not available"). Earlier tags reject the flags at startup
		// and every integration test fails immediately.
		Image: "dxflrs/garage:v2.3.0",
		// --single-node auto-applies the initial layout.
		// --default-bucket + --default-access-key provision from env on
		// startup so the helper doesn't need to shell out post-boot.
		Cmd: []string{
			"/garage",
			"server",
			"--single-node",
			"--default-bucket",
			"--default-access-key",
		},
		Env: map[string]string{
			"GARAGE_DEFAULT_ACCESS_KEY": accessKey,
			"GARAGE_DEFAULT_SECRET_KEY": secretKey,
			"GARAGE_DEFAULT_BUCKET":     bucket,
		},
		ExposedPorts: []string{"3900/tcp"},
		Files: []testcontainers.ContainerFile{{
			Reader:            strings.NewReader(garageConfigTOML),
			ContainerFilePath: "/etc/garage.toml",
			FileMode:          0o644,
		}},
		// Garage emits "Creating default bucket" exactly once after the
		// S3 API is bound; waiting on that string guarantees the
		// bucket + key exist before any test touches the endpoint.
		WaitingFor: wait.ForLog("Creating default bucket").WithStartupTimeout(45 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("garage: start container: %v", err)
	}
	t.Cleanup(func() {
		// Bounded Terminate ctx: if the Docker daemon stalls during
		// shutdown, the test process shouldn't hang forever on a
		// hung teardown. 10s is generous for a single-container stop.
		termCtx, termCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer termCancel()
		_ = container.Terminate(termCtx)
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("garage: host: %v", err)
	}
	port, err := container.MappedPort(ctx, "3900")
	if err != nil {
		t.Fatalf("garage: mapped port: %v", err)
	}

	return &Garage{
		container:  container,
		Endpoint:   fmt.Sprintf("http://%s:%s", host, port.Port()),
		AccessKey:  accessKey,
		SecretKey:  secretKey,
		Region:     "garage",
		BucketName: bucket,
	}
}
