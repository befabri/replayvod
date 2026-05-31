package recordingwebhook

import (
	"errors"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// ErrDeliveryNotRetryable is returned by Dispatcher.RetryDelivery when the id
// does not name a delivery in a retryable (failed/rejected) state — either it
// is missing or it is pending/delivering/delivered. The API maps it to a 404 so
// a caller cannot reset a delivered or in-flight row into a duplicate send.
var ErrDeliveryNotRetryable = errors.New("recording webhook delivery: not found or not in a retryable state")

// DeliveryOutcome is the dashboard-facing delivery state. Delivered, rejected,
// and failed are terminal; pending/delivering are durable outbox states.
type DeliveryOutcome string

const (
	OutcomePending    DeliveryOutcome = "pending"
	OutcomeDelivering DeliveryOutcome = "delivering"
	OutcomeDelivered  DeliveryOutcome = "delivered"
	OutcomeRejected   DeliveryOutcome = "rejected"
	OutcomeFailed     DeliveryOutcome = "failed"
)

// DeliveryRecord is one durable delivery row surfaced to the owner dashboard.
// Metadata only: no payload body, no signing secret, and no raw target URL.
type DeliveryRecord struct {
	ID        int64
	Time      time.Time
	Event     string
	VideoID   int64
	Outcome   DeliveryOutcome
	Status    int
	Attempts  int
	Error     string
	Test      bool
	MessageID string
}

func deliveryRecordFromRow(row repository.RecordingWebhookDelivery) DeliveryRecord {
	return DeliveryRecord{
		ID:        row.ID,
		Time:      row.UpdatedAt,
		Event:     row.Event,
		VideoID:   row.VideoID,
		Outcome:   outcomeForStatus(row.Status),
		Status:    row.LastStatus,
		Attempts:  row.Attempts,
		Error:     row.LastError,
		Test:      row.Test,
		MessageID: row.MessageID,
	}
}

func outcomeForStatus(status string) DeliveryOutcome {
	switch status {
	case repository.RecordingWebhookDeliveryPending:
		return OutcomePending
	case repository.RecordingWebhookDeliveryDelivering:
		return OutcomeDelivering
	case repository.RecordingWebhookDeliveryDelivered:
		return OutcomeDelivered
	case repository.RecordingWebhookDeliveryRejected:
		return OutcomeRejected
	case repository.RecordingWebhookDeliveryFailed:
		return OutcomeFailed
	default:
		return OutcomeFailed
	}
}
