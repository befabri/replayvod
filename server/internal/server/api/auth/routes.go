package auth

import (
	"log/slog"

	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/trpcgo"
)

// RegisterTRPC wires the auth.* tRPC procedures. All require an
// authenticated session. The domain Service is passed in (rather than
// constructed here) because the Chi OAuth Handler needs the same
// instance — router.go constructs it once and shares.
func RegisterTRPC(tr *trpcgo.Router, svc *Service, sm *session.Manager, log *slog.Logger, authed *trpcgo.ProcedureBuilder) {
	h := NewTRPCHandler(svc, sm, log)
	trpcgo.MustVoidQuery(tr, "auth.session", h.Session, authed)
	trpcgo.MustVoidMutation(tr, "auth.logout", h.Logout, authed)
	trpcgo.MustVoidQuery(tr, "auth.sessions", h.ListSessions, authed)
	trpcgo.MustMutation(tr, "auth.revokeSession", h.RevokeSession, authed)
}
