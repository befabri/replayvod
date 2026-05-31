package sqliteadapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func (a *SQLiteAdapter) CreateRecordingWebhookDelivery(ctx context.Context, input *repository.RecordingWebhookDeliveryInput) (*repository.RecordingWebhookDelivery, error) {
	row, err := a.queries.CreateRecordingWebhookDelivery(ctx, sqliteCreateRecordingWebhookDeliveryParams(input))
	if err != nil {
		return nil, fmt.Errorf("sqlite create recording webhook delivery: %w", err)
	}
	return sqliteRecordingWebhookDeliveryToDomain(row), nil
}

func (a *SQLiteAdapter) CreateClaimedRecordingWebhookDelivery(ctx context.Context, input *repository.RecordingWebhookDeliveryInput) (*repository.RecordingWebhookDelivery, error) {
	p := sqliteCreateRecordingWebhookDeliveryParams(input)
	row, err := a.queries.CreateClaimedRecordingWebhookDelivery(ctx, sqlitegen.CreateClaimedRecordingWebhookDeliveryParams{
		MessageID:     p.MessageID,
		DedupeKey:     p.DedupeKey,
		Event:         p.Event,
		VideoID:       p.VideoID,
		Test:          p.Test,
		NextAttemptAt: p.NextAttemptAt,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite create claimed recording webhook delivery: %w", err)
	}
	return sqliteRecordingWebhookDeliveryToDomain(row), nil
}

func (a *SQLiteAdapter) DeleteOldRecordingWebhookDeliveries(ctx context.Context, before time.Time) error {
	if err := a.queries.DeleteOldRecordingWebhookDeliveries(ctx, formatTime(before)); err != nil {
		return fmt.Errorf("sqlite delete old recording webhook deliveries: %w", err)
	}
	return nil
}

func (a *SQLiteAdapter) ClaimDueRecordingWebhookDeliveries(ctx context.Context, now time.Time, limit int) ([]repository.RecordingWebhookDelivery, error) {
	if limit <= 0 {
		return nil, nil
	}
	out := make([]repository.RecordingWebhookDelivery, 0, limit)
	for len(out) < limit {
		row, err := a.queries.ClaimDueRecordingWebhookDelivery(ctx, sql.NullString{String: formatTime(now), Valid: true})
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("sqlite claim recording webhook delivery: %w", err)
		}
		out = append(out, *sqliteRecordingWebhookDeliveryToDomain(row))
	}
	return out, nil
}

func (a *SQLiteAdapter) MarkRecordingWebhookDeliveryDelivered(ctx context.Context, id int64, status int, now time.Time) error {
	if err := a.queries.MarkRecordingWebhookDeliveryDelivered(ctx, sqlitegen.MarkRecordingWebhookDeliveryDeliveredParams{
		ID:         id,
		LastStatus: int64(status),
		Now:        sql.NullString{String: formatTime(now), Valid: true},
	}); err != nil {
		return fmt.Errorf("sqlite mark recording webhook delivery delivered: %w", err)
	}
	return nil
}

func (a *SQLiteAdapter) MarkRecordingWebhookDeliveryFinal(ctx context.Context, id int64, status string, httpStatus int, errMsg string, nextAttemptAt time.Time, now time.Time) error {
	if err := a.queries.MarkRecordingWebhookDeliveryFinal(ctx, sqlitegen.MarkRecordingWebhookDeliveryFinalParams{
		ID:            id,
		Status:        status,
		LastStatus:    int64(httpStatus),
		LastError:     errMsg,
		NextAttemptAt: formatTime(nextAttemptAt),
		Now:           formatTime(now),
	}); err != nil {
		return fmt.Errorf("sqlite mark recording webhook delivery final: %w", err)
	}
	return nil
}

func (a *SQLiteAdapter) ResetStaleRecordingWebhookDeliveries(ctx context.Context, before time.Time, now time.Time) error {
	if err := a.queries.ResetStaleRecordingWebhookDeliveries(ctx, sqlitegen.ResetStaleRecordingWebhookDeliveriesParams{
		Now:    formatTime(now),
		Before: formatTime(before),
	}); err != nil {
		return fmt.Errorf("sqlite reset stale recording webhook deliveries: %w", err)
	}
	return nil
}

func (a *SQLiteAdapter) RetryRecordingWebhookDelivery(ctx context.Context, id int64, now time.Time) (*repository.RecordingWebhookDelivery, error) {
	row, err := a.queries.RetryRecordingWebhookDelivery(ctx, sqlitegen.RetryRecordingWebhookDeliveryParams{
		ID:  id,
		Now: formatTime(now),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteRecordingWebhookDeliveryToDomain(row), nil
}

func (a *SQLiteAdapter) ListRecordingWebhookDeliveries(ctx context.Context, limit int) ([]repository.RecordingWebhookDelivery, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := a.queries.ListRecordingWebhookDeliveries(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("sqlite list recording webhook deliveries: %w", err)
	}
	out := make([]repository.RecordingWebhookDelivery, len(rows))
	for i, row := range rows {
		out[i] = *sqliteRecordingWebhookDeliveryToDomain(row)
	}
	return out, nil
}

func sqliteCreateRecordingWebhookDeliveryParams(input *repository.RecordingWebhookDeliveryInput) sqlitegen.CreateRecordingWebhookDeliveryParams {
	next := input.NextAttemptAt
	if next.IsZero() {
		next = time.Now().UTC()
	}
	var test int64
	if input.Test {
		test = 1
	}
	return sqlitegen.CreateRecordingWebhookDeliveryParams{
		MessageID:     input.MessageID,
		DedupeKey:     input.DedupeKey,
		Event:         input.Event,
		VideoID:       input.VideoID,
		Test:          test,
		NextAttemptAt: formatTime(next),
	}
}

func sqliteCreateRecordingWebhookDeliveryIfEnabled(ctx context.Context, q *sqlitegen.Queries, input *repository.RecordingWebhookDeliveryInput) error {
	if input == nil {
		return nil
	}
	next := input.NextAttemptAt
	if next.IsZero() {
		next = time.Now().UTC()
	}
	_, err := q.CreateRecordingWebhookDeliveryIfEnabled(ctx, sqlitegen.CreateRecordingWebhookDeliveryIfEnabledParams{
		MessageID:     input.MessageID,
		DedupeKey:     input.DedupeKey,
		Event:         input.Event,
		VideoID:       input.VideoID,
		NextAttemptAt: formatTime(next),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("sqlite create recording webhook delivery if enabled: %w", err)
	}
	return nil
}

func sqliteRecordingWebhookDeliveryToDomain(d sqlitegen.RecordingWebhookDelivery) *repository.RecordingWebhookDelivery {
	return &repository.RecordingWebhookDelivery{
		ID:            d.ID,
		MessageID:     d.MessageID,
		DedupeKey:     d.DedupeKey,
		Event:         d.Event,
		VideoID:       d.VideoID,
		Status:        d.Status,
		Attempts:      int(d.Attempts),
		LastStatus:    int(d.LastStatus),
		LastError:     d.LastError,
		Test:          d.Test != 0,
		NextAttemptAt: parseTime(d.NextAttemptAt),
		LastAttemptAt: parseNullTime(d.LastAttemptAt),
		DeliveredAt:   parseNullTime(d.DeliveredAt),
		CreatedAt:     parseTime(d.CreatedAt),
		UpdatedAt:     parseTime(d.UpdatedAt),
	}
}
