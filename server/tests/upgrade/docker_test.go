//go:build upgrade

package upgrade

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type installation struct {
	t             *testing.T
	network       string
	volume        string
	probePath     string
	config        string
	backend       string
	artifacts     string
	app           string
	postgresMajor int
}

func artifactRoot(t *testing.T) string {
	t.Helper()
	path := os.Getenv("UPGRADE_ARTIFACTS")
	if path == "" {
		path = "../../target/upgrade"
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return absolute
}

func command(timeout time.Duration, stdin []byte, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, stderr.String())
	}
	return out, nil
}

func docker(t *testing.T, args ...string) string {
	t.Helper()
	out, err := command(3*time.Minute, nil, "docker", args...)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func newInstallation(t *testing.T, m manifest, backend, probe, artifacts string) *installation {
	t.Helper()
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	name := "replayvod-upgrade-" + hex.EncodeToString(suffix[:])
	artifacts = filepath.Join(artifacts, name)
	config, err := filepath.Abs("testdata/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	h := &installation{t: t, network: name, volume: name, backend: backend, probePath: probe, config: config, artifacts: artifacts, postgresMajor: m.PostgresMajor}
	if err := os.MkdirAll(artifacts, 0755); err != nil {
		t.Fatal(err)
	}
	docker(t, "network", "create", "--internal", "--label", "replayvod.upgrade=true", name)
	t.Cleanup(func() { h.cleanup("network", "rm", name) })
	docker(t, "volume", "create", "--label", "replayvod.upgrade=true", name)
	t.Cleanup(func() { h.cleanup("volume", "rm", name) })
	if backend == "postgres" {
		pg := name + "-postgres"
		h.container(pg, "--network-alias", "database", "-e", "POSTGRES_PASSWORD=upgrade", "-e", "POSTGRES_DB=replayvod", m.PostgresImage)
		h.waitFor("PostgreSQL", func() bool {
			return postgresReady(pg)
		})
	}
	return h
}

func postgresReady(container string) bool {
	// The initialization server accepts Unix sockets before the TCP server starts.
	_, err := command(5*time.Second, nil, "docker", "exec", container, "pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-d", "replayvod")
	return err == nil
}

func TestPostgresReadinessRequiresTCP(t *testing.T) {
	m := checkHistoricalFixtures(t)
	h := newInstallation(t, m, "sqlite", "unused", filepath.Join(artifactRoot(t), "postgres-readiness"))
	pg := h.network + "-socket-only"
	h.container(pg, "-e", "POSTGRES_PASSWORD=upgrade", "-e", "POSTGRES_DB=replayvod", m.PostgresImage, "postgres", "-c", "listen_addresses=")
	h.waitFor("PostgreSQL Unix socket", func() bool {
		process, err := command(5*time.Second, nil, "docker", "exec", pg, "cat", "/proc/1/comm")
		if err != nil || strings.TrimSpace(string(process)) != "postgres" {
			return false
		}
		_, err = command(5*time.Second, nil, "docker", "exec", pg, "pg_isready", "-U", "postgres", "-d", "replayvod")
		return err == nil
	})
	if postgresReady(pg) {
		t.Fatal("Unix-socket-only PostgreSQL was reported ready for application TCP connections")
	}
}

func (h *installation) cleanup(args ...string) {
	if _, err := command(45*time.Second, nil, "docker", args...); err != nil {
		h.t.Errorf("cleanup: %v", err)
	}
}

func (h *installation) container(name string, args ...string) {
	h.t.Helper()
	base := []string{"create", "--name", name, "--label", "replayvod.upgrade=true", "--network", h.network}
	docker(h.t, append(base, args...)...)
	h.t.Cleanup(func() {
		h.capture(name)
		h.cleanup("rm", "-fv", name)
	})
	docker(h.t, "start", name)
}

func (h *installation) capture(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "logs", "--timestamps", name).CombinedOutput()
	if err != nil {
		out = append(out, []byte(err.Error())...)
	}
	h.artifact(name+".log", out)
	state, err := command(10*time.Second, nil, "docker", "inspect", name)
	if err != nil {
		state = []byte(err.Error())
	}
	h.artifact(name+".json", state)
}

func (h *installation) artifact(name string, data []byte) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.artifacts, name), data, 0600); err != nil {
		h.t.Errorf("write artifact: %v", err)
	}
}

