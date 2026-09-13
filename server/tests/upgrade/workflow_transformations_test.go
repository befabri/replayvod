package upgrade

import (
	"encoding/hex"
	"encoding/json"
	"maps"
	"reflect"
	"testing"
)

func failOrphanAdmissions(rows []map[string]any, historical snapshot) {
	jobs, saved := map[any]bool{}, map[any]bool{}
	for _, row := range historical["jobs"] {
		jobs[row["id"]] = true
	}
	for _, row := range historical["video_parts"] {
		if size, ok := row["size_bytes"].(float64); ok && size > 0 {
			saved[row["video_id"]] = true
		}
	}
	for _, row := range rows {
		if (row["status"] != "PENDING" && row["status"] != "RUNNING") || jobs[row["job_id"]] {
			continue
		}
		row["status"] = "FAILED"
		row["error"] = "Admission interrupted before an attempt was created"
		row["completion_kind"] = "complete"
		size, _ := row["size_bytes"].(float64)
		if saved[row["id"]] || size > 0 {
			row["completion_kind"] = "partial"
		}
	}
}

// checkpointValue normalizes the probe's PostgreSQL hex and SQLite text JSON
// encodings so formatting differences do not count as data loss.
func checkpointValue(raw any) any {
	text, ok := raw.(string)
	if !ok {
		return raw
	}
	data := []byte(text)
	if !json.Valid(data) {
		decoded, err := hex.DecodeString(text)
		if err != nil || !json.Valid(decoded) {
			return raw
		}
		data = decoded
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return raw
	}
	return value
}

func compareCheckpointRows(before, after []map[string]any) error {
	normalize := func(rows []map[string]any) []map[string]any {
		copy := cloneSnapshot(snapshot{"jobs": rows})["jobs"]
		for _, row := range copy {
			row["resume_state"] = checkpointValue(row["resume_state"])
		}
		return copy
	}
	return compareRows("jobs", normalize(before), normalize(after))
}

func upgradeRecordingCheckpoints(rows []map[string]any, _ snapshot) {
	for _, row := range rows {
		state, ok := checkpointValue(row["resume_state"]).(map[string]any)
		if !ok || !normalizableCheckpoint(state) {
			continue
		}
		state = maps.Clone(state)
		if state["stage"] == nil {
			state["stage"] = "AUTH"
		}
		index, _ := state["current_part_index"].(float64)
		state["current_part_index"] = max(index, 1)
		started, _ := state["part_started"].(bool)
		for _, key := range []string{"part_start_media_sequence", "accounted_frontier_media_sequence"} {
			number, _ := state[key].(float64)
			started = started || number != 0
		}
		bytes, _ := state["part_bytes"].(float64)
		started = started || bytes > 0
		for _, key := range []string{"completed_above_frontier", "gaps"} {
			items, _ := state[key].([]any)
			started = started || len(items) > 0
		}
		switch state["stage"] {
		case "PREPARE_INPUT", "REMUX", "PROBE", "THUMBNAIL", "CORRUPTION_CHECK", "STORE":
			started = true
		}
		state["part_started"] = started
		row["resume_state"] = state
	}
}

func normalizableCheckpoint(state map[string]any) bool {
	if _, ok := state["stage"].(string); state["stage"] != nil && !ok {
		return false
	}
	for _, key := range []string{"current_part_index", "part_start_media_sequence", "accounted_frontier_media_sequence", "part_bytes"} {
		if _, ok := state[key].(float64); state[key] != nil && !ok {
			return false
		}
	}
	_, ok := state["part_started"].(bool)
	return state["part_started"] == nil || ok
}

