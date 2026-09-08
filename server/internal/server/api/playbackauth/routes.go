package playbackauth

import (
	"log/slog"

	svc "github.com/befabri/replayvod/server/internal/playbackauth"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes mounts the Twitch playback connection procedures. All of them
// take the owner builder because the credential authorizes the shared recorder,
// and the credential itself is only ever accepted by a mutation.
func RegisterRoutes(tr *trpcgo.Router, service *svc.Service, publicOrigin string, log *slog.Logger, owner *trpcgo.ProcedureBuilder) {
	h := &Handler{svc: service, log: log.With("domain", "twitch-playback-api")}
	trpcgo.MustVoidQuery(tr, "twitchPlayback.status", h.Status, owner)
	trpcgo.MustMutation(tr, "twitchPlayback.connect", h.Connect, owner.Use(middleware.TRPCCredentialTransport(publicOrigin)))
	trpcgo.MustVoidMutation(tr, "twitchPlayback.check", h.Check, owner)
	trpcgo.MustVoidMutation(tr, "twitchPlayback.disconnect", h.Disconnect, owner)
}
