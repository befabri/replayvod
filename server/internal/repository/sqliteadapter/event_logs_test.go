package sqliteadapter

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

// TestEventLog_Append_PreservesJSONData pins SQLite's byte-exact storage of
// the data column, which the contract suite cannot promise because Postgres
// JSONB reorders keys. The semantic round trip both backends owe is
// EventLog_DataRoundTrip in contracttest.
func TestEventLog_Append_PreservesJSONData(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	data := json.RawMessage(`{"schedule_id":42,"job_id":"abc-123"}`)
	row, err := a.CreateEventLog(ctx, &repository.EventLogInput{
		Domain:    "schedule",
		EventType: "auto_download_triggered",
		Severity:  repository.EventLogSeverityInfo,
		Message:   "schedule fired",
		Data:      data,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if row.CreatedAt.IsZero() {
		t.Error("created_at must be populated by DB default")
	}
	if string(row.Data) != string(data) {
		t.Errorf("data round-trip differs: want %q got %q", string(data), string(row.Data))
	}
}
