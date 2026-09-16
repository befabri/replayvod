package contracttest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func subscriptionIDs(subs []repository.Subscription) map[string]bool {
	out := make(map[string]bool, len(subs))
	for _, s := range subs {
		out[s.ID] = true
	}
	return out
}

func seedSubscription(t *testing.T, ctx context.Context, repo repository.Repository, id, broadcasterID, subType string) {
	t.Helper()
	bid := broadcasterID
	if _, err := repo.CreateSubscription(ctx, &repository.SubscriptionInput{
		ID: id, Status: "enabled", Type: subType, Version: "1", Cost: 1,
		Condition: []byte(`{"broadcaster_user_id":"` + broadcasterID + `"}`), BroadcasterID: &bid,
		TransportMethod: "webhook", TransportCallback: "https://example/cb", TwitchCreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed subscription %s: %v", id, err)
	}
}

func testSubscriptionLookupsAndCounts(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "bc-1")
	if _, err := repo.UpsertChannel(ctx, &repository.Channel{BroadcasterID: "bc-2", BroadcasterLogin: "bc-2", BroadcasterName: "bc-2"}); err != nil {
		t.Fatal(err)
	}
	seedSubscription(t, ctx, repo, "sub-1", "bc-1", "stream.online")
	seedSubscription(t, ctx, repo, "sub-2", "bc-1", "stream.offline")
	seedSubscription(t, ctx, repo, "sub-3", "bc-2", "stream.online")
	if got, err := repo.GetActiveSubscriptionForBroadcasterType(ctx, "bc-1", "stream.online"); err != nil || got.ID != "sub-1" {
		t.Fatalf("active subscription = %+v, %v", got, err)
	}
	if _, err := repo.GetActiveSubscriptionForBroadcasterType(ctx, "bc-2", "stream.offline"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("absent subscription type: %v", err)
	}
	if n, err := repo.CountActiveSubscriptions(ctx); err != nil || n != 3 {
		t.Fatalf("active count = %d, %v", n, err)
	}
	if err := repo.UpdateSubscriptionStatus(ctx, "sub-1", "webhook_callback_verification_pending"); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.GetSubscription(ctx, "sub-1"); err != nil || got.Status != "webhook_callback_verification_pending" {
		t.Fatalf("status after update = %+v, %v", got, err)
	}
	online, err := repo.ListSubscriptionsByType(ctx, "stream.online")
	if err != nil || len(online) != 2 || !subscriptionIDs(online)["sub-1"] || !subscriptionIDs(online)["sub-3"] {
		t.Fatalf("online subscriptions = %+v, %v", online, err)
	}
	if err := repo.MarkSubscriptionRevoked(ctx, "sub-3", "user_removed"); err != nil {
		t.Fatal(err)
	}
	if online, err := repo.ListSubscriptionsByType(ctx, "stream.online"); err != nil || len(online) != 1 || online[0].ID != "sub-1" {
		t.Fatalf("online subscriptions after revoke = %+v, %v", online, err)
	}
	if byBroadcaster, err := repo.ListSubscriptionsByBroadcaster(ctx, "bc-2"); err != nil || len(byBroadcaster) != 1 || byBroadcaster[0].ID != "sub-3" || byBroadcaster[0].RevokedAt == nil {
		t.Fatalf("revoked row must stay listed for its broadcaster: %+v, %v", byBroadcaster, err)
	}
	if _, err := repo.GetActiveSubscriptionForBroadcasterType(ctx, "bc-2", "stream.online"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("revoked subscription still active: %v", err)
	}
	if n, err := repo.CountActiveSubscriptions(ctx); err != nil || n != 2 {
		t.Fatalf("active count after revoke = %d, %v", n, err)
	}
	if err := repo.DeleteSubscription(ctx, "sub-2"); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteSubscription(ctx, "sub-2"); err != nil {
		t.Fatalf("deleting a missing subscription: %v", err)
	}
	if _, err := repo.GetSubscription(ctx, "sub-2"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("deleted subscription survived: %v", err)
	}
	if n, err := repo.CountActiveSubscriptions(ctx); err != nil || n != 1 {
		t.Fatalf("active count after delete = %d, %v", n, err)
	}
	if byBroadcaster, err := repo.ListSubscriptionsByBroadcaster(ctx, "bc-1"); err != nil || len(byBroadcaster) != 1 || byBroadcaster[0].ID != "sub-1" {
		t.Fatalf("bc-1 subscriptions = %+v, %v", byBroadcaster, err)
	}
}

func testEventSubSnapshots(t *testing.T, h Harness) {
	ctx, repo := t.Context(), h.Repo()
	SeedUserChannel(t, ctx, repo, "owner", "bc-1")
	seedSubscription(t, ctx, repo, "sub-1", "bc-1", "stream.online")
	first, err := repo.CreateEventSubSnapshot(ctx, 10, 5, 100)
	if err != nil || first.ID == 0 || first.Total != 10 || first.TotalCost != 5 || first.MaxTotalCost != 100 || first.FetchedAt.IsZero() {
		t.Fatalf("snapshot = %+v, %v", first, err)
	}
	second, err := repo.CreateEventSubSnapshot(ctx, 12, 6, 100)
	if err != nil {
		t.Fatal(err)
	}
	// Both rows are stamped by the database within the same second, so the
	// latest is whichever the backend orders first among the tied stamps.
	latest, err := repo.GetLatestEventSubSnapshot(ctx)
	if err != nil || (latest.ID != first.ID && latest.ID != second.ID) || latest.FetchedAt.Before(first.FetchedAt) {
		t.Fatalf("latest snapshot = %+v, %v", latest, err)
	}
	if all, err := repo.ListEventSubSnapshots(ctx, 10, 0); err != nil || len(all) != 2 {
		t.Fatalf("snapshots = %+v, %v", all, err)
	}
	if page, err := repo.ListEventSubSnapshots(ctx, 1, 1); err != nil || len(page) != 1 {
		t.Fatalf("second snapshot page = %+v, %v", page, err)
	}
	for range 2 {
		if err := repo.LinkSnapshotSubscription(ctx, first.ID, "sub-1", 1, "enabled"); err != nil {
			t.Fatalf("link snapshot subscription: %v", err)
		}
	}
	if err := repo.DeleteOldEventSubSnapshots(ctx, time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if all, err := repo.ListEventSubSnapshots(ctx, 10, 0); err != nil || len(all) != 2 {
		t.Fatalf("recent snapshots pruned: %+v, %v", all, err)
	}
	if err := repo.DeleteOldEventSubSnapshots(ctx, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if all, err := repo.ListEventSubSnapshots(ctx, 10, 0); err != nil || len(all) != 0 {
		t.Fatalf("old snapshots survived: %+v, %v", all, err)
	}
	if _, err := repo.GetLatestEventSubSnapshot(ctx); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("latest of no snapshots: %v", err)
	}
}
