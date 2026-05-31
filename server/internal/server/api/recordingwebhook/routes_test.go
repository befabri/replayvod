package recordingwebhook

import (
	"testing"

	svc "github.com/befabri/replayvod/server/internal/recordingwebhook"
)

func TestDispatcherOrNil_returnsTrueNilSenderForNilDispatcher(t *testing.T) {
	var d *svc.Dispatcher
	if got := dispatcherOrNil(d); got != nil {
		t.Fatalf("dispatcherOrNil(nil) = %T, want nil sender interface", got)
	}
}

func TestDispatcherOrNil_wrapsNonNilDispatcher(t *testing.T) {
	d := &svc.Dispatcher{}
	got := dispatcherOrNil(d)
	if got == nil {
		t.Fatal("dispatcherOrNil(non-nil) returned nil")
	}
	if got != d {
		t.Fatalf("dispatcherOrNil(non-nil) = %T, want original dispatcher", got)
	}
}
