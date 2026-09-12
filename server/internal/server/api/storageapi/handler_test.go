package storageapi

import (
	"context"
	"errors"
	"fmt"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/server/api/task"
	"github.com/befabri/replayvod/server/internal/testdb"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/scheduler"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/service/storagehealth"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/trpcgo"
)

type fakeMonitor struct {
	status   storagehealth.Status
	adoptErr error
	adopted  []string
}

func (m *fakeMonitor) Status() storagehealth.Status { return m.status }

func (m *fakeMonitor) Adopt(_ context.Context, actor string) (storagehealth.Status, error) {
	m.adopted = append(m.adopted, actor)
	if m.adoptErr != nil {
		return m.status, m.adoptErr
	}
	m.status.State = storagehealth.StateAttached
	m.status.Reason = ""
	return m.status, nil
}

type fakeTasks struct {
	err      error
	disabled bool
	names    []string
}

func (f *fakeTasks) ScheduleIfEnabled(_ context.Context, name string) (bool, error) {
	f.names = append(f.names, name)
	if f.err != nil {
		return false, f.err
	}
	return !f.disabled, nil
}

func newLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func unattachedStatus() storagehealth.Status {
	return storagehealth.Status{
		State: storagehealth.StateUnattached, Reason: "storage unattached: identity marker .replayvod-storage is missing",
		Backend: "local", Location: "/mnt/data", StorageID: "abc", CheckedAt: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC),
	}
}

func TestStatusHidesOwnerFacts(t *testing.T) {
	h := NewHandler(&fakeMonitor{status: unattachedStatus()}, nil, newLog())
	got, err := h.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StorageStateUnattached || got.CheckedAt != unattachedStatus().CheckedAt {
		t.Fatalf("status = %+v", got)
	}
	details, err := h.Details(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if details.Reason == "" || details.Location != "/mnt/data" || details.Backend != "local" || details.StorageID != "abc" {
		t.Fatalf("details = %+v", details)
	}
}

func TestAdoptRecordsActorAndSchedulesScan(t *testing.T) {
	mon := &fakeMonitor{status: unattachedStatus()}
	tasks := &fakeTasks{}
	h := NewHandler(mon, tasks, newLog())
	ctx := middleware.WithUser(context.Background(), &repository.User{ID: "owner-1"})
	got, err := h.Adopt(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StorageStateAttached || got.ScanStatus != StorageScanScheduled {
		t.Fatalf("adopt = %+v", got)
	}
	if len(mon.adopted) != 1 || mon.adopted[0] != "owner-1" {
		t.Fatalf("adopt actors = %v", mon.adopted)
	}
	if len(tasks.names) != 1 || tasks.names[0] != scheduler.TaskStorageScan {
		t.Fatalf("scheduled tasks = %v", tasks.names)
	}
}

func TestAdoptWithoutScanTaskStillSucceeds(t *testing.T) {
	mon := &fakeMonitor{status: unattachedStatus()}
	h := NewHandler(mon, &fakeTasks{disabled: true}, newLog())
	ctx := middleware.WithUser(context.Background(), &repository.User{ID: "owner-1"})
	got, err := h.Adopt(ctx)
	if err != nil || got.ScanStatus != StorageScanDisabled {
		t.Fatalf("adopt = %+v, %v; want success without a scheduled scan", got, err)
	}
}

func TestAdoptRequiresAUser(t *testing.T) {
	h := NewHandler(&fakeMonitor{status: unattachedStatus()}, nil, newLog())
	if _, err := h.Adopt(context.Background()); err == nil {
		t.Fatal("adopt without a user succeeded")
	}
}

func TestAdoptMapsStorageErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code trpcgo.ErrorCode
	}{
		{name: "unreachable", err: fmt.Errorf("%w: stat /mnt/data", storage.ErrUnreachable), code: trpcgo.CodeServiceUnavailable},
		{name: "read-only", err: fmt.Errorf("%w: write probe", storage.ErrReadOnly), code: trpcgo.CodeConflict},
		{name: "full", err: fmt.Errorf("%w: write marker", storage.ErrFull), code: trpcgo.CodeConflict},
		{name: "still foreign", err: fmt.Errorf("%w: other install", storage.ErrUnattached), code: trpcgo.CodeConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tasks := &fakeTasks{}
			h := NewHandler(&fakeMonitor{status: unattachedStatus(), adoptErr: tc.err}, tasks, newLog())
			ctx := middleware.WithUser(context.Background(), &repository.User{ID: "owner-1"})
			_, err := h.Adopt(ctx)
			var terr *trpcgo.Error
			if !errors.As(err, &terr) || terr.Code != tc.code {
				t.Fatalf("adopt err = %v, want code %d", err, tc.code)
			}
			if len(tasks.names) != 0 {
				t.Fatal("a failed adopt scheduled the scan")
			}
		})
	}
}

func TestAdoptSchedulesOnlyEnabledTasksUsingRealService(t *testing.T) {
	for _, tc := range []struct {
		name     string
		enabled  bool
		interval int64
		want     StorageScanStatus
	}{
		{"enabled", true, 60, StorageScanScheduled},
		{"disabled", false, 60, StorageScanDisabled},
		{"zero interval", true, 0, StorageScanDisabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
			ctx := middleware.WithUser(t.Context(), &repository.User{ID: "owner"})
			if _, err := repo.UpsertTask(ctx, scheduler.TaskStorageScan, "scan", tc.interval); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.SetTaskEnabled(ctx, scheduler.TaskStorageScan, tc.enabled); err != nil {
				t.Fatal(err)
			}
			before, err := repo.GetTask(ctx, scheduler.TaskStorageScan)
			if err != nil {
				t.Fatal(err)
			}
			mon := &fakeMonitor{status: unattachedStatus()}
			h := NewHandler(mon, task.New(repo, newLog()), newLog())
			got, err := h.Adopt(ctx)
			if err != nil || got.ScanStatus != tc.want {
				t.Fatalf("adopt: %+v %v", got, err)
			}
			after, err := repo.GetTask(ctx, scheduler.TaskStorageScan)
			if err != nil {
				t.Fatal(err)
			}
			if after.IsEnabled != tc.enabled {
				t.Fatal("adoption changed task enablement")
			}
			if tc.want == StorageScanDisabled && !reflect.DeepEqual(before, after) {
				t.Fatalf("disabled task mutated: %+v -> %+v", before, after)
			}
		})
	}
}

func TestAdoptDistinguishesScanSchedulingFailure(t *testing.T) {
	h := NewHandler(&fakeMonitor{status: unattachedStatus()}, &fakeTasks{err: errors.New("database unavailable")}, newLog())
	got, err := h.Adopt(middleware.WithUser(t.Context(), &repository.User{ID: "owner"}))
	if err != nil || got.State != StorageStateAttached || got.ScanStatus != StorageScanFailed {
		t.Fatalf("adoption result: %+v %v", got, err)
	}
}