func (h *installation) waitFor(what string, ready func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(75 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		select {
		case <-h.t.Context().Done():
			h.t.Fatal(h.t.Context().Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
	h.t.Fatalf("%s did not become ready; see %s", what, h.artifacts)
}

func (h *installation) applicationArgs() []string {
	return []string{
		"-v", h.volume + ":/app/data",
		"-v", h.probePath + ":/upgrade-probe:ro",
		"-v", h.config + ":/app/config.toml:ro",
		"-e", "DATABASE_DRIVER=" + h.backend,
		"-e", "POSTGRES_HOST=database", "-e", "POSTGRES_PORT=5432", "-e", "POSTGRES_DATABASE=replayvod",
		"-e", "POSTGRES_USER=postgres", "-e", "POSTGRES_PASSWORD=upgrade", "-e", "POSTGRES_SSL_MODE=disable",
		"-e", "SQLITE_PATH=/app/data/replayvod.db", "-e", "HOST=0.0.0.0", "-e", "PORT=8080",
		"-e", "SERVER_MODE=off", "-e", "SESSION_SECRET=replayvod-upgrade-synthetic-secret-only",
		"-e", "WHITELIST_ENABLED=true", "-e", "DEVELOPMENT=false", "-e", "PUBLIC_BASE_URL=http://localhost:8080",
		"-e", "VIDEO_DIR=/app/data/videos", "-e", "THUMBNAIL_DIR=/app/data/thumbnails", "-e", "SCRATCH_DIR=/app/data/.scratch",
	}
}

func (h *installation) maintenance(image, stage string) {
	h.app = h.network + "-" + stage
	h.container(h.app, append(h.applicationArgs(), "--entrypoint", "sleep", image, "infinity")...)
}

func (h *installation) start(image, stage string) {
	h.t.Helper()
	h.app = h.network + "-" + stage
	h.container(h.app, append(h.applicationArgs(), image)...)

	h.waitFor(stage, func() bool {
		_, err := command(5*time.Second, nil, "docker", "exec", h.app, "wget", "-qO-", "http://127.0.0.1:8080/api/v1/health")
		return err == nil
	})
}

func (h *installation) stop() {
	docker(h.t, "stop", "--time", "15", h.app)
	h.capture(h.app)
	if code := docker(h.t, "inspect", "--format", "{{.State.ExitCode}}", h.app); code != "0" {
		h.t.Fatalf("application shutdown exit code %s", code)
	}
}

type probeRequest struct {
	Exec    []string          `json:"exec,omitempty"`
	Queries map[string]string `json:"queries,omitempty"`
	Files   map[string][]byte `json:"files,omitempty"`
}

func (h *installation) probe(req probeRequest) snapshot {
	h.t.Helper()
	result, err := h.tryProbe(req)
	if err != nil {
		h.t.Fatal(err)
	}
	return result
}

func (h *installation) tryProbe(req probeRequest) (snapshot, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	out, err := command(time.Minute, data, "docker", "exec", "-i", h.app, "/upgrade-probe")
	if err != nil {
		return nil, err
	}
	var result snapshot
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("decode database probe: %w: %s", err, out)
	}
	return result, nil
}

func (h *installation) rpc(method, procedure, user string, input any, status int) any {
	h.t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		h.t.Fatal(err)
	}
	path := "/trpc/" + procedure
	var body io.Reader
	if method == http.MethodGet {
		path += "?input=" + url.QueryEscape(string(data))
	} else {
		body = bytes.NewReader(data)
	}
	resp, output := h.request(method, path, user, body, nil)
	if resp.StatusCode != status {
		h.t.Fatalf("%s %s: status %d, want %d: %s", method, procedure, resp.StatusCode, status, output)
	}
	if status != http.StatusOK {
		return nil
	}
	var envelope struct {
		Result struct {
			Data any `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil || envelope.Result.Data == nil {
		h.t.Fatalf("%s invalid RPC response: %s (%v)", procedure, output, err)
	}
	return envelope.Result.Data
}

func (h *installation) request(method, path, user string, body io.Reader, headers map[string]string) (*http.Response, []byte) {
	h.t.Helper()
	if headers == nil {
		headers = map[string]string{}
	}
	if user != "" {
		headers["Cookie"] = "session_id=" + strings.Repeat(user, 64)
	}
	headers["Content-Type"] = "application/json"
	headers["Origin"] = "http://localhost:8080"
	var data []byte
	if body != nil {
		var err error
		data, err = io.ReadAll(body)
		if err != nil {
			h.t.Fatal(err)
		}
	}
	input, err := json.Marshal(map[string]any{"http": map[string]any{"method": method, "path": path, "headers": headers, "body": data}})
	if err != nil {
		h.t.Fatal(err)
	}
	output, err := command(20*time.Second, input, "docker", "exec", "-i", h.app, "/upgrade-probe")
	if err != nil {
		h.t.Fatal(err)
	}
	var response struct {
		Status  int         `json:"status"`
		Headers http.Header `json:"headers"`
		Body    []byte      `json:"body"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		h.t.Fatal(err)
	}
	return &http.Response{StatusCode: response.Status, Header: response.Headers}, response.Body
}
