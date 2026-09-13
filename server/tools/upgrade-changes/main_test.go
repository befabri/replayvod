package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpgradeNeeded(t *testing.T) {
	for _, path := range []string{"server/main.go", "server/migrations/sqlite/new.up.sql", "dashboard/src/index.ts", "Dockerfile", "docker-compose.yml", ".dockerignore", ".github/workflows/upgrade.yml", ".github/actions/image-metadata/action.yml", "server/tools/publish-images/main.go"} {
		if !upgradeNeeded([]string{path}) {
			t.Errorf("%s should need the upgrade checks", path)
		}
	}
	for _, paths := range [][]string{nil, {"README.md"}, {"landing/src/page.ts", "relay/index.ts"}, {"server/README.md", "server/docs/example.toml"}, {"dashboard/README.md"}} {
		if upgradeNeeded(paths) {
			t.Errorf("%v should not need the upgrade checks", paths)
		}
	}
	if !upgradeNeeded([]string{"README.md", "server/main.go"}) {
		t.Error("a runtime change next to documentation was ignored")
	}
}

func TestDecideFailsOpenWithoutABase(t *testing.T) {
	for _, tc := range []struct{ scope, base string }{{"latest", ""}, {"latest", strings.Repeat("0", 40)}, {"full", "unavailable"}} {
		needed, err := decide(tc.scope, tc.base, "HEAD", false)
		if err != nil || !needed {
			t.Errorf("scope %q base %q: needed %t, %v", tc.scope, tc.base, needed, err)
		}
	}
	if _, err := decide("typo", "", "HEAD", false); err == nil {
		t.Error("unknown scope was accepted")
	}
}

func TestChangedPathsComparePullRequestsAgainstTheMergeBase(t *testing.T) {
	t.Chdir(t.TempDir())
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(path, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	write("README.md", "original")
	run("add", ".")
	run("commit", "-q", "-m", "initial")
	initial := run("rev-parse", "HEAD")
	for _, pullRequest := range []bool{false, true} {
		needed, err := decide("latest", strings.Repeat("1", 40), "HEAD", pullRequest)
		if err != nil || !needed {
			t.Fatalf("unavailable base after history rewrite: needed %t, %v", needed, err)
		}
	}
	if _, err := decide("latest", initial, "missing-head", false); err == nil {
		t.Fatal("invalid head was accepted")
	}
	write("server/main.go", "package main")
	run("add", ".")
	run("commit", "-q", "-m", "base change")
	base := run("rev-parse", "HEAD")
	run("checkout", "-q", "--detach", initial)
	write("README.md", "documentation")
	run("add", ".")
	run("commit", "-q", "-m", "docs")

	paths, err := changedPaths(base, "HEAD", true)
	if err != nil || upgradeNeeded(paths) {
		t.Fatalf("documentation-only pull request needs checks: %v, %v", paths, err)
	}
	paths, err = changedPaths(base, "HEAD", false)
	if err != nil || !upgradeNeeded(paths) {
		t.Fatalf("branch comparison ignored the base change: %v, %v", paths, err)
	}
	run("checkout", "-q", "--detach", base)
	run("mv", "server/main.go", "example.md")
	run("commit", "-q", "-m", "remove runtime source")
	paths, err = changedPaths(base, "HEAD", true)
	if err != nil || !upgradeNeeded(paths) {
		t.Fatalf("removed runtime path was ignored: %v, %v", paths, err)
	}
}
