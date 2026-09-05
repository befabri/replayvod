//go:build upgrade

package upgrade

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

func (h *installation) recoverRecordings(previous, candidate string, b baseline) {
	h.start(previous, "previous")
	h.seed(b, "normal")
	h.stop()
	h.maintenance(previous, "interrupted")
	// Seed after shutdown so the old process cannot consume the saved checkpoint.
	var statements []string
	for _, file := range b.RecoveryFiles[h.backend] {
		statements = append(statements, string(fixture(h.t, file)))
	}
	h.probe(probeRequest{Exec: statements, Files: map[string][]byte{
		".scratch/upgrade-resume/part01/segments/0.ts": fixture(h.t, "recording.ts"),
		"videos/upgrade-partial-part01.mp4":            fixture(h.t, "recording.mp4"),
	}})
	before := h.recoverySnapshot("running")
	for _, row := range before["videos"] {
		if row["status"] != "RUNNING" {
			h.t.Fatalf("fixture is not in progress: %v", row)
		}
	}
	docker(h.t, "stop", "--time", "1", h.app)
	h.start(candidate, "candidate")
	h.waitFor("recording recovery", func() bool {
		rows := h.probe(probeRequest{Queries: map[string]string{"active": "SELECT id FROM jobs WHERE status IN ('PENDING', 'RUNNING')"}})
		return len(rows["active"]) == 0
	})
	h.assertRecoveredRecordings()
	h.checkIntegrity()
	h.verifyPreserved(seededPosition(b, h.backend))
	recovered := h.recoverySnapshot("recovered")
	h.stop()
	h.start(candidate, "restart")
	h.assertRecoveredRecordings()
	restarted := h.recoverySnapshot("recovery-restart")
	if err := compareSnapshots(recovered, restarted); err != nil {
		h.t.Fatalf("recovery repeated on restart: %v", err)
	}
	h.stop()
}

func (h *installation) recoverySnapshot(name string) snapshot {
	result := h.probe(probeRequest{Queries: map[string]string{
		"videos": "SELECT * FROM videos WHERE id BETWEEN 6001 AND 6003",
		"jobs":   "SELECT * FROM jobs WHERE video_id BETWEEN 6001 AND 6003",
		"parts":  "SELECT * FROM video_parts WHERE video_id BETWEEN 6001 AND 6003",
	}})
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		h.t.Fatal(err)
	}
	h.artifact(name+".json", data)
	return result
}

func (h *installation) assertRecoveredRecordings() {
	result := h.recoverySnapshot("recovery-state")
	if len(result["videos"]) != 3 || len(result["jobs"]) != 3 || len(result["parts"]) != 2 {
		h.t.Fatalf("lost or duplicated recovery rows: %v", result)
	}
	for _, video := range result["videos"] {
		id := video["id"].(float64)
		status, kind, truncated := "FAILED", "complete", true
		if id == 6001 {
			status, truncated = "DONE", false
		}
		if id == 6002 {
			kind = "partial"
		}
		if video["status"] != status || video["completion_kind"] != kind || (video["truncated"] == true || video["truncated"] == float64(1)) != truncated {
			h.t.Fatalf("incorrect recovery classification: %v", video)
		}
	}
	for _, job := range result["jobs"] {
		status := "FAILED"
		if job["video_id"] == float64(6001) {
			status = "DONE"
		}
		if job["status"] != status || job["finished_at"] == nil {
			h.t.Fatalf("unfinished recovery job: %v", job)
		}
		if status == "FAILED" && (job["error"] == nil || job["error"] == "") {
			h.t.Fatalf("missing recovery error: %v", job)
		}
	}
	for _, part := range result["parts"] {
		id := int(part["video_id"].(float64))
		if part["duration_seconds"].(float64) <= 0 || part["size_bytes"].(float64) <= 0 {
			h.t.Fatalf("unfinalized recovered part: %v", part)
		}
		response, body := h.request("GET", fmt.Sprintf("/api/v1/videos/%d/parts/1/stream", id), "c", nil, nil)
		if id == 6002 {
			if response.StatusCode != 404 {
				h.t.Fatalf("failed recording unexpectedly served: %d", response.StatusCode)
			}
			saved, err := command(5*time.Second, nil, "docker", "exec", h.app, "cat", "/app/data/videos/upgrade-partial-part01.mp4")
			if err != nil || !bytes.Equal(saved, fixture(h.t, "recording.mp4")) {
				h.t.Fatalf("partial recording bytes changed: %v", err)
			}
			continue
		}
		if response.StatusCode != 200 || len(body) < 16 {
			h.t.Fatalf("recovered recording %d is unplayable: %d", id, response.StatusCode)
		}

		response, chunk := h.request("GET", fmt.Sprintf("/api/v1/videos/%d/parts/1/stream", id), "c", nil, map[string]string{"Range": "bytes=1-15"})
		if response.StatusCode != 206 || !bytes.Equal(chunk, body[1:16]) {
			h.t.Fatalf("recovered recording %d cannot seek", id)
		}
	}
}