func TestWorkflowUpgradeRulesPreserveUnrelatedValues(t *testing.T) {
	before := snapshot{
		"videos": {
			{"id": float64(1), "job_id": "orphan", "status": "PENDING", "error": nil, "completion_kind": "complete", "title": "untouched"},
			{"id": float64(2), "job_id": "partial", "status": "RUNNING", "error": nil, "completion_kind": "complete"},
			{"id": float64(3), "job_id": "owned", "status": "RUNNING", "error": nil, "completion_kind": "complete"},
			{"id": float64(4), "job_id": "single-pending", "status": "PENDING", "size_bytes": float64(100), "error": nil, "completion_kind": "complete"},
			{"id": float64(5), "job_id": "single-running", "status": "RUNNING", "size_bytes": float64(100), "error": nil, "completion_kind": "complete"},
		},
		"jobs":                   {{"id": "owned", "status": "RUNNING", "resume_state": `{"stage":"SEGMENTS","part_bytes":12,"custom":"kept"}`}},
		"video_parts":            {{"id": float64(10), "video_id": float64(2), "size_bytes": float64(12)}},
		"video_metadata_changes": {},
	}
	p := projection{rules: map[string]tableRule{
		"jobs": {compare: compareCheckpointRows}, "video_parts": grows(), "video_metadata_changes": grows(),
	}, changes: []valueChange{{"videos", tableRule{up: failOrphanAdmissions}}, {"jobs", tableRule{up: upgradeRecordingCheckpoints}}}}
	after := cloneSnapshot(before)
	after["videos"][0]["status"], after["videos"][0]["error"] = "FAILED", "Admission interrupted before an attempt was created"
	after["videos"][1]["status"], after["videos"][1]["error"], after["videos"][1]["completion_kind"] = "FAILED", "Admission interrupted before an attempt was created", "partial"
	for _, index := range []int{3, 4} {
		after["videos"][index]["status"], after["videos"][index]["error"], after["videos"][index]["completion_kind"] = "FAILED", "Admission interrupted before an attempt was created", "partial"
	}
	after["jobs"][0]["resume_state"] = hex.EncodeToString([]byte(`{"part_started":true,"stage":"SEGMENTS","part_bytes":12,"current_part_index":1,"custom":"kept"}`))
	after["video_parts"] = append(after["video_parts"], map[string]any{"id": float64(11), "video_id": float64(4), "size_bytes": float64(100)})
	after["video_metadata_changes"] = []map[string]any{{"id": float64(1), "video_id": float64(2)}}
	if err := p.compare(before, after); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(snapshot){
		"title rewritten":                         func(s snapshot) { s["videos"][0]["title"] = "lost" },
		"pending single-file media misclassified": func(s snapshot) { s["videos"][3]["completion_kind"] = "complete" },
		"running single-file media misclassified": func(s snapshot) { s["videos"][4]["completion_kind"] = "complete" },
		"owned admission failed":                  func(s snapshot) { s["videos"][2]["status"] = "FAILED" },
		"checkpoint field lost": func(s snapshot) {
			s["jobs"][0]["resume_state"] = `{"part_started":true,"stage":"SEGMENTS","part_bytes":12,"current_part_index":1}`
		},
		"job status rewritten": func(s snapshot) { s["jobs"][0]["status"] = "FAILED" },
		"saved part deleted":   func(s snapshot) { s["video_parts"] = s["video_parts"][1:] },
	} {
		t.Run(name, func(t *testing.T) {
			corrupt := cloneSnapshot(after)
			mutate(corrupt)
			if err := p.compare(before, corrupt); err == nil {
				t.Fatal("undeclared data loss passed upgrade gate")
			}
		})
	}
}

func TestCheckpointUpgradeRulesLeaveCorruptionAndInputUntouched(t *testing.T) {
	for _, raw := range []string{`{"stage":42}`, `{"part_bytes":"bad"}`, `{"part_started":1}`, `[]`, `null`, `invalid`} {
		rows := []map[string]any{{"resume_state": raw}}
		upgradeRecordingCheckpoints(rows, nil)
		if rows[0]["resume_state"] != raw {
			t.Fatalf("corrupt checkpoint normalized: %s", raw)
		}
	}
	state := map[string]any{"stage": "AUTH", "current_part_index": float64(0)}
	rows := []map[string]any{{"resume_state": state}}
	upgradeRecordingCheckpoints(rows, nil)
	if !reflect.DeepEqual(state, map[string]any{"stage": "AUTH", "current_part_index": float64(0)}) {
		t.Fatal("comparison mutated historical checkpoint")
	}
	if got := rows[0]["resume_state"].(map[string]any); got["current_part_index"] != float64(1) || got["part_started"] != false {
		t.Fatalf("normalization = %+v", got)
	}
}
