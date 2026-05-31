package recordingwebhook

import (
	"log/slog"

	svc "github.com/befabri/replayvod/server/internal/recordingwebhook"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes wires recordingWebhook.* tRPC procedures. Owner-only — the
// config carries a signing secret and a server-side egress URL. dispatch is the
// live dispatcher backing the test-send and delivery-history procedures; it may
// be nil (those procedures then degrade gracefully).
func RegisterRoutes(tr *trpcgo.Router, repo repository.Repository, dispatch *svc.Dispatcher, log *slog.Logger, owner *trpcgo.ProcedureBuilder) {
	h := NewHandler(svc.New(repo, log), dispatcherOrNil(dispatch), log)
	trpcgo.MustVoidQuery(tr, "recordingWebhook.config", h.Config, owner)
	trpcgo.MustMutation(tr, "recordingWebhook.updateConfig", h.UpdateConfig, owner)
	trpcgo.MustVoidMutation(tr, "recordingWebhook.regenerateSecret", h.RegenerateSecret, owner)
	trpcgo.MustVoidMutation(tr, "recordingWebhook.testDelivery", h.TestDelivery, owner)
	trpcgo.MustVoidQuery(tr, "recordingWebhook.deliveries", h.Deliveries, owner)
	trpcgo.MustMutation(tr, "recordingWebhook.retryDelivery", h.RetryDelivery, owner)
}

// dispatcherOrNil adapts a possibly-nil *svc.Dispatcher to the sender interface
// without wrapping a nil pointer in a non-nil interface (which would defeat the
// handler's nil check).
func dispatcherOrNil(d *svc.Dispatcher) sender {
	if d == nil {
		return nil
	}
	return d
}
