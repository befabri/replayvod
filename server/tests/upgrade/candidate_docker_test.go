//go:build upgrade

package upgrade

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/migrations"
)

func checkCandidateMigrations(t *testing.T, image string) {
	t.Helper()
	// Cleanup is registered before start so an old binary that ignores the flag cannot leak.
	container := docker(t, "create", "--label", "replayvod.upgrade=true", "--network", "none", "--read-only", "--entrypoint", "/app/replayvod", image, "--migration-manifest")
	t.Cleanup(func() { docker(t, "rm", "-fv", container) })
	data, err := command(15*time.Second, nil, "docker", "start", "--attach", container)
	if err != nil {
		t.Fatalf("cannot read candidate %s migration provenance; rebuild with task test-upgrade: %v", image, err)
	}
	root := artifactRoot(t)
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "candidate-migrations.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	expected, err := migrations.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if err := compareCandidateMigrations(data, expected); err != nil {
		t.Fatalf("candidate %s: %v; rebuild with task test-upgrade", image, err)
	}
}
