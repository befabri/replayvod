// Package storageapi exposes storage readiness to the dashboard: the banner
// every user sees, the owner's System card, and the deliberate adopt action.
package storageapi

import (
	"context"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/scheduler"
	"github.com/befabri/replayvod/server/internal/server/api/apierr"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/replayvod/server/internal/service/storagehealth"
	"github.com/befabri/replayvod/server/internal/storage"
	"github.com/befabri/trpcgo"
)

// Monitor is the slice of the readiness monitor the API needs.
type Monitor interface {
	Status() storagehealth.Status
	Adopt(ctx context.Context, actorUserID string) (storagehealth.Status, error)
}

// TaskRunner schedules the storage scan right after an adoption.
type TaskRunner interface {
	ScheduleIfEnabled(ctx context.Context, name string) (bool, error)
}

// StorageState is the readiness verdict on the wire.
type StorageState string

const (
	StorageStateAttached    StorageState = StorageState(storagehealth.StateAttached)
	StorageStateReadOnly    StorageState = StorageState(storagehealth.StateReadOnly)
	StorageStateFull        StorageState = StorageState(storagehealth.StateFull)
	StorageStateUnattached  StorageState = StorageState(storagehealth.StateUnattached)
	StorageStateUnreachable StorageState = StorageState(storagehealth.StateUnreachable)
)

// StorageStatusResponse is what every signed-in user may see: enough for the banner.
type StorageStatusResponse struct {
	State     StorageState `json:"state"`
	CheckedAt time.Time    `json:"checked_at"`
}

// StorageDetailsResponse adds the owner-only facts: why, which backend, where, and
// which identity the database expects.
type StorageDetailsResponse struct {
	State     StorageState `json:"state"`
	Reason    string       `json:"reason"`
	Backend   string       `json:"backend"`
	Location  string       `json:"location"`
	StorageID string       `json:"storage_id"`
	CheckedAt time.Time    `json:"checked_at"`
}

// StorageScanStatus distinguishes an operator-disabled scan from a scheduling
// failure after adoption succeeded.
type StorageScanStatus string

const (
	StorageScanScheduled StorageScanStatus = "scheduled"
	StorageScanDisabled  StorageScanStatus = "disabled"
	StorageScanFailed    StorageScanStatus = "failed"
)

type StorageAdoptResponse struct {
	StorageDetailsResponse
	ScanStatus StorageScanStatus `json:"scan_status"`
}

type Handler struct {
	monitor Monitor
	tasks   TaskRunner
	log     *slog.Logger
}

func NewHandler(monitor Monitor, tasks TaskRunner, log *slog.Logger) *Handler {
	return &Handler{monitor: monitor, tasks: tasks, log: log.With("domain", "storage-api")}
}

func statusResponse(s storagehealth.Status) StorageStatusResponse {
	return StorageStatusResponse{State: StorageState(s.State), CheckedAt: s.CheckedAt}
}

func detailsResponse(s storagehealth.Status) StorageDetailsResponse {
	return StorageDetailsResponse{
		State:     StorageState(s.State),
		Reason:    s.Reason,
		Backend:   s.Backend,
		Location:  s.Location,
		StorageID: s.StorageID,
		CheckedAt: s.CheckedAt,
	}
}

func (h *Handler) Status(ctx context.Context) (StorageStatusResponse, error) {
	return statusResponse(h.monitor.Status()), nil
}

func (h *Handler) Details(ctx context.Context) (StorageDetailsResponse, error) {
	return detailsResponse(h.monitor.Status()), nil
}

// Adopt claims the reachable storage for this install and queues a scan if
// automatic scanning is enabled.
func (h *Handler) Adopt(ctx context.Context) (StorageAdoptResponse, error) {
	user, err := middleware.RequireUser(ctx)
	if err != nil {
		return StorageAdoptResponse{}, err
	}
	status, err := h.monitor.Adopt(ctx, user.ID)
	if err != nil {
		return StorageAdoptResponse{}, apierr.Map(h.log, err, "adopt storage",
			apierr.On(storage.ErrUnreachable, trpcgo.CodeServiceUnavailable,
				"storage is unreachable; mount it or fix its configuration first"),
			apierr.On(storage.ErrReadOnly, trpcgo.CodeConflict,
				"storage is read-only; the identity marker cannot be written"),
			apierr.On(storage.ErrFull, trpcgo.CodeConflict,
				"storage is full; free capacity before adopting it"),
			apierr.On(storage.ErrUnattached, trpcgo.CodeConflict,
				"storage still reads as another install's after writing the marker"))
	}
	resp := StorageAdoptResponse{StorageDetailsResponse: detailsResponse(status), ScanStatus: StorageScanDisabled}
	if h.tasks != nil {
		scheduled, err := h.tasks.ScheduleIfEnabled(ctx, scheduler.TaskStorageScan)
		if err != nil {
			h.log.Warn("schedule storage scan after adopt", "error", err)
			resp.ScanStatus = StorageScanFailed
		} else if scheduled {
			resp.ScanStatus = StorageScanScheduled
		}
	}
	return resp, nil
}
