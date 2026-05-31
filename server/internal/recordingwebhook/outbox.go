package recordingwebhook

import (
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// NewTerminalDeliveryInput builds the durable outbox create payload for one
// terminal recording event. Both identifiers are derived from the (event, video)
// tuple: the dedupe key is stable so a retried terminal path reuses the original
// row instead of enqueueing a duplicate webhook, and the message id is derived
// the same way (see terminalMessageID) so minting can never fail. That makes the
// terminal transition and its durable webhook row all-or-nothing — there is no
// path where the video finalizes but the webhook is silently lost.
func NewTerminalDeliveryInput(event string, videoID int64, now time.Time) *repository.RecordingWebhookDeliveryInput {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return &repository.RecordingWebhookDeliveryInput{
		MessageID:     terminalMessageID(event, videoID),
		DedupeKey:     fmt.Sprintf("%s:%d", event, videoID),
		Event:         event,
		VideoID:       videoID,
		NextAttemptAt: now,
	}
}

func newTestDeliveryInput(now time.Time) (*repository.RecordingWebhookDeliveryInput, error) {
	id, err := newMessageID()
	if err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return &repository.RecordingWebhookDeliveryInput{
		MessageID:     id,
		DedupeKey:     "test:" + id,
		Event:         EventTest,
		Test:          true,
		NextAttemptAt: now,
	}, nil
}
