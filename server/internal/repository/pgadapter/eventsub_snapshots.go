package pgadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) CreateEventSubSnapshot(ctx context.Context, total, totalCost, maxTotalCost int64) (*repository.EventSubSnapshot, error) {
	row, err := a.queries.CreateSnapshot(ctx, pggen.CreateSnapshotParams{
		Total:        int32(total),
		TotalCost:    int32(totalCost),
		MaxTotalCost: int32(maxTotalCost),
	})
	if err != nil {
		return nil, fmt.Errorf("pg create snapshot: %w", err)
	}
	return pgSnapshotToDomain(row), nil
}

func (a *PGAdapter) GetLatestEventSubSnapshot(ctx context.Context) (*repository.EventSubSnapshot, error) {
	row, err := a.queries.GetLatestSnapshot(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgSnapshotToDomain(row), nil
}

func (a *PGAdapter) ListEventSubSnapshots(ctx context.Context, limit, offset int) ([]repository.EventSubSnapshot, error) {
	rows, err := a.queries.ListSnapshots(ctx, pggen.ListSnapshotsParams{
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("pg list snapshots: %w", err)
	}
	out := make([]repository.EventSubSnapshot, len(rows))
	for i, r := range rows {
		out[i] = *pgSnapshotToDomain(r)
	}
	return out, nil
}

func (a *PGAdapter) DeleteOldEventSubSnapshots(ctx context.Context, before time.Time) error {
	return a.queries.DeleteOldSnapshots(ctx, before)
}

func (a *PGAdapter) LinkSnapshotSubscription(ctx context.Context, snapshotID int64, subscriptionID string, costAtSnapshot int64, statusAtSnapshot string) error {
	return a.queries.LinkSnapshotSubscription(ctx, pggen.LinkSnapshotSubscriptionParams{
		SnapshotID:       snapshotID,
		SubscriptionID:   subscriptionID,
		CostAtSnapshot:   int32(costAtSnapshot),
		StatusAtSnapshot: statusAtSnapshot,
	})
}

func pgSnapshotToDomain(s pggen.EventsubSnapshot) *repository.EventSubSnapshot {
	return &repository.EventSubSnapshot{
		ID:           s.ID,
		Total:        int64(s.Total),
		TotalCost:    int64(s.TotalCost),
		MaxTotalCost: int64(s.MaxTotalCost),
		FetchedAt:    s.FetchedAt,
	}
}
