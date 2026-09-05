// Command upgrade-baseline records a published release in the upgrade
// manifest: its source commit, the image digest of each CI architecture, and
// the checksums of the migrations it shipped. Existing entries and fixtures
// are never rewritten.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var architectures = []string{"amd64", "arm64"}

type baseline struct {
	Version         string              `json:"version"`
	Commit          string              `json:"commit"`
	Image           string              `json:"image"`
	PlatformImages  map[string]string   `json:"platform_images"`
	MigrationSHA256 map[string]string   `json:"migration_sha256"`
	SeedFiles       map[string][]string `json:"seed_files"`
	RecoveryFiles   map[string][]string `json:"recovery_files"`
}

type manifest struct {
	PostgresImage string            `json:"postgres_image"`
	PostgresMajor int               `json:"postgres_major"`
	Latest        string            `json:"latest"`
	Baselines     []baseline        `json:"baselines"`
	FixtureSHA256 map[string]string `json:"fixture_sha256"`
}

func main() {
	version := flag.String("version", "", "published stable tag, such as v2.7.3")
	image := flag.String("image", "", "published image reference including its @sha256 digest")
	latest := flag.Bool("latest", false, "use this baseline for the regular PR checks")
	seed := flag.String("seed-baseline", "", "reuse this baseline's seed and recovery profiles (default: the current latest)")
	flag.Parse()
	if err := run(*version, *image, *seed, *latest); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(version, image, seedVersion string, latest bool) error {
	if !regexp.MustCompile(`^v\d+\.\d+\.\d+$`).MatchString(version) {
		return errors.New("version must be a stable vMAJOR.MINOR.PATCH tag")
	}
	if !regexp.MustCompile(`^ghcr\.io/befabri/replayvod:` + regexp.QuoteMeta(version[1:]) + `@sha256:[a-f0-9]{64}$`).MatchString(image) {
		return errors.New("image must match the published version and include its digest")
	}
	root, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	path := filepath.Join(root, "server", "tests", "upgrade", "testdata", "manifest.json")
	m, err := readManifest(path)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(m.Baselines, func(b baseline) bool { return b.Version == version }) {
		return errors.New("baseline already exists; historical entries are never overwritten")
	}
	if seedVersion == "" {
		seedVersion = m.Latest
	}
	seedIndex := slices.IndexFunc(m.Baselines, func(b baseline) bool { return b.Version == seedVersion })
	if seedIndex < 0 {
		return fmt.Errorf("unknown seed baseline %s", seedVersion)
	}
	for name, digest := range m.FixtureSHA256 {
		data, err := os.ReadFile(filepath.Join(filepath.Dir(path), name))
		if err != nil {
			return err
		}
		if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != digest {
			return fmt.Errorf("historical fixture changed: %s", name)
		}
	}
	commit, err := git("rev-parse", version+"^{commit}")
	if err != nil {
		return err
	}
	index, err := docker("buildx", "imagetools", "inspect", "--raw", image)
	if err != nil {
		return err
	}
	images, err := platformImages([]byte(index), image)
	if err != nil {
		return err
	}
	for _, arch := range architectures {
		if _, err := docker("pull", "--platform", "linux/"+arch, images[arch]); err != nil {
			return err
		}
		revision, err := docker("image", "inspect", "--format", `{{index .Config.Labels "org.opencontainers.image.revision"}}`, images[arch])
		if err != nil {
			return err
		}
		if revision != commit {
			return fmt.Errorf("linux/%s image source %s does not match release commit %s", arch, revision, commit)
		}
	}
	migrations, err := releasedMigrations(root, commit)
	if err != nil {
		return err
	}
	seed := m.Baselines[seedIndex]
	m.Baselines = append(m.Baselines, baseline{
		Version:         version,
		Commit:          commit,
		Image:           image,
		PlatformImages:  images,
		MigrationSHA256: migrations,
		SeedFiles:       seed.SeedFiles,
		RecoveryFiles:   seed.RecoveryFiles,
	})
	if latest {
		m.Latest = version
	}
	if err := writeManifest(path, m); err != nil {
		return err
	}
	fmt.Printf("Added %s. Review the fixture contract and run the full upgrade suite.\n", version)
	return nil
}

func readManifest(path string) (manifest, error) {
	var m manifest
	data, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return m, decoder.Decode(&m)
}

func writeManifest(path string, m manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// platformImages pins each CI architecture to its manifest digest from a
// published index, ignoring attestation entries.
func platformImages(index []byte, reference string) (map[string]string, error) {
	var parsed struct {
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform struct {
				OS           string `json:"os"`
				Architecture string `json:"architecture"`
			} `json:"platform"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(index, &parsed); err != nil {
		return nil, err
	}
	name, _, _ := strings.Cut(reference, "@")
	valid := regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	images := map[string]string{}
	for _, entry := range parsed.Manifests {
		arch := entry.Platform.Architecture
		if entry.Platform.OS != "linux" || !slices.Contains(architectures, arch) {
			continue
		}
		if _, duplicate := images[arch]; duplicate {
			return nil, fmt.Errorf("ambiguous linux/%s manifests in %s", arch, reference)
		}
		if !valid.MatchString(entry.Digest) {
			return nil, fmt.Errorf("invalid linux/%s digest %q in %s", arch, entry.Digest, reference)
		}
		images[arch] = name + "@" + entry.Digest
	}
	for _, arch := range architectures {
		if images[arch] == "" {
			return nil, fmt.Errorf("%s has no linux/%s manifest", reference, arch)
		}
	}
	return images, nil
}

// releasedMigrations hashes every migration file as committed at the release,
// read from Git objects rather than the checkout.
func releasedMigrations(root, commit string) (map[string]string, error) {
	listing, err := gitIn(root, "ls-tree", "-r", "--name-only", commit, "server/migrations")
	if err != nil {
		return nil, err
	}
	migrations := map[string]string{}
	for _, path := range strings.Split(strings.TrimSpace(listing), "\n") {
		if !strings.HasSuffix(path, ".sql") {
			continue
		}
		content, err := gitIn(root, "show", commit+":"+path)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256([]byte(content))
		migrations[path] = hex.EncodeToString(sum[:])
	}
	if len(migrations) == 0 {
		return nil, errors.New("release contains no migration files")
	}
	return migrations, nil
}

func git(args ...string) (string, error) { return gitIn("", args...) }

func gitIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return output(cmd)
}

func docker(args ...string) (string, error) { return output(exec.Command("docker", args...)) }

// output runs cmd and returns its stdout. Trailing whitespace is trimmed except
// for git show, whose file contents must hash exactly.
func output(cmd *exec.Cmd) (string, error) {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", strings.Join(cmd.Args, " "), err, stderr.String())
	}
	if len(cmd.Args) > 1 && cmd.Args[1] == "show" {
		return string(out), nil
	}
	return strings.TrimSpace(string(out)), nil
}
