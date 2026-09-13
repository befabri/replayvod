package video

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

type relatedRepo struct {
	repository.Repository
	intent    *repository.RecordingIntent
	rows      []repository.RelatedRecording
	intentErr error
	rowsErr   error
}

func (r relatedRepo) GetVideo(context.Context, int64) (*repository.Video, error) {
	return &repository.Video{ID: 78, JobID: "job-78"}, nil
}
func (r relatedRepo) GetRecordingIntentByJob(context.Context, string) (*repository.RecordingIntent, error) {
	if r.intentErr != nil {
		return nil, r.intentErr
	}
	if r.intent == nil {
		return nil, repository.ErrNotFound
	}
	return r.intent, nil
}
func (r relatedRepo) ListRelatedRecordings(context.Context, int64) ([]repository.RelatedRecording, error) {
	return r.rows, r.rowsErr
}

func TestRelatedRecordingsReportsReadFailures(t *testing.T) {
	for _, repo := range []relatedRepo{
		{intentErr: errors.New("intent database unavailable")},
		{intent: &repository.RecordingIntent{ID: "intent"}, rowsErr: errors.New("members database unavailable")},
	} {
		h := &Handler{video: &Service{repo: repo}, log: testClientLogger()}
		_, err := h.RelatedRecordings(t.Context(), GetByIDInput{ID: 78})
		var rpcErr *trpcgo.Error
		if !errors.As(err, &rpcErr) || rpcErr.Code != trpcgo.CodeInternalServerError {
			t.Fatalf("read failure = %v", err)
		}
	}
}

func TestRelatedRecordingsWireState(t *testing.T) {
	until := time.Now().UTC().Add(time.Minute)
	repo := relatedRepo{
		intent: &repository.RecordingIntent{ID: "manual", Status: repository.RecordingIntentStatusWaiting, WaitUntil: &until},
		rows:   []repository.RelatedRecording{{ID: 78, JobID: "job-78", Position: 1, Status: repository.VideoStatusFailed, CompletionKind: repository.CompletionKindCancelled}},
	}
	h := &Handler{video: &Service{repo: repo}, log: testClientLogger()}
	out, err := h.RelatedRecordings(t.Context(), GetByIDInput{ID: 78})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != RecordingIntentStatusWaiting || out.WaitUntil == nil || !out.WaitUntil.Equal(until) {
		t.Fatalf("intent state lost: %+v", out)
	}
	if len(out.Items) != 1 || out.Items[0].Status != VideoStatusFailed || out.Items[0].CompletionKind != CompletionKindCancelled {
		t.Fatalf("cancelled outcome lost: %+v", out.Items)
	}
}

func TestUnrelatedRecordingOmitsIntentState(t *testing.T) {
	h := &Handler{video: &Service{repo: relatedRepo{}}, log: testClientLogger()}
	out, err := h.RelatedRecordings(t.Context(), GetByIDInput{ID: 78})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"items":[]}` {
		t.Fatalf("unexpected empty relationship: %s", body)
	}
}
