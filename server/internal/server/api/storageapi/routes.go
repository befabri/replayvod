package storageapi

import (
	"log/slog"

	"github.com/befabri/trpcgo"
)

// RegisterRoutes wires the storage readiness procedures. The status is
// viewer-level so the banner can tell everyone why nothing plays or records;
// the details and the adopt action are owner-only.
func RegisterRoutes(tr *trpcgo.Router, monitor Monitor, tasks TaskRunner, log *slog.Logger, viewer, owner *trpcgo.ProcedureBuilder) {
	h := NewHandler(monitor, tasks, log)
	trpcgo.MustVoidQuery(tr, "storage.status", h.Status, viewer)
	trpcgo.MustVoidQuery(tr, "storage.details", h.Details, owner)
	trpcgo.MustVoidMutation(tr, "storage.adopt", h.Adopt, owner)
}
