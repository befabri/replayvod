package pgadapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func (a *PGAdapter) CreateRecordingWebhookDelivery(ctx context.Context, input *repository.RecordingWebhookDeliveryInput) (*repository.RecordingWebhookDelivery, error) {
	row, err := a.queries.CreateRecordingWebhookDelivery(ctx, pgCreateRecordingWebhookDeliveryParams(input))
	if err != nil {
		return nil, fmt.Errorf("pg create recording webhook delivery: %w", err)
	}
	return pgRecordingWebhookDeliveryToDomain(row), nil
}

func (a *PGAdapter) CreateClaimedRecordingWebhookDelivery(ctx context.Context, input *repository.RecordingWebhookDeliveryInput) (*repository.RecordingWebhookDelivery, error) {
	p := pgCreateRecordingWebhookDeliveryParams(input)
	row, err := a.queries.CreateClaimedRecordingWebhookDelivery(ctx, pggen.CreateClaimedRecordingWebhookDeliveryParams{
		MessageID:     p.MessageID,
		DedupeKey:     p.DedupeKey,
		Event:         p.Event,
		VideoID:       p.VideoID,
		Test:          p.Test,
		NextAttemptAt: p.NextAttemptAt,
	})
	if err != nil {
		return nil, fmt.Errorf("pg create claimed recording webhook delivery: %w", err)
	}
	return pgRecordingWebhookDeliveryToDomain(row), nil
}

func (a *PGAdapter) DeleteOldRecordingWebhookDeliveries(ctx context.Context, before time.Time) error {
	if err := a.queries.DeleteOldRecordingWebhookDeliveries(ctx, before); err != nil {
		return fmt.Errorf("pg delete old recording webhook deliveries: %w", err)
	}
	return nil
}

func (a *PGAdapter) ClaimDueRecordingWebhookDeliveries(ctx context.Context, now time.Time, limit int) ([]repository.RecordingWebhookDelivery, error) {
	if limit <= 0 {
		return nil, nil
	}
	out := make([]repository.RecordingWebhookDelivery, 0, limit)
	for len(out) < limit {
		row, err := a.queries.ClaimDueRecordingWebhookDelivery(ctx, now)
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("pg claim recording webhook delivery: %w", err)
		}
		out = append(out, *pgRecordingWebhookDeliveryToDomain(row))
	}
	return out, nil
}

func (a *PGAdapter) MarkRecordingWebhookDeliveryDelivered(ctx context.Context, id int64, status int, now time.Time) error {
	if err := a.queries.MarkRecordingWebhookDeliveryDelivered(ctx, pggen.MarkRecordingWebhookDeliveryDeliveredParams{
		ID:         id,
		LastStatus: int32(status),
		Now:        now,
	}); err != nil {
		return fmt.Errorf("pg mark recording webhook delivery delivered: %w", err)
	}
	return nil
}

func (a *PGAdapter) MarkRecordingWebhookDeliveryFinal(ctx context.Context, id int64, status string, httpStatus int, errMsg string, nextAttemptAt time.Time, now time.Time) error {
	if err := a.queries.MarkRecordingWebhookDeliveryFinal(ctx, pggen.MarkRecordingWebhookDeliveryFinalParams{
		ID:            id,
		Status:        status,
		LastStatus:    int32(httpStatus),
		LastError:     errMsg,
		NextAttemptAt: nextAttemptAt,
		Now:           now,
	}); err != nil {
		return fmt.Errorf("pg mark recording webhook delivery final: %w", err)
	}
	return nil
}

func (a *PGAdapter) ResetStaleRecordingWebhookDeliveries(ctx context.Context, before time.Time, now time.Time) error {
	if err := a.queries.ResetStaleRecordingWebhookDeliveries(ctx, pggen.ResetStaleRecordingWebhookDeliveriesParams{
		Now:    now,
		Before: before,
	}); err != nil {
		return fmt.Errorf("pg reset stale recording webhook deliveries: %w", err)
	}
	return nil
}

func (a *PGAdapter) RetryRecordingWebhookDelivery(ctx context.Context, id int64, now time.Time) (*repository.RecordingWebhookDelivery, error) {
	row, err := a.queries.RetryRecordingWebhookDelivery(ctx, pggen.RetryRecordingWebhookDeliveryParams{
		ID:  id,
		Now: now,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return pgRecordingWebhookDeliveryToDomain(row), nil
}

func (a *PGAdapter) ListRecordingWebhookDeliveries(ctx context.Context, limit int) ([]repository.RecordingWebhookDelivery, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := a.queries.ListRecordingWebhookDeliveries(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("pg list recording webhook deliveries: %w", err)
	}
	out := make([]repository.RecordingWebhookDelivery, len(rows))
	for i, row := range rows {
		out[i] = *pgRecordingWebhookDeliveryToDomain(row)
	}
	return out, nil
}

func pgCreateRecordingWebhookDeliveryParams(input *repository.RecordingWebhookDeliveryInput) pggen.CreateRecordingWebhookDeliveryParams {
	next := input.NextAttemptAt
	if next.IsZero() {
		next = time.Now().UTC()
	}
	return pggen.CreateRecordingWebhookDeliveryParams{
		MessageID:     input.MessageID,
		DedupeKey:     input.DedupeKey,
		Event:         input.Event,
		VideoID:       input.VideoID,
		Test:          input.Test,
		NextAttemptAt: next,
	}
}

func pgCreateRecordingWebhookDeliveryIfEnabled(ctx context.Context, q *pggen.Queries, input *repository.RecordingWebhookDeliveryInput) error {
	if input == nil {
		return nil
	}
	next := input.NextAttemptAt
	if next.IsZero() {
		next = time.Now().UTC()
	}
	_, err := q.CreateRecordingWebhookDeliveryIfEnabled(ctx, pggen.CreateRecordingWebhookDeliveryIfEnabledParams{
		MessageID:     input.MessageID,
		DedupeKey:     input.DedupeKey,
		Event:         input.Event,
		VideoID:       input.VideoID,
		NextAttemptAt: next,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("pg create recording webhook delivery if enabled: %w", err)
	}
	return nil
}

func pgRecordingWebhookDeliveryToDomain(d pggen.RecordingWebhookDelivery) *repository.RecordingWebhookDelivery {
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
		Test:          d.Test,
		NextAttemptAt: d.NextAttemptAt,
		LastAttemptAt: d.LastAttemptAt,
		DeliveredAt:   d.DeliveredAt,
		CreatedAt:     d.CreatedAt,
		UpdatedAt:     d.UpdatedAt,
	}
}
