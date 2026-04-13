package schedule

import (
	"log/slog"

	schedulesvc "github.com/befabri/replayvod/server/internal/service/schedule"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes wires schedule.* tRPC procedures. Reads are viewer-
// level (owners see all); writes are admin-only so viewers can't burn
// Twitch quota auto-downloading. The domain service is passed in
// rather than constructed here so the webhook processor (separately
// constructed in router.go) uses the same lifetime.
func RegisterRoutes(tr *trpcgo.Router, svc *schedulesvc.Service, log *slog.Logger, viewer, admin *trpcgo.ProcedureBuilder) {
	h := NewHandler(svc, log)
	trpcgo.MustQuery(tr, "schedule.list", h.List, viewer)
	trpcgo.MustQuery(tr, "schedule.mine", h.Mine, viewer)
	trpcgo.MustQuery(tr, "schedule.getById", h.GetByID, viewer)
	trpcgo.MustMutation(tr, "schedule.create", h.Create, admin)
	trpcgo.MustMutation(tr, "schedule.update", h.Update, admin)
	trpcgo.MustMutation(tr, "schedule.toggle", h.Toggle, admin)
	trpcgo.MustMutation(tr, "schedule.delete", h.Delete, admin)
}
