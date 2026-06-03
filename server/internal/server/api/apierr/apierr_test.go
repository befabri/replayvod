package apierr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

func codeOf(t *testing.T, err error) trpcgo.ErrorCode {
	t.Helper()
	var te *trpcgo.Error
	if !errors.As(err, &te) {
		t.Fatalf("error %v is not a *trpcgo.Error", err)
	}
	return te.Code
}

func msgOf(t *testing.T, err error) string {
	t.Helper()
	var te *trpcgo.Error
	if !errors.As(err, &te) {
		t.Fatalf("error %v is not a *trpcgo.Error", err)
	}
	return te.Message
}

func TestMap_BuiltinChain(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	if got := Map(log, nil, "do thing"); got != nil {
		t.Fatalf("nil error mapped to %v, want nil", got)
	}

	cases := []struct {
		name string
		err  error
		want trpcgo.ErrorCode
	}{
		{"canceled", context.Canceled, trpcgo.CodeClientClosed},
		{"wrapped cancel", fmt.Errorf("query: %w", context.Canceled), trpcgo.CodeClientClosed},
		{"deadline", context.DeadlineExceeded, trpcgo.CodeTimeout},
		{"not found", repository.ErrNotFound, trpcgo.CodeNotFound},
		{"wrapped not found", fmt.Errorf("load: %w", repository.ErrNotFound), trpcgo.CodeNotFound},
		{"other", errors.New("boom"), trpcgo.CodeInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := codeOf(t, Map(log, tc.err, "do thing")); got != tc.want {
				t.Fatalf("code = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMap_Rules(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	errNotOwner := errors.New("not your thing")
	errInvalid := errors.New("filter must be non-empty")

	t.Run("rule maps a domain sentinel before the builtin chain", func(t *testing.T) {
		err := Map(log, errNotOwner, "update thing",
			On(errNotOwner, trpcgo.CodeForbidden, "not your thing"))
		if got := codeOf(t, err); got != trpcgo.CodeForbidden {
			t.Fatalf("code = %v, want Forbidden", got)
		}
		if got := msgOf(t, err); got != "not your thing" {
			t.Fatalf("message = %q, want overridden message", got)
		}
	})

	t.Run("rule overrides a builtin default (ErrNotFound message)", func(t *testing.T) {
		err := Map(log, repository.ErrNotFound, "get schedule",
			On(repository.ErrNotFound, trpcgo.CodeNotFound, "schedule not found"))
		if got := codeOf(t, err); got != trpcgo.CodeNotFound {
			t.Fatalf("code = %v, want NotFound", got)
		}
		if got := msgOf(t, err); got != "schedule not found" {
			t.Fatalf("message = %q, want specialized message", got)
		}
	})

	t.Run("OnVerbatim surfaces err.Error()", func(t *testing.T) {
		err := Map(log, errInvalid, "create thing",
			OnVerbatim(errInvalid, trpcgo.CodeBadRequest))
		if got := codeOf(t, err); got != trpcgo.CodeBadRequest {
			t.Fatalf("code = %v, want BadRequest", got)
		}
		if got := msgOf(t, err); got != "filter must be non-empty" {
			t.Fatalf("message = %q, want verbatim err text", got)
		}
	})

	t.Run("On with no message uses the code's generic default", func(t *testing.T) {
		err := Map(log, errNotOwner, "x", On(errNotOwner, trpcgo.CodeForbidden))
		if got := msgOf(t, err); got != "forbidden" {
			t.Fatalf("message = %q, want generic 'forbidden'", got)
		}
	})

	t.Run("unmatched rule falls through to the builtin chain", func(t *testing.T) {
		err := Map(log, context.Canceled, "x", On(errNotOwner, trpcgo.CodeForbidden))
		if got := codeOf(t, err); got != trpcgo.CodeClientClosed {
			t.Fatalf("code = %v, want ClientClosed", got)
		}
	})
}
