//go:build upgrade

package upgrade

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpgrade(t *testing.T) {
	m := checkHistoricalFixtures(t)
	image := os.Getenv("UPGRADE_CANDIDATE_IMAGE")
	if image == "" {
		t.Fatal("UPGRADE_CANDIDATE_IMAGE must name a locally built image; run task test-upgrade from server/")
	}
	image = docker(t, "image", "inspect", "--format", "{{.Id}}", image)
	checkCandidateMigrations(t, image)
	arch := docker(t, "image", "inspect", "--format", "{{.Architecture}}", image)
	probe := filepath.Join(t.TempDir(), "probe")
	out, err := command(3*time.Minute, nil, "env", "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+arch, "go", "build", "-o", probe, "./probe")
	if err != nil {
		t.Fatalf("build probe: %v: %s", err, out)
	}
	if err := os.Chmod(probe, 0755); err != nil {
		t.Fatal(err)
	}
	artifacts := artifactRoot(t)
	cases, err := selectCases(m, os.Getenv("UPGRADE_SCOPE"), os.Getenv("UPGRADE_BASELINE"), os.Getenv("UPGRADE_BACKEND"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		b, backend, size := c.baseline, c.backend, c.size
		t.Run(b.Version+"/"+backend+"/"+size, func(t *testing.T) {
			previousImage := baselineImage(t, b, arch)
			p, err := projectionFor(transformations, candidateMigrations(backend), backend, b)
			if err != nil {
				t.Fatal(err)
			}
			h := newInstallation(t, m, backend, probe, filepath.Join(artifacts, b.Version, backend, size))
			metadata, _ := json.MarshalIndent(map[string]any{"baseline": b.Version, "commit": b.Commit, "baseline_image": b.Image, "platform_image": previousImage, "candidate_image": image, "architecture": arch, "postgres_image": m.PostgresImage, "size": size, "added_migrations": p.added}, "", "  ")
			h.artifact("run.json", metadata)
			h.start(previousImage, "previous")
			h.assertLedger(b.MigrationSHA256, false)
			h.seed(b, size)
			h.seedServed()
			before, columns := h.baselineSnapshot("before")
			p.columns = columns
			h.stop()
			started := time.Now()
			h.start(image, "candidate")
			t.Logf("candidate ready after %s", time.Since(started).Round(time.Millisecond))
			after := h.projectSnapshot(before, "after", p)
			if err := p.compare(withoutLedger(before), withoutLedger(after)); err != nil {
				t.Fatal(err)
			}
			h.assertLedger(b.MigrationSHA256, true)
			h.assertOldLedger(before, after)
			h.checkIntegrity()
			h.verifyPreserved(seededPosition(b, backend))
			h.writeApplication()
			written := h.takeSnapshot(nil, "written")
			h.stop()
			h.start(image, "restart")
			restarted := h.takeSnapshot(written, "restarted")
			if err := compareSnapshots(written, restarted); err != nil {
				t.Fatalf("restart: %v", err)
			}
			h.checkIntegrity()
			h.verifyPreserved(150.25)
			h.assertNewFeatures()
			if b.Version == m.Latest {
				h.downgrade(image, previousImage, b, before, p)
			}
			h.stop()
			if size == "normal" {
				t.Run("recording-recovery", func(t *testing.T) {
					recovery := newInstallation(t, m, backend, probe, filepath.Join(artifacts, b.Version, backend, "recovery"))
					recovery.recoverRecordings(previousImage, image, b)
				})
			}
		})
	}
}

// baselineImage returns the baseline's pinned image for the candidate's
// architecture, pulling it only when the Docker engine does not hold it yet.
func baselineImage(t *testing.T, b baseline, arch string) string {
	t.Helper()
	ref := b.PlatformImages[arch]
	if ref == "" {
		t.Fatalf("baseline %s pins no linux/%s image", b.Version, arch)
	}
	if _, err := command(time.Minute, nil, "docker", "image", "inspect", ref); err != nil {
		docker(t, "pull", "--platform", "linux/"+arch, ref)
	}
	if platform := docker(t, "image", "inspect", "--format", "{{.Os}}/{{.Architecture}}", ref); platform != "linux/"+arch {
		t.Fatalf("baseline image %s is %s, want linux/%s", ref, platform, arch)
	}
	if revision := docker(t, "image", "inspect", "--format", `{{index .Config.Labels "org.opencontainers.image.revision"}}`, ref); revision != b.Commit {
		t.Fatalf("baseline image revision %s differs from manifest %s", revision, b.Commit)
	}
	return ref
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (h *installation) seed(b baseline, size string) {
	var sql []string
	for _, name := range b.SeedFiles[h.backend] {
		sql = append(sql, string(fixture(h.t, name)))
	}
	if size == "large" {
		sql = append(sql, string(fixture(h.t, "large.sql")))
		if h.backend == "postgres" {
			sql = append(sql, "SELECT setval(pg_get_serial_sequence('videos', 'id'), (SELECT MAX(id) FROM videos))")
		}
	}
	h.probe(probeRequest{Exec: sql, Files: map[string][]byte{"videos/upgrade-recording-part00.mp4": fixture(h.t, "recording.mp4")}})
}

// baselineSnapshot reads every table of the running historical image with its
// column names, so renamed columns of empty tables can still be aliased.
func (h *installation) baselineSnapshot(name string) (snapshot, schema) {
	tables := h.tableQueries()
	queries := map[string]string{}
	for table := range tables {
		literal := "'" + strings.ReplaceAll(table, "'", "''") + "'"
		queries[table] = "SELECT name FROM pragma_table_info(" + literal + ")"
		if h.backend == "postgres" {
			queries[table] = "SELECT column_name AS name FROM information_schema.columns WHERE table_schema='public' AND table_name=" + literal
		}
	}
	columns := schema{}
	for table, rows := range h.probe(probeRequest{Queries: queries}) {
		for _, row := range rows {
			columns[table] = append(columns[table], row["name"].(string))
		}
		if len(columns[table]) == 0 {
			h.t.Fatalf("no columns found for table %s", table)
		}
	}
	return h.readSnapshot(name, tables), columns
}

// takeSnapshot reads every table of the running image, or the tables and
// columns of a previous snapshot when the schema has not changed in between.
func (h *installation) takeSnapshot(previous snapshot, name string) snapshot {
	if previous == nil {
		return h.readSnapshot(name, h.tableQueries())
	}
	return h.readSnapshot(name, projection{}.queries(previous))
}

// projectSnapshot reads the historical tables through the candidate schema under
// the declared transformations. A failing query names the declaration site.
func (h *installation) projectSnapshot(previous snapshot, name string, p projection) snapshot {
	h.t.Helper()
	result, err := h.tryProbe(probeRequest{Queries: p.queries(previous)})
	if err != nil {
		h.t.Fatal(p.explain(err))
	}
	h.saveSnapshot(name, result)
	return result
}

func (h *installation) tableQueries() map[string]string {
	query := "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'event_logs_fts%'"
	if h.backend == "postgres" {
		query = "SELECT tablename AS name FROM pg_tables WHERE schemaname='public'"
	}
	queries := map[string]string{}
	for _, row := range h.probe(probeRequest{Queries: map[string]string{"tables": query}})["tables"] {
		table := row["name"].(string)
		queries[table] = "SELECT * FROM " + quoteIdentifier(table)
	}
	return queries
}

func (h *installation) readSnapshot(name string, queries map[string]string) snapshot {
	result := h.probe(probeRequest{Queries: queries})
	h.saveSnapshot(name, result)
	return result
}

func (h *installation) saveSnapshot(name string, result snapshot) {
	data, _ := json.MarshalIndent(result, "", "  ")
	h.artifact(name+".json", data)
}

func withoutLedger(s snapshot) snapshot {
	out := snapshot{}
	for table, rows := range s {
		if table != "schema_migrations" {
			out[table] = rows
		}
	}
	return out
}

func (h *installation) assertOldLedger(before, after snapshot) {
	old := map[any]bool{}
	for _, row := range before["schema_migrations"] {
		old[row["version"]] = true
	}
	filtered := snapshot{"schema_migrations": {}}
	for _, row := range after["schema_migrations"] {
		if old[row["version"]] {
			filtered["schema_migrations"] = append(filtered["schema_migrations"], row)
		}
	}
	if err := compareSnapshots(snapshot{"schema_migrations": before["schema_migrations"]}, filtered); err != nil {
		h.t.Fatal(err)
	}
}

func (h *installation) assertLedger(historical map[string]string, current bool) {
	versions := map[string]bool{}
	if current {
		files, err := fs.Glob(candidateMigrations(h.backend), "*.up.sql")
		if err != nil {
			h.t.Fatal(err)
		}
		for _, path := range files {
			versions[strings.TrimSuffix(filepath.Base(path), ".up.sql")] = true
		}
	} else {
		for path := range historical {
			if strings.Contains(path, "/"+h.backend+"/") && strings.HasSuffix(path, ".up.sql") {
				versions[strings.TrimSuffix(filepath.Base(path), ".up.sql")] = true
			}
		}
	}
	rows := h.probe(probeRequest{Queries: map[string]string{"ledger": "SELECT version FROM schema_migrations"}})["ledger"]
	if len(rows) != len(versions) {
		h.t.Fatalf("migration ledger has %d rows, want %d", len(rows), len(versions))
	}
	for _, row := range rows {
		if !versions[row["version"].(string)] {
			h.t.Fatalf("unexpected migration: %v", row)
		}
	}
}

func (h *installation) checkIntegrity() {
	if h.backend == "sqlite" {
		result := h.probe(probeRequest{Queries: map[string]string{"foreign_keys": "PRAGMA foreign_key_check", "integrity": "PRAGMA integrity_check", "journal": "PRAGMA journal_mode"}})
		if len(result["foreign_keys"]) != 0 || len(result["integrity"]) != 1 || result["integrity"][0]["integrity_check"] != "ok" || result["journal"][0]["journal_mode"] != "wal" {
			h.t.Fatalf("SQLite integrity: %v", result)
		}
	} else {
		result := h.probe(probeRequest{Queries: map[string]string{"invalid": "SELECT conname FROM pg_constraint WHERE connamespace='public'::regnamespace AND NOT convalidated", "version": "SELECT current_setting('server_version_num')::int / 10000 AS major"}})
		if len(result["invalid"]) != 0 || len(result["version"]) != 1 || result["version"][0]["major"] != float64(h.postgresMajor) {
			h.t.Fatalf("PostgreSQL integrity/version: %v", result)
		}
	}
}

// seededPosition is the playback progress the seed stored, or zero for
// baselines released before playback state existed.
func seededPosition(b baseline, backend string) float64 {
	if b.hasMigration(backend, "044_user_states") {
		return 123.5
	}
	return 0
}

// seedServed checks the seed through the surface every release shares. It runs
// against historical images, so it must not assert field shapes.
func (h *installation) seedServed() {
	for cookie, role := range map[string]string{"a": "owner", "b": "admin", "c": "viewer"} {
		data := h.rpc("GET", "auth.session", cookie, nil, 200).(map[string]any)
		if data["role"] != role {
			h.t.Fatalf("session role: %v, want %s", data, role)
		}
	}
	h.rpc("GET", "auth.session", "", nil, 401)
	h.rpc("GET", "schedule.getById", "b", map[string]any{"id": 1}, 200)
	h.rpc("GET", "video.getById", "c", map[string]any{"id": 1}, 200)
	want := fixture(h.t, "recording.mp4")
	resp, data := h.request("GET", "/api/v1/videos/1/parts/0/stream", "c", nil, nil)
	if resp.StatusCode != 200 || !bytes.Equal(data, want) {
		h.t.Fatalf("recording bytes: status %d, length %d; want %d", resp.StatusCode, len(data), len(want))
	}
	resp, data = h.request("GET", "/api/v1/videos/1/parts/0/stream", "c", nil, map[string]string{"Range": "bytes=1-15"})
	if resp.StatusCode != 206 || !bytes.Equal(data, want[1:16]) || resp.Header.Get("Content-Range") != fmt.Sprintf("bytes 1-15/%d", len(want)) {
		h.t.Fatalf("recording seeking: status %d, headers %v", resp.StatusCode, resp.Header)
	}
	resp, _ = h.request("GET", "/api/v1/videos/1/parts/0/stream", "", nil, nil)
	if resp.StatusCode != 401 {
		h.t.Fatalf("anonymous recording access: %d", resp.StatusCode)
	}
}

// verifyPreserved asserts the candidate exposes the seeded state in detail.
// Only the candidate speaks the current API, so field assertions live here.
func (h *installation) verifyPreserved(position float64) {
	h.seedServed()
	schedule := h.rpc("GET", "schedule.getById", "b", map[string]any{"id": 1}, 200).(map[string]any)
	for name, expected := range map[string]any{"quality": "MEDIUM", "trigger_count": float64(17), "requested_by": "1002", "min_viewers": float64(250), "is_disabled": false} {
		if schedule[name] != expected {
			h.t.Fatalf("schedule.%s = %v, want %v", name, schedule[name], expected)
		}
	}
	if len(schedule["categories"].([]any)) != 1 || len(schedule["tags"].([]any)) != 1 {
		h.t.Fatalf("lost schedule filters: %v", schedule)
	}
	h.verifyPlayback(position)
}

// verifyPlayback checks the viewer's saved progress; zero means none was stored.
func (h *installation) verifyPlayback(position float64) {
	video := h.rpc("GET", "video.getById", "c", map[string]any{"id": 1}, 200).(map[string]any)
	state, ok := video["user_state"].(map[string]any)
	if !ok && position != 0 {
		h.t.Fatalf("missing video state: %v", video)
	}
	if ok && state["last_position_seconds"] != position {
		h.t.Fatalf("playback progress: %v, want %v", state, position)
	}
}

func (h *installation) writeApplication() {
	h.rpc("GET", "video.statisticsByBroadcaster", "c", map[string]any{"broadcaster_id": "2001"}, 200)
	h.rpc("GET", "system.listUsers", "b", nil, 200)
	h.rpc("GET", "system.listUsers", "c", nil, 403)
	h.rpc("POST", "system.createInvite", "b", map[string]any{"role": "viewer", "ttl_minutes": 60, "note": "Upgrade verification"}, 200)
	h.rpc("POST", "video.updateWatchProgress", "c", map[string]any{"video_id": 1, "position_seconds": 150.25, "observed_at_ms": 2000000000}, 200)
	h.rpc("POST", "schedule.createRequest", "c", map[string]any{"broadcaster_id": "2002", "note": "Keep after restart"}, 200)
	h.rpc("POST", "schedule.createRequest", "c", map[string]any{"broadcaster_id": "2002"}, 400)
	requests := h.rpc("GET", "schedule.myRequests", "c", map[string]any{"limit": 1}, 200).(map[string]any)["items"].([]any)
	if len(requests) != 1 {
		h.t.Fatalf("requests: %v", requests)
	}
	id := requests[0].(map[string]any)["id"]
	input := map[string]any{"request_id": id, "quality": "HIGH", "is_disabled": true, "category_ids": []string{}, "tag_ids": []int{}}
	h.rpc("POST", "schedule.approveRequest", "c", input, 403)
	created := h.rpc("POST", "schedule.approveRequest", "b", input, 200).(map[string]any)
	if created["id"].(float64) <= 2 || created["requested_from"] != "1003" || created["requested_by"] != "1002" {
		h.t.Fatalf("approved schedule: %v", created)
	}
	h.rpc("POST", "schedule.approveRequest", "b", input, 400)
	previous := h.probe(probeRequest{Queries: map[string]string{"max": "SELECT MAX(id) AS id FROM videos"}})["max"][0]["id"].(float64)
	h.probe(probeRequest{Exec: []string{"INSERT INTO videos (job_id, filename, display_name, broadcaster_id, status) VALUES ('after-upgrade', 'after-upgrade', 'New recording', '2001', 'DONE')"}})
	inserted := h.probe(probeRequest{Queries: map[string]string{"new": "SELECT id FROM videos WHERE job_id='after-upgrade'"}})["new"][0]["id"].(float64)
	if inserted <= previous {
		h.t.Fatalf("recording ID sequence moved backwards: %v <= %v", inserted, previous)
	}
	h.assertNewFeatures()
}

func (h *installation) assertNewFeatures() {
	invites := h.rpc("GET", "system.listInvites", "b", nil, 200).([]any)
	if len(invites) != 1 {
		h.t.Fatalf("invitation not retained: %v", invites)
	}
	requests := h.rpc("GET", "schedule.myRequests", "c", map[string]any{"limit": 1}, 200).(map[string]any)["items"].([]any)
	if len(requests) != 1 || requests[0].(map[string]any)["status"] != "APPROVED" {
		h.t.Fatalf("request decision not retained: %v", requests)
	}
}
