package settings

import (
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// RegisterRoutes exposes settings procedures scoped to the authenticated user.
func RegisterRoutes(tr *trpcgo.Router, repo repository.Repository, log *slog.Logger, viewer *trpcgo.ProcedureBuilder) {
	h := NewHandler(New(repo, log), log)
	trpcgo.MustVoidQuery(tr, "settings.get", h.Get, viewer)
	trpcgo.MustMutation(tr, "settings.update", h.Update, viewer)
	trpcgo.MustMutation(tr, "settings.updatePlayback", h.UpdatePlayback, viewer)
}
