package recordingwebhook

import (
	"regexp"
	"testing"
	"time"
)

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

// TestNewTerminalDeliveryInput_Deterministic pins the contract the silent-drop
// fix relies on: a terminal delivery payload is always non-nil, its message id
// is derived from (event, video) rather than the RNG (so minting can never
// fail), and it is stable across calls. A regression that reintroduced a random
// or failable id would break the "terminal transition and its durable webhook
// row are all-or-nothing" guarantee.
func TestNewTerminalDeliveryInput_Deterministic(t *testing.T) {
	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)

	a := NewTerminalDeliveryInput(EventCompleted, 42, now)
	b := NewTerminalDeliveryInput(EventCompleted, 42, now)

	if a == nil || b == nil {
		t.Fatal("NewTerminalDeliveryInput returned nil; a terminal event must always have a row to enqueue")
	}
	if a.MessageID != b.MessageID {
		t.Errorf("message id not stable across calls: %q vs %q", a.MessageID, b.MessageID)
	}
	if a.DedupeKey != b.DedupeKey {
		t.Errorf("dedupe key not stable across calls: %q vs %q", a.DedupeKey, b.DedupeKey)
	}
	if !hex32.MatchString(a.MessageID) {
		t.Errorf("message id %q is not the 32-hex-char shape receivers expect", a.MessageID)
	}
	if want := "recording.completed:42"; a.DedupeKey != want {
		t.Errorf("dedupe key = %q, want %q", a.DedupeKey, want)
	}
	if a.Event != EventCompleted || a.VideoID != 42 {
		t.Errorf("event/video = %q/%d, want %q/42", a.Event, a.VideoID, EventCompleted)
	}
	if !a.NextAttemptAt.Equal(now) {
		t.Errorf("next attempt = %v, want %v", a.NextAttemptAt, now)
	}
	if a.Test {
		t.Error("terminal delivery must not be flagged as a test send")
	}
}

// TestNewTerminalDeliveryInput_DistinctPerTuple proves the derived id is a
// function of (event, video): different events or different videos must not
// collide, or two distinct terminal webhooks would share an idempotency id and
// a receiver could discard one as a duplicate of the other.
func TestNewTerminalDeliveryInput_DistinctPerTuple(t *testing.T) {
	seen := map[string]string{} // messageID -> dedupeKey that produced it
	tuples := []struct {
		event   string
		videoID int64
	}{
		{EventCompleted, 1},
		{EventCompleted, 2},
		{EventFailed, 1},
		{EventFailed, 2},
	}
	for _, tup := range tuples {
		in := NewTerminalDeliveryInput(tup.event, tup.videoID, time.Time{})
		if prev, ok := seen[in.MessageID]; ok {
			t.Errorf("message id collision %q: produced by both %q and %q", in.MessageID, prev, in.DedupeKey)
		}
		seen[in.MessageID] = in.DedupeKey
	}
}

// TestNewTerminalDeliveryInput_ZeroTimeDefaultsToNow keeps the documented
// fallback: a zero NextAttemptAt becomes "now" so the row is immediately due.
func TestNewTerminalDeliveryInput_ZeroTimeDefaultsToNow(t *testing.T) {
	before := time.Now().UTC()
	in := NewTerminalDeliveryInput(EventFailed, 7, time.Time{})
	after := time.Now().UTC()
	if in.NextAttemptAt.Before(before) || in.NextAttemptAt.After(after) {
		t.Errorf("zero time not defaulted to now: got %v, want within [%v, %v]", in.NextAttemptAt, before, after)
	}
}
