package video

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/befabri/trpcgo"
)

// A read handler that loses its client mid-flight (dashboard navigation cancels
// the request) must report tRPC's client-closed code, not a server fault. The
// router's error hook drops that code from the logs, so this is what keeps a
// cancelled getById/timeline from showing up as ERROR + 500.
func TestClientClosed(t *testing.T) {
	cancellations := []error{
		context.Canceled,
		context.DeadlineExceeded,
		fmt.Errorf("pg list video metadata changes: %w", context.Canceled),
	}
	for _, err := range cancellations {
		e := clientClosed(err)
		if e == nil {
			t.Fatalf("clientClosed(%v) = nil, want a client-closed error", err)
		}
		if e.Code != trpcgo.CodeClientClosed {
			t.Fatalf("clientClosed(%v) code = %v, want CodeClientClosed", err, e.Code)
		}
	}

	if e := clientClosed(errors.New("a real database failure")); e != nil {
		t.Fatalf("clientClosed(non-cancellation) = %v, want nil so the caller logs + 500s", e)
	}
}
