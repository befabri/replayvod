package recordinglock

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLocksExcludeSameRecordingAndReleaseEntries(t *testing.T) {
	var locks Locks
	var wg sync.WaitGroup
	count := 0 // protected only by the recording lock; also exercise under -race
	for range 4 {
		wg.Go(func() {
			for range 100 {
				unlock, err := locks.Lock(t.Context(), 1)
				if err != nil {
					t.Error(err)
					return
				}
				count++
				unlock()
			}
		})
	}
	wg.Wait()
	if count != 400 || len(locks.entries) != 0 {
		t.Fatalf("count=%d retained locks=%d", count, len(locks.entries))
	}
}

func TestLocksAllowOtherRecordingsAndCancelableWaits(t *testing.T) {
	var locks Locks
	first, err := locks.Lock(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	second, err := locks.Lock(ctx, 2)
	if err != nil {
		first()
		t.Fatalf("unrelated recording blocked: %v", err)
	}
	second()
	if unlock, err := locks.Lock(ctx, 1); !errors.Is(err, context.DeadlineExceeded) {
		if unlock != nil {
			unlock()
		}
		t.Errorf("waiting lock ignored cancellation: %v", err)
	}
	first()
	if len(locks.entries) != 0 {
		t.Fatalf("cancelled wait retained %d lock entries", len(locks.entries))
	}
}
