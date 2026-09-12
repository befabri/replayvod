// Package recordinglock coordinates publication and deletion of recording
// artifacts within one server process. Share one Locks across the writers and
// deletion service. Expensive generation runs outside the lock; the final row
// check and object publication share a lock with purge and tombstoning.
package recordinglock

import (
	"context"
	"sync"
)

// Locks is a set of per-recording locks. Its zero value is ready to use, and
// entries are discarded after the last holder or waiter leaves.
type Locks struct {
	mu      sync.Mutex
	entries map[int64]*entry
}

type entry struct {
	held chan struct{}
	refs int
}

// Lock waits for exclusive ownership, respecting cancellation. The returned
// unlock function must be called exactly once after a successful acquisition.
func (l *Locks) Lock(ctx context.Context, videoID int64) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l.mu.Lock()
	if l.entries == nil {
		l.entries = make(map[int64]*entry)
	}
	e := l.entries[videoID]
	if e == nil {
		e = &entry{held: make(chan struct{}, 1)}
		l.entries[videoID] = e
	}
	e.refs++
	l.mu.Unlock()
	releaseRef := func() {
		l.mu.Lock()
		e.refs--
		if e.refs == 0 {
			delete(l.entries, videoID)
		}
		l.mu.Unlock()
	}
	select {
	case e.held <- struct{}{}:
		unlock := func() { <-e.held; releaseRef() }
		if err := ctx.Err(); err != nil {
			unlock()
			return nil, err
		}
		return unlock, nil
	case <-ctx.Done():
		releaseRef()
		return nil, ctx.Err()
	}
}
