package storagehealth

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/storage"
)

type fullProbe struct{ *storage.LocalStorage }

func (s fullProbe) ProbeWrite(context.Context) error {
	return &os.PathError{Op: "create", Path: s.Root, Err: syscall.ENOSPC}
}

func TestFullStorageRetainsIdentityAndRecovers(t *testing.T) {
	f := newFixture(t)
	id, err := storage.NewStorageID()
	if err != nil {
		t.Fatal(err)
	}
	f.writeMarker(t, id)
	f.mon.store = fullProbe{f.store}
	events := f.bus.StorageStatus.Subscribe(t.Context())
	st, err := f.mon.Attach(f.ctx)
	if !errors.Is(err, storage.ErrFull) || st.State != StateFull || !st.Readable() || f.storedID(t) != id {
		t.Fatalf("full attach: %+v %v", st, err)
	}
	if !storage.CanDelete(f.mon.Ready()) || f.mon.Ready() == nil {
		t.Fatalf("full storage must permit cleanup and refuse writes: %v", f.mon.Ready())
	}
	if ev := receiveProbe(t, events); ev.State != string(StateFull) || !ev.At.Equal(st.CheckedAt) {
		t.Fatalf("full transition: %+v", ev)
	}
	if n := f.eventCount(t, EventFull); n != 1 {
		t.Fatalf("full audit count=%d", n)
	}
	waitProbeIdle(t, f.mon)
	if st, err = f.mon.Adopt(f.ctx, ""); err != nil || st.State != StateFull {
		t.Fatalf("adoption of matching full volume: %+v %v", st, err)
	}
	waitProbeIdle(t, f.mon)
	v := *f.mon.cache.Load()
	v.expires = time.Now().Add(-time.Second)
	f.mon.cache.Store(&v)
	if f.mon.Status().Readable() || storage.CanDelete(f.mon.Ready()) {
		t.Fatal("expired full verdict still authorizes access")
	}
	f.mon.store = f.store
	if st = f.mon.Check(f.ctx); st.State != StateAttached || f.mon.Ready() != nil {
		t.Fatalf("capacity recovery: %+v", st)
	}
	if ev := receiveProbe(t, events); ev.State != string(StateAttached) {
		t.Fatalf("recovery event: %+v", ev)
	}
}
