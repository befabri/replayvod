package recordingwebhook

import (
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

func TestDeliveryRecordFromRow_mapsDurableStatus(t *testing.T) {
	now := time.Now().UTC()
	row := repository.RecordingWebhookDelivery{
		ID:         7,
		MessageID:  "msg",
		Event:      EventCompleted,
		VideoID:    42,
		Status:     repository.RecordingWebhookDeliveryDelivered,
		Attempts:   2,
		LastStatus: 200,
		Test:       false,
		UpdatedAt:  now,
	}
	rec := deliveryRecordFromRow(row)
	if rec.ID != 7 || rec.MessageID != "msg" || rec.Outcome != OutcomeDelivered || rec.Status != 200 || rec.Attempts != 2 {
		t.Fatalf("record = %+v", rec)
	}
	if !rec.Time.Equal(now) {
		t.Fatalf("time = %s, want %s", rec.Time, now)
	}
}

func TestOutcomeForStatus_pendingAndDelivering(t *testing.T) {
	if outcomeForStatus(repository.RecordingWebhookDeliveryPending) != OutcomePending {
		t.Fatal("pending status should surface as pending")
	}
	if outcomeForStatus(repository.RecordingWebhookDeliveryDelivering) != OutcomeDelivering {
		t.Fatal("delivering status should surface as delivering")
	}
}
