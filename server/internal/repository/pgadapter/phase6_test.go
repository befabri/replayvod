package pgadapter

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// TestEventLog_Append_PreservesJSONData checks the JSONB round-trip. PG may
// reformat JSONB (key-order / whitespace), so this compares semantically. The
// semantic round-trip is also covered backend-agnostically by the contract
// suite (WebhookEvent_PayloadRoundTrip); this stays here to document the PG
// JSONB reformatting behavior explicitly (SQLite's mirror pins byte equality).
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

	var want, got any
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	if err := json.Unmarshal(row.Data, &got); err != nil {
		t.Fatalf("unmarshal got: %v", err)
	}
	if wj, _ := json.Marshal(want); string(wj) != mustMarshal(got) {
		t.Errorf("data semantic mismatch: want %v got %v", want, got)
	}
}

func mustMarshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TestSettings_UserCascadeDelete confirms ON DELETE CASCADE on
// settings.user_id. FK enforcement is always on in PostgreSQL, unlike SQLite
// where a per-connection pragma is required — so this stays a per-adapter test.
func TestSettings_UserCascadeDelete(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPGPool(t)
	a := New(pool)

	if _, err := a.UpsertUser(ctx, &repository.User{
		ID: "u-cascade", Login: "u", DisplayName: "u", Role: "viewer",
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := a.UpsertSettings(ctx, &repository.Settings{
		UserID: "u-cascade", Timezone: "UTC", DatetimeFormat: "ISO", Language: "en",
	}); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", "u-cascade"); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	_, err := a.GetSettings(ctx, "u-cascade")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("settings row must be cascaded on user delete; got %v", err)
	}
}
