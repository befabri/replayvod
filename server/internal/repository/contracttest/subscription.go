package contracttest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// testSubscriptionRevokeKeepsRowForAudit pins the soft-delete contract. When
// Twitch revokes a subscription we MUST NOT hard-delete: the audit log needs
// the original row to answer "when did we lose delivery for broadcaster X?" and
// to dedup retries of old events that reference a now-revoked subscription_id.
// ListActiveSubscriptions must filter revoked rows so the dashboard "active"
// view stays truthful.
func testSubscriptionRevokeKeepsRowForAudit(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u-rev", "b-rev")

	bid := "b-rev"
	sub, err := repo.CreateSubscription(ctx, &repository.SubscriptionInput{
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

	if err := repo.MarkSubscriptionRevoked(ctx, sub.ID, "authorization_revoked"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	got, err := repo.GetSubscription(ctx, sub.ID)
	if err != nil {
		t.Fatalf("GetSubscription after revoke must still return the row, got %v", err)
	}
	if got.RevokedAt == nil {
		t.Error("RevokedAt must be set on revoked row")
	}
	if got.RevokedReason == nil || *got.RevokedReason != "authorization_revoked" {
		t.Errorf("RevokedReason = %v, want authorization_revoked", got.RevokedReason)
	}

	active, err := repo.ListActiveSubscriptions(ctx, 100, 0)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	for _, s := range active {
		if s.ID == sub.ID {
			t.Errorf("revoked subscription %q must NOT appear in ListActiveSubscriptions", sub.ID)
		}
	}
}

// testSubscriptionListActiveStableWithTiedCreatedAt forces every row to share a
// created_at so the secondary sort key has to break the tie; paging must then
// return a stable, non-overlapping ordering.
func testSubscriptionListActiveStableWithTiedCreatedAt(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	for i := 1; i <= 3; i++ {
		broadcasterID := fmt.Sprintf("b-order-%03d", i)
		SeedUserChannel(t, ctx, repo, broadcasterID, broadcasterID)

		_, err := repo.CreateSubscription(ctx, &repository.SubscriptionInput{
			ID:                fmt.Sprintf("sub-order-%03d", i),
			Status:            "enabled",
			Type:              "stream.online",
			Version:           "1",
			Cost:              1,
			Condition:         []byte(`{"broadcaster_user_id":"` + broadcasterID + `"}`),
			BroadcasterID:     &broadcasterID,
			TransportMethod:   "webhook",
			TransportCallback: "https://example/cb",
			TwitchCreatedAt:   time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("create subscription %d: %v", i, err)
		}
	}

	h.BackdateAllSubscriptionsCreated(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	page1, err := repo.ListActiveSubscriptions(ctx, 2, 0)
	if err != nil {
		t.Fatalf("list active page 1: %v", err)
	}
	page2, err := repo.ListActiveSubscriptions(ctx, 2, 2)
	if err != nil {
		t.Fatalf("list active page 2: %v", err)
	}

	got := make([]string, 0, len(page1)+len(page2))
	for _, sub := range append(page1, page2...) {
		got = append(got, sub.ID)
	}
	want := []string{"sub-order-003", "sub-order-002", "sub-order-001"}
	if len(got) != len(want) {
		t.Fatalf("paged IDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paged IDs = %v, want %v", got, want)
		}
	}
}

// testSubscriptionActiveUniquePerBroadcasterType guards the partial-unique
// index that mirrors Twitch's own constraint: at most one ACTIVE sub per
// (broadcaster_id, type). The partial WHERE clause allows re-creation after a
// revoke, which this test also covers.
func testSubscriptionActiveUniquePerBroadcasterType(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()
	SeedUserChannel(t, ctx, repo, "u-uniq", "b-uniq")

	bid := "b-uniq"
	create := func(id string) error {
		_, err := repo.CreateSubscription(ctx, &repository.SubscriptionInput{
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

	// The second active sub for the same (broadcaster, type) must fail. The
	// driver-specific error text differs by backend (Postgres "duplicate"/
	// "unique", SQLite "unique"/"constraint"), so the substring match is only
	// advisory; failing at all is the real contract.
	if err := create("sub-dup"); err == nil {
		t.Fatal("second active sub for same (broadcaster, type) must fail unique index")
	} else {
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "duplicate") && !strings.Contains(msg, "unique") && !strings.Contains(msg, "constraint") {
			t.Logf("unique-violation error text differs (still failing is correct): %v", err)
		}
	}

	if err := repo.MarkSubscriptionRevoked(ctx, "sub-first", "manual"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if err := create("sub-replacement"); err != nil {
		t.Errorf("after revoke the partial-unique must allow a new active sub: %v", err)
	}
}

// testWebhookEventDedupOnConflict pins the retry-safety contract. Twitch
// retries delivery with the same Message-Id on failure; the ON CONFLICT DO
// NOTHING path must surface a distinguishable signal (ErrNotFound) so the
// handler returns 204 without re-invoking the event processor.
func testWebhookEventDedupOnConflict(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	input := &repository.WebhookEventInput{
		EventID:          "dup-msg-1",
		MessageType:      repository.WebhookMessageNotification,
		MessageTimestamp: time.Now().UTC(),
		Payload:          json.RawMessage(`{"hello":"world"}`),
	}

	if _, err := repo.CreateWebhookEvent(ctx, input); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	_, err := repo.CreateWebhookEvent(ctx, input)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("duplicate CreateWebhookEvent must return ErrNotFound (dedup signal), got %v", err)
	}

	count, err := repo.CountWebhookEvents(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("rows after dup insert = %d, want 1", count)
	}
}

// testWebhookEventPayloadRoundTrip verifies the JSONB (PG) / TEXT (SQLite)
// storage path preserves the payload through json.RawMessage. Compared
// semantically because PG may re-whitespace JSONB output.
func testWebhookEventPayloadRoundTrip(t *testing.T, h Harness) {
	ctx := context.Background()
	repo := h.Repo()

	payload := json.RawMessage(`{"event":"stream.online","broadcaster_user_id":"12345","nested":{"array":[1,2,3]}}`)
	created, err := repo.CreateWebhookEvent(ctx, &repository.WebhookEventInput{
		EventID:          "payload-msg-1",
		MessageType:      repository.WebhookMessageNotification,
		MessageTimestamp: time.Now().UTC(),
		Payload:          payload,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	reloaded, err := repo.GetWebhookEvent(ctx, created.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}

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
