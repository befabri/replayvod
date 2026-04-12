package sqliteadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) CreateEventSubSnapshot(ctx context.Context, total, totalCost, maxTotalCost int64) (*repository.EventSubSnapshot, error) {
	row, err := a.queries.CreateSnapshot(ctx, sqlitegen.CreateSnapshotParams{
		Total:        total,
		TotalCost:    totalCost,
		MaxTotalCost: maxTotalCost,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite create snapshot: %w", err)
	}
	return sqliteSnapshotToDomain(row), nil
}

func (a *SQLiteAdapter) GetLatestEventSubSnapshot(ctx context.Context) (*repository.EventSubSnapshot, error) {
	row, err := a.queries.GetLatestSnapshot(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteSnapshotToDomain(row), nil
}

func (a *SQLiteAdapter) ListEventSubSnapshots(ctx context.Context, limit, offset int) ([]repository.EventSubSnapshot, error) {
	rows, err := a.queries.ListSnapshots(ctx, sqlitegen.ListSnapshotsParams{
		Limit:  int64(limit),
		Offset: int64(offset),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite list snapshots: %w", err)
	}
	out := make([]repository.EventSubSnapshot, len(rows))
	for i, r := range rows {
		out[i] = *sqliteSnapshotToDomain(r)
	}
	return out, nil
}

func (a *SQLiteAdapter) DeleteOldEventSubSnapshots(ctx context.Context, before time.Time) error {
	return a.queries.DeleteOldSnapshots(ctx, formatTime(before))
}

func (a *SQLiteAdapter) LinkSnapshotSubscription(ctx context.Context, snapshotID int64, subscriptionID string, costAtSnapshot int64, statusAtSnapshot string) error {
	return a.queries.LinkSnapshotSubscription(ctx, sqlitegen.LinkSnapshotSubscriptionParams{
		SnapshotID:       snapshotID,
		SubscriptionID:   subscriptionID,
		CostAtSnapshot:   costAtSnapshot,
		StatusAtSnapshot: statusAtSnapshot,
	})
}

func sqliteSnapshotToDomain(s sqlitegen.EventsubSnapshot) *repository.EventSubSnapshot {
	return &repository.EventSubSnapshot{
		ID:           s.ID,
		Total:        s.Total,
		TotalCost:    s.TotalCost,
		MaxTotalCost: s.MaxTotalCost,
		FetchedAt:    parseTime(s.FetchedAt),
	}
}
