package upgrade

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type baseline struct {
	Version         string              `json:"version"`
	Commit          string              `json:"commit"`
	Image           string              `json:"image"`
	PlatformImages  map[string]string   `json:"platform_images"`
	SeedFiles       map[string][]string `json:"seed_files"`
	RecoveryFiles   map[string][]string `json:"recovery_files"`
	MigrationSHA256 map[string]string   `json:"migration_sha256"`
}

type manifest struct {
	PostgresImage string            `json:"postgres_image"`
	PostgresMajor int               `json:"postgres_major"`
	Latest        string            `json:"latest"`
	Baselines     []baseline        `json:"baselines"`
	FixtureSHA256 map[string]string `json:"fixture_sha256"`
}

// architectures are the platforms CI tests natively and every baseline must pin.
var architectures = []string{"amd64", "arm64"}

var pinnedReference = regexp.MustCompile(`^[a-z0-9./:_-]+@sha256:[a-f0-9]{64}$`)

// hasMigration reports whether the release shipped an up migration for the backend.
func (b baseline) hasMigration(backend, version string) bool {
	_, ok := b.MigrationSHA256["server/migrations/"+backend+"/"+version+".up.sql"]
	return ok
}

// validatePlatformImages requires one immutable image per CI architecture, drawn
// from the same repository and tag as the baseline's index.
func validatePlatformImages(b baseline) error {
	name, index, _ := strings.Cut(b.Image, "@")
	seen := map[string]bool{index: true}
	for _, arch := range architectures {
		ref := b.PlatformImages[arch]
		_, digest, found := strings.Cut(ref, "@")
		if !found || !strings.HasPrefix(ref, name+"@sha256:") || !pinnedReference.MatchString(ref) || seen[digest] {
			return fmt.Errorf("baseline %s must pin a distinct linux/%s image from %s", b.Version, arch, name)
		}
		seen[digest] = true
	}
	return nil
}

func readManifest() (manifest, error) {
	var m manifest
	data, err := os.ReadFile("testdata/manifest.json")
	if err != nil {
		return m, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return m, err
	}
	return m, validateManifest(m)
}

func validateManifest(m manifest) error {
	pinned := pinnedReference
	commit := regexp.MustCompile(`^[a-f0-9]{40}$`)
	version := regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	if m.PostgresMajor < 10 || !strings.HasPrefix(m.PostgresImage, fmt.Sprintf("postgres:%d-alpine@", m.PostgresMajor)) || !pinned.MatchString(m.PostgresImage) {
		return fmt.Errorf("PostgreSQL image must be digest-pinned and match postgres_major %d", m.PostgresMajor)
	}
	seen := map[string]bool{}
	for _, b := range m.Baselines {
		if !version.MatchString(b.Version) || seen[b.Version] || !commit.MatchString(b.Commit) || !pinned.MatchString(b.Image) || len(b.MigrationSHA256) == 0 {
			return fmt.Errorf("invalid or duplicate baseline %q", b.Version)
		}
		seen[b.Version] = true
		if err := validatePlatformImages(b); err != nil {
			return err
		}
		for profile, files := range map[string]map[string][]string{"seed_files": b.SeedFiles, "recovery_files": b.RecoveryFiles} {
			for _, backend := range []string{"sqlite", "postgres"} {
				if len(files[backend]) == 0 {
					return fmt.Errorf("baseline %s has no %s %s", b.Version, backend, profile)
				}
				for _, name := range files[backend] {
					if !filepath.IsLocal(name) || !strings.HasSuffix(name, ".sql") || len(m.FixtureSHA256[name]) != 64 {
						return fmt.Errorf("baseline %s references an unverified %s file %q", b.Version, profile, name)
					}
				}
			}
		}
	}
	if !seen[m.Latest] {
		return fmt.Errorf("latest baseline %q is missing", m.Latest)
	}
	if len(m.FixtureSHA256) == 0 {
		return fmt.Errorf("fixture checksums are missing")
	}
	for _, name := range []string{"large.sql", "config.toml", "recording.mp4", "recording.ts"} {
		if len(m.FixtureSHA256[name]) != 64 {
			return fmt.Errorf("missing checksum for %s", name)
		}
	}
	return nil
}

func verifyHash(path, expected string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != expected {
		return fmt.Errorf("historical file changed: %s", path)
	}
	return nil
}

func TestHistoricalFixtures(t *testing.T) {
	checkHistoricalFixtures(t)
}

func checkHistoricalFixtures(t *testing.T) manifest {
	t.Helper()
	m, err := readManifest()
	if err != nil {
		t.Fatal(err)
	}
	for name, expected := range m.FixtureSHA256 {
		if !filepath.IsLocal(name) {
			t.Fatalf("invalid fixture name %q", name)
		}
		if err := verifyHash(filepath.Join("testdata", name), expected); err != nil {
			t.Error(err)
		}
	}
	for _, b := range m.Baselines {
		for name, expected := range b.MigrationSHA256 {
			if !filepath.IsLocal(name) || !strings.HasPrefix(name, "server/migrations/") || !strings.HasSuffix(name, ".sql") {
				t.Fatalf("invalid migration path %q", name)
			}
			if err := verifyHash(filepath.Join("../../..", name), expected); err != nil {
				t.Errorf("%s: %v", b.Version, err)
			}
		}
	}
	compose, err := os.ReadFile("../../../docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkComposePostgres(compose, m); err != nil {
		t.Fatal(err)
	}
	if t.Failed() {
		t.FailNow()
	}
	return m
}

