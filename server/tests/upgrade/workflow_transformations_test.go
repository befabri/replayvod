package upgrade

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
		if saved[row["id"]] {
			row["completion_kind"] = "partial"
		}
	}
}
