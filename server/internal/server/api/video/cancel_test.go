package video

import (
	"errors"
	"testing"
)

type refusedCancellation struct{ fakeDownloadRunner }

func (refusedCancellation) Cancel(string) error { return errors.New("database unavailable") }

func TestCancelDoesNotAcknowledgeUnpersistedStop(t *testing.T) {
	h := &Handler{download: &DownloadService{downloader: &refusedCancellation{}}, log: testClientLogger()}
	result, err := h.Cancel(t.Context(), CancelInput{JobID: "manual-recording"})
	if err == nil || result.OK {
		t.Fatalf("failed stop acknowledged: %+v %v", result, err)
	}
}
