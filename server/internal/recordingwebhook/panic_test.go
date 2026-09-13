package recordingwebhook

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

type panicDeliveryTransport struct{}

type deliveryRoundTripFunc func(*http.Request) (*http.Response, error)

func (f deliveryRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func (panicDeliveryTransport) RoundTrip(*http.Request) (*http.Response, error) {
	panic("delivery transport panic")
}

type contextCheckingDeliveryStore struct {
	deliveryStore
	settled chan error
}

func (s contextCheckingDeliveryStore) MarkRecordingWebhookDeliveryFinal(ctx context.Context, id int64, status string, httpStatus int, errMsg string, nextAttemptAt, now time.Time) error {
	if err := ctx.Err(); err != nil {
		s.settled <- err
		return err
	}
	if _, ok := ctx.Deadline(); !ok {
		err := errors.New("delivery settlement has no deadline")
		s.settled <- err
		return err
	}
	err := s.deliveryStore.MarkRecordingWebhookDeliveryFinal(ctx, id, status, httpStatus, errMsg, nextAttemptAt, now)
	s.settled <- err
	return err
}

func TestDispatcherCancellationPersistsRetryBeforeReleasingOwnership(t *testing.T) {
	store := completedStore()
	store.settings = &repository.ServerSettings{RecordingWebhookEnabled: true, RecordingWebhookURL: "https://receiver.example/hook", RecordingWebhookSecret: "secret"}
	d := newTestDispatcher(store)
	settled := make(chan error, 1)
	d.store = contextCheckingDeliveryStore{deliveryStore: store, settled: settled}
	started := make(chan struct{})
	d.client = &http.Client{Transport: deliveryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	row := enqueueTerminal(t, store, EventCompleted, store.video.ID, time.Now().Add(-time.Minute))
	d.drainDue(t.Context())
	<-started
	d.work.Stop()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := d.work.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-settled; err != nil {
		t.Fatalf("cancelled delivery did not settle: %v", err)
	}
	got := deliverySnapshot(t, store, row.ID)
	if got.Status != repository.RecordingWebhookDeliveryPending || got.Attempts != 1 {
		t.Fatalf("cancelled delivery retained its claim: %+v", got)
	}
}

func TestDispatcherPanicUsesClaimedAttemptForRetryAndFailure(t *testing.T) {
	for _, maxAttempts := range []int{1, 3} {
		store := completedStore()
		store.settings = &repository.ServerSettings{RecordingWebhookEnabled: true, RecordingWebhookURL: "https://receiver.example/hook", RecordingWebhookSecret: "secret"}
		d := newTestDispatcher(store)
		d.attempts, d.backoff, d.maxBackoff = maxAttempts, time.Hour, time.Hour
		d.client = &http.Client{Transport: panicDeliveryTransport{}}
		row := enqueueTerminal(t, store, EventCompleted, store.video.ID, time.Now().Add(-time.Minute))
		d.drainDue(t.Context())
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		if err := d.work.WaitIdle(ctx); err != nil {
			cancel()
			t.Fatal(err)
		}
		cancel()
		d.work.Stop()
		got := deliverySnapshot(t, store, row.ID)
		want := repository.RecordingWebhookDeliveryPending
		if maxAttempts == 1 {
			want = repository.RecordingWebhookDeliveryFailed
		}
		if got.Attempts != 1 || got.Status != want || !strings.Contains(got.LastError, "delivery transport panic") {
			t.Fatalf("panic settlement: %+v", got)
		}
		if maxAttempts > 1 && time.Until(got.NextAttemptAt) < 59*time.Minute {
			t.Fatalf("retry ignored claimed attempt: %+v", got)
		}
	}
}
