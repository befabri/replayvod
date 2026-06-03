// Package apierr centralizes the service-error -> tRPC-code mapping that every
// handler would otherwise hand-roll. One canonical mapper keeps the common
// behaviors consistent across domains:
//
//   - a client that navigates away mid-request (context.Canceled) is reported
//     as CodeClientClosed, not logged as a 500 (trpcgo's WithOnError hook in
//     router.go skips logging that code);
//   - a request/upstream deadline (context.DeadlineExceeded) is CodeTimeout,
//     distinct from the client closing the connection;
//   - a missing row (repository.ErrNotFound) is CodeNotFound;
//   - anything else is logged and surfaced as a generic 500.
//
// Domain-specific sentinels (forbidden, bad-request, etc.) are supplied per
// call via On/OnVerbatim rules, which are matched before the built-in chain so
// a handler can map its own errors or specialize a message without re-rolling
// the whole ladder.
package apierr

import (
	"context"
	"errors"
	"log/slog"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// Rule maps a domain error sentinel to a tRPC code. Build it with On/OnVerbatim.
type Rule struct {
	target   error
	code     trpcgo.ErrorCode
	message  string
	verbatim bool
}

// On maps any error satisfying errors.Is(err, target) to code. An optional
// message overrides the code's generic default, e.g.
// On(repository.ErrNotFound, trpcgo.CodeNotFound, "schedule not found").
func On(target error, code trpcgo.ErrorCode, message ...string) Rule {
	r := Rule{target: target, code: code}
	if len(message) > 0 {
		r.message = message[0]
	}
	return r
}

// OnVerbatim maps target to code and surfaces err.Error() as the client
// message — for service errors that already carry an operator-facing string
// (e.g. a validation error whose text is meant to be shown).
func OnVerbatim(target error, code trpcgo.ErrorCode) Rule {
	return Rule{target: target, code: code, verbatim: true}
}

// Map converts a service error into the right tRPC error. op is a short action
// phrase ("list channels") used for the generic 500 message and the log line.
// Caller rules are matched first (so a handler can specialize a message or map
// a domain sentinel), then the built-in cancel/deadline/not-found/500 chain.
// Returns nil when err is nil, so a handler can `return Map(...)` directly.
func Map(log *slog.Logger, err error, op string, rules ...Rule) error {
	if err == nil {
		return nil
	}
	for _, r := range rules {
		if errors.Is(err, r.target) {
			switch {
			case r.verbatim:
				return trpcgo.NewError(r.code, err.Error())
			case r.message != "":
				return trpcgo.NewError(r.code, r.message)
			default:
				return trpcgo.NewError(r.code, defaultMessage(r.code))
			}
		}
	}
	switch {
	case errors.Is(err, context.Canceled):
		// Client navigated away mid-request: a client outcome, not a server
		// fault. router.go's WithOnError hook suppresses logging for
		// CodeClientClosed, so this stays out of the error log.
		return trpcgo.NewError(trpcgo.CodeClientClosed, "request canceled")
	case errors.Is(err, context.DeadlineExceeded):
		// A request/upstream deadline expired: a real timeout, distinct from the
		// client hanging up. CodeTimeout is deliberately NOT in WithOnError's
		// suppress list (unlike CodeClientClosed), so a deadline IS logged: a
		// timeout signals server/upstream slowness worth surfacing. The
		// dashboard's navigate-away storm is context.Canceled (handled above and
		// kept silent), so this only fires on genuine timeouts.
		return trpcgo.NewError(trpcgo.CodeTimeout, "request timed out")
	case errors.Is(err, repository.ErrNotFound):
		return trpcgo.NewError(trpcgo.CodeNotFound, "not found")
	default:
		log.Error(op, "error", err)
		return trpcgo.NewError(trpcgo.CodeInternalServerError, "failed to "+op)
	}
}

func defaultMessage(code trpcgo.ErrorCode) string {
	switch code {
	case trpcgo.CodeNotFound:
		return "not found"
	case trpcgo.CodeForbidden:
		return "forbidden"
	case trpcgo.CodeBadRequest:
		return "bad request"
	case trpcgo.CodeUnauthorized:
		return "not authenticated"
	default:
		return trpcgo.NameFromCode(code)
	}
}
