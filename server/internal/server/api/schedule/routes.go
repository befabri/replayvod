package schedule

import (
	"log/slog"

	schedulesvc "github.com/befabri/replayvod/server/internal/service/schedule"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes registers schedule procedures with viewer reads and admin
// writes. Pass the same service used by the webhook processor to share its
// lifetime.
func RegisterRoutes(tr *trpcgo.Router, svc *schedulesvc.Service, log *slog.Logger, viewer, admin *trpcgo.ProcedureBuilder) {
	h := NewHandler(svc, log)
	trpcgo.MustQuery(tr, "schedule.list", h.List, viewer)
	trpcgo.MustQuery(tr, "schedule.mine", h.Mine, viewer)
	trpcgo.MustQuery(tr, "schedule.getById", h.GetByID, viewer)
	trpcgo.MustVoidQuery(tr, "schedule.pauseState", h.PauseState, viewer)
	trpcgo.MustMutation(tr, "schedule.create", h.Create, admin)
	trpcgo.MustMutation(tr, "schedule.update", h.Update, admin)
	trpcgo.MustMutation(tr, "schedule.toggle", h.Toggle, admin)
	trpcgo.MustMutation(tr, "schedule.setPaused", h.SetPaused, admin)
	trpcgo.MustMutation(tr, "schedule.delete", h.Delete, admin)

	trpcgo.MustMutation(tr, "schedule.createRequest", h.CreateRequest, viewer)
	trpcgo.MustQuery(tr, "schedule.myRequests", h.MyRequests, viewer)
	trpcgo.MustMutation(tr, "schedule.cancelRequest", h.CancelRequest, viewer)
	trpcgo.MustQuery(tr, "schedule.requests", h.Requests, admin)
	trpcgo.MustMutation(tr, "schedule.approveRequest", h.ApproveRequest, admin)
	trpcgo.MustMutation(tr, "schedule.rejectRequest", h.RejectRequest, admin)
}
