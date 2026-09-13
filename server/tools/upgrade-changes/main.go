// Command upgrade-changes decides whether a change set needs the Docker upgrade
// checks and appends the answer to GITHUB_OUTPUT as upgrade=true or false.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	scope := os.Getenv("UPGRADE_SCOPE")
	if scope == "" {
		scope = "latest"
	}
	needed, err := decide(scope, os.Getenv("UPGRADE_BASE_SHA"), os.Getenv("UPGRADE_HEAD_SHA"), os.Getenv("GITHUB_EVENT_NAME") == "pull_request")
	if err != nil {
		return err
	}
	output, err := os.OpenFile(os.Getenv("GITHUB_OUTPUT"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer output.Close()
	_, err = fmt.Fprintf(output, "upgrade=%t\n", needed)
	return err
}

// decide fails open: a release scope or a missing base commit checks everything.
func decide(scope, base, head string, pullRequest bool) (bool, error) {
	if scope != "latest" && scope != "full" {
		return false, fmt.Errorf("unknown upgrade scope %q", scope)
	}
	if scope == "full" || strings.Trim(base, "0") == "" {
		return true, nil
	}
	if _, err := git("rev-parse", "--verify", "--quiet", base+"^{commit}"); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			fmt.Fprintln(os.Stderr, "Base commit is unavailable; running upgrade checks.")
			return true, nil
		}
		return false, err
	}
	paths, err := changedPaths(base, head, pullRequest)
	if err != nil {
		return false, err
	}
	return upgradeNeeded(paths), nil
}

// upgradeNeeded reports whether any path can change the image or its checks.
func upgradeNeeded(paths []string) bool {
	for _, path := range paths {
		if strings.HasSuffix(path, ".md") || strings.HasPrefix(path, "server/docs/") || strings.HasPrefix(path, "dashboard/docs/") {
			continue
		}
		switch path {
		case "Dockerfile", ".dockerignore", "docker-compose.yml":
			return true
		}
		for _, prefix := range []string{"server/", "dashboard/", ".github/workflows/", ".github/actions/"} {
			if strings.HasPrefix(path, prefix) {
				return true
			}
		}
	}
	return false
}

// changedPaths lists the files that differ between base and head. A pull
// request compares against the merge base so changes already on the target
// branch do not count. Renames are not detected, so a removed runtime path
// stays listed.
func changedPaths(base, head string, pullRequest bool) ([]string, error) {
	if pullRequest {
		out, err := git("merge-base", base, head)
		if err != nil {
			return nil, err
		}
		base = strings.TrimSpace(string(out))
	}
	out, err := git("diff", "--name-only", "--no-renames", "-z", base, head)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, path := range bytes.Split(out, []byte{0}) {
		if len(path) > 0 {
			paths = append(paths, string(path))
		}
	}
	return paths, nil
}

func git(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return out, nil
}
