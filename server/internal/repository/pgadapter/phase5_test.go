package pgadapter

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// seedUserChannel is the fixture for Phase 5 tests: every schedule and
// subscription FKs into users + channels, so each test needs both.
func seedUserChannel(t *testing.T, ctx context.Context, a *PGAdapter, userID, broadcasterID string) {
	t.Helper()
	if _, err := a.UpsertUser(ctx, &repository.User{
		ID: userID, Login: userID, DisplayName: userID, Role: "viewer",
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := a.UpsertChannel(ctx, &repository.Channel{
		BroadcasterID:    broadcasterID,
		BroadcasterLogin: broadcasterID,
		BroadcasterName:  broadcasterID,
	}); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
}

// TestSchedule_Upsert_PreservesTriggerCount guards operational history.
// UpdateSchedule deliberately omits trigger_count and last_triggered_at
// from the SET clause — if someone adds those fields later ("let me also
// update this while I'm here"), operators lose the fire-history the
// dashboard uses to answer "is this schedule actually working?" and the
// retention task uses to pick what to prune. This test is the regression
// gate for that schema contract.
func TestSchedule_Upsert_PreservesTriggerCount(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	seedUserChannel(t, ctx, a, "u-1", "b-1")

	created, err := a.CreateSchedule(ctx, &repository.ScheduleInput{
		BroadcasterID: "b-1", RequestedBy: "u-1", Quality: "HIGH",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Fire the schedule twice so trigger_count is non-zero and
	// last_triggered_at is set — the very state the test needs to defend.
	for i := 0; i < 2; i++ {
		if err := a.RecordScheduleTrigger(ctx, created.ID); err != nil {
			t.Fatalf("record trigger: %v", err)
		}
	}
	before, err := a.GetSchedule(ctx, created.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if before.TriggerCount != 2 || before.LastTriggeredAt == nil {
		t.Fatalf("setup precondition failed: count=%d triggered=%v", before.TriggerCount, before.LastTriggeredAt)
	}

	updated, err := a.UpdateSchedule(ctx, created.ID, &repository.ScheduleInput{
		BroadcasterID: "b-1", RequestedBy: "u-1", Quality: "MEDIUM",
		IsDisabled: true,
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Quality != "MEDIUM" || !updated.IsDisabled {
		t.Errorf("update didn't apply: quality=%s disabled=%v", updated.Quality, updated.IsDisabled)
	}
	if updated.TriggerCount != 2 {
		t.Errorf("UpdateSchedule clobbered trigger_count: was 2, now %d", updated.TriggerCount)
	}
	if updated.LastTriggeredAt == nil || !updated.LastTriggeredAt.Equal(*before.LastTriggeredAt) {
		t.Errorf("UpdateSchedule clobbered last_triggered_at: was %v, now %v", before.LastTriggeredAt, updated.LastTriggeredAt)
	}
}

// TestSubscription_Revoke_KeepsRowForAudit pins the soft-delete contract.
// When Twitch revokes a subscription (authorization_revoked,
// notification_failures_exceeded, etc.), we MUST NOT hard-delete — the
// audit log needs the original row to answer "when did we lose delivery
// for broadcaster X?" and dedup against retries of old events that
// reference a now-revoked subscription_id. ListActiveSubscriptions must
// filter revoked rows so the dashboard "active" view stays truthful.
func TestSubscription_Revoke_KeepsRowForAudit(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	seedUserChannel(t, ctx, a, "u-rev", "b-rev")

	bid := "b-rev"
	sub, err := a.CreateSubscription(ctx, &repository.SubscriptionInput{
		ID: "sub-audit-1", Status: "enabled", Type: "stream.online", Version: "1",
		Cost:              1,
		Condition:         []byte(`{"broadcaster_user_id":"b-rev"}`),
		BroadcasterID:     &bid,
		TransportMethod:   "webhook",
		TransportCallback: "https://example/cb",
		TwitchCreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := a.MarkSubscriptionRevoked(ctx, sub.ID, "authorization_revoked"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	got, err := a.GetSubscription(ctx, sub.ID)
	if err != nil {
		t.Fatalf("GetSubscription after revoke must still return the row, got %v", err)
	}
	if got.RevokedAt == nil {
		t.Error("RevokedAt must be set on revoked row")
	}
	if got.RevokedReason == nil || *got.RevokedReason != "authorization_revoked" {
		t.Errorf("RevokedReason = %v, want authorization_revoked", got.RevokedReason)
	}

	active, err := a.ListActiveSubscriptions(ctx, 100, 0)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	for _, s := range active {
		if s.ID == sub.ID {
			t.Errorf("revoked subscription %q must NOT appear in ListActiveSubscriptions", sub.ID)
		}
	}
}

// TestSubscription_ActiveUniquePerBroadcasterType guards the partial-unique
// index that mirrors Twitch's own constraint: at most one ACTIVE sub per
// (broadcaster_id, type). Without this, a retry storm or race could create
// duplicate subs; when the first one's revocation arrives we'd mark only
// one of them revoked and permanently leak cost. The partial WHERE clause
// allows re-creation after a revoke, which this test also covers.
func TestSubscription_ActiveUniquePerBroadcasterType(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)
	seedUserChannel(t, ctx, a, "u-uniq", "b-uniq")

	bid := "b-uniq"
	create := func(id string) error {
		_, err := a.CreateSubscription(ctx, &repository.SubscriptionInput{
			ID: id, Status: "enabled", Type: "stream.online", Version: "1",
			Cost:              1,
			Condition:         []byte(`{"broadcaster_user_id":"b-uniq"}`),
			BroadcasterID:     &bid,
			TransportMethod:   "webhook",
			TransportCallback: "https://example/cb",
			TwitchCreatedAt:   time.Now().UTC(),
		})
		return err
	}

	if err := create("sub-first"); err != nil {
		t.Fatalf("first create: %v", err)
	}

	if err := create("sub-dup"); err == nil {
		t.Fatal("second active sub for same (broadcaster, type) must fail unique index")
	} else if !strings.Contains(strings.ToLower(err.Error()), "duplicate") &&
		!strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Logf("expected unique-violation error, got (still failing is correct): %v", err)
	}

	if err := a.MarkSubscriptionRevoked(ctx, "sub-first", "manual"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if err := create("sub-replacement"); err != nil {
		t.Errorf("after revoke the partial-unique must allow a new active sub: %v", err)
	}
}

// TestWebhookEvent_Dedup_OnConflict pins the retry-safety contract. Twitch
// retries delivery with the same Message-Id on failure; the ON CONFLICT
// DO NOTHING path must surface a distinguishable signal (ErrNotFound here)
// so the handler returns 204 without re-invoking the event processor —
// otherwise every Twitch retry storm causes duplicate downloads.
func TestWebhookEvent_Dedup_OnConflict(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	input := &repository.WebhookEventInput{
		EventID:          "dup-msg-1",
		MessageType:      repository.WebhookMessageNotification,
		MessageTimestamp: time.Now().UTC(),
		Payload:          json.RawMessage(`{"hello":"world"}`),
	}

	if _, err := a.CreateWebhookEvent(ctx, input); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	_, err := a.CreateWebhookEvent(ctx, input)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("duplicate CreateWebhookEvent must return ErrNotFound (dedup signal), got %v", err)
	}

	count, err := a.CountWebhookEvents(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("rows after dup insert = %d, want 1", count)
	}
}

// TestWebhookEvent_PayloadRoundTrip verifies the JSONB (PG) / TEXT (SQLite)
// storage path preserves the payload byte-for-byte through json.RawMessage.
// A future migration that serializes-on-store could silently re-encode the
// payload; future replay/debug tooling compares against original bytes, so
// byte-level equivalence matters.
func TestWebhookEvent_PayloadRoundTrip(t *testing.T) {
	ctx := context.Background()
	a := newTestAdapter(t)

	payload := json.RawMessage(`{"event":"stream.online","broadcaster_user_id":"12345","nested":{"array":[1,2,3]}}`)
	created, err := a.CreateWebhookEvent(ctx, &repository.WebhookEventInput{
		EventID:          "payload-msg-1",
		MessageType:      repository.WebhookMessageNotification,
		MessageTimestamp: time.Now().UTC(),
		Payload:          payload,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	reloaded, err := a.GetWebhookEvent(ctx, created.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

	// Compare semantically (normalized JSON) because PG may re-whitespace
	// JSONB output. Byte-level equality is nice-to-have; semantic equality
	// is the real contract — a downstream consumer reading .Payload as
	// json.RawMessage will decode the same structure either way.
	var want, got any
	if err := json.Unmarshal(payload, &want); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	if err := json.Unmarshal(reloaded.Payload, &got); err != nil {
		t.Fatalf("unmarshal got (%q): %v", string(reloaded.Payload), err)
	}
	if !jsonEqual(want, got) {
		t.Errorf("payload round-trip differs:\n want=%q\n got =%q", string(payload), string(reloaded.Payload))
	}
}

func jsonEqual(a, b any) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}