func checkComposePostgres(data []byte, m manifest) error {
	var compose struct {
		Services map[string]struct {
			Image string `yaml:"image"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return fmt.Errorf("parse Compose PostgreSQL service: %w", err)
	}
	expected, _, _ := strings.Cut(m.PostgresImage, "@")
	actual := compose.Services["postgres"].Image
	// Compose may use the same digest pin, or the corresponding floating tag.
	if actual != expected && actual != m.PostgresImage {
		return fmt.Errorf("Compose services.postgres.image is %q, want %q or %q", actual, expected, m.PostgresImage)
	}
	return nil
}

func TestFixtureChecksumRejectsEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "historical.sql")
	original := []byte("CREATE TABLE original (id INTEGER);")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(original)
	if err := verifyHash(path, hex.EncodeToString(hash[:])); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("CREATE TABLE replacement (id INTEGER);"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyHash(path, hex.EncodeToString(hash[:])); err == nil {
		t.Fatal("changed historical SQL was accepted")
	}
}

func TestManifestRejectsUnreproducibleBaselines(t *testing.T) {
	for _, name := range []string{"floating image", "missing platform image", "foreign platform image", "index as platform image", "missing latest", "duplicate baseline", "missing provenance", "wrong postgres", "missing postgres major", "floating postgres", "missing recovery profile", "unverified recovery fixture"} {
		t.Run(name, func(t *testing.T) {
			m, err := readManifest()
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "floating image":
				m.Baselines[0].Image = "ghcr.io/befabri/replayvod:latest"
			case "missing platform image":
				delete(m.Baselines[0].PlatformImages, "arm64")
			case "foreign platform image":
				m.Baselines[0].PlatformImages["amd64"] = "ghcr.io/example/other:1.0.0@sha256:" + strings.Repeat("a", 64)
			case "index as platform image":
				m.Baselines[0].PlatformImages["amd64"] = m.Baselines[0].Image
			case "missing latest":
				m.Latest = "v0.0.0"
			case "duplicate baseline":
				m.Baselines = append(m.Baselines, m.Baselines[0])
			case "missing provenance":
				m.Baselines[0].Commit = ""
			case "missing recovery profile":
				m.Baselines[0].RecoveryFiles = nil
			case "unverified recovery fixture":
				m.Baselines[0].RecoveryFiles["sqlite"] = []string{"unknown.sql"}
			case "wrong postgres":
				m.PostgresMajor++
			case "missing postgres major":
				m.PostgresMajor = 0
			case "floating postgres":
				m.PostgresImage, _, _ = strings.Cut(m.PostgresImage, "@")
			}
			if err := validateManifest(m); err == nil {
				t.Fatal("invalid baseline was accepted")
			}
		})
	}
}

func TestPostgresVersionFollowsManifest(t *testing.T) {
	for _, major := range []int{16, 17, 18} {
		t.Run(fmt.Sprint(major), func(t *testing.T) {
			m, err := readManifest()
			if err != nil {
				t.Fatal(err)
			}
			m.PostgresMajor = major
			m.PostgresImage = fmt.Sprintf("postgres:%d-alpine@sha256:%s", major, strings.Repeat("a", 64))
			if err := validateManifest(m); err != nil {
				t.Fatal(err)
			}
			tag, _, _ := strings.Cut(m.PostgresImage, "@")
			for _, image := range []string{tag, m.PostgresImage} {
				// A real YAML parser must support quoted scalars and merged service values.
				data := fmt.Sprintf("x-db: &db\n  image: %q\nservices:\n  postgres:\n    <<: *db\n", image)
				if err := checkComposePostgres([]byte(data), m); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestComposePostgresRejectsDrift(t *testing.T) {
	m, err := readManifest()
	if err != nil {
		t.Fatal(err)
	}
	tag, _, _ := strings.Cut(m.PostgresImage, "@")
	for _, data := range []string{
		"services: {}\n# image: " + tag,
		"services:\n  unrelated:\n    image: " + tag,
		"services:\n  postgres:\n    image: " + tag + "-different",
		"services:\n  postgres:\n    image: " + tag + "@sha256:" + strings.Repeat("0", 64),
		fmt.Sprintf("services:\n  postgres:\n    image: postgres:%d-alpine\n# image: %s", m.PostgresMajor+1, tag),
		"services: [invalid",
	} {
		t.Run(data, func(t *testing.T) {
			if err := checkComposePostgres([]byte(data), m); err == nil {
				t.Fatal("invalid Compose service was accepted")
			}
		})
	}
}
