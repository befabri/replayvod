package sqliteadapter

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/contracttest"
)

// TestRecordingIntentDeadlineKeepsNanoseconds pins the restart deadline to
// the precise layout: it is the one SQLite timestamp compared against
// instants with sub-second parts, and the layout is what rows written before
// the typed binding already carry.
func TestRecordingIntentDeadlineKeepsNanoseconds(t *testing.T) {
	a := newTestAdapter(t)
	ctx := t.Context()
	contracttest.SeedUserChannel(t, ctx, a, "owner", "precise-channel")
	if err := a.CreateRecordingIntent(ctx, repository.RecordingIntent{ID: "precise", BroadcasterID: "precise-channel", CurrentJobID: "precise-first", Params: json.RawMessage(`{}`), WaitSeconds: 120}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Date(2030, 1, 2, 3, 4, 5, 678_000_000, time.UTC)
	if err := a.SetRecordingIntentWaiting(ctx, "precise", "precise-first", deadline); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := a.db.QueryRowContext(ctx, "SELECT wait_until FROM recording_intents WHERE id = ?", "precise").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "2030-01-02 03:04:05.678000000" {
		t.Fatalf("wait_until stored as %q", stored)
	}
	intent, err := a.GetRecordingIntent(ctx, "precise")
	if err != nil || intent.WaitUntil == nil || !intent.WaitUntil.Equal(deadline) {
		t.Fatalf("deadline read back as %+v, %v", intent, err)
	}
	if err := a.ActivateRecordingIntent(ctx, "precise", "precise-first", "precise-second", "stream", deadline.Add(time.Millisecond)); !errors.Is(err, repository.ErrStaleExecution) {
		t.Fatalf("observation a millisecond past the deadline activated: %v", err)
	}
	if err := a.ActivateRecordingIntent(ctx, "precise", "precise-first", "precise-second", "stream", deadline); err != nil {
		t.Fatalf("observation on the deadline: %v", err)
	}
}
