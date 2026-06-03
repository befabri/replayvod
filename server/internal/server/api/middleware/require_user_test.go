package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/trpcgo"
)

// RequireUser is the unauthenticated->CodeUnauthorized gate backing every authed
// tRPC procedure (18 call sites), so its two outcomes are pinned directly.
func TestRequireUser(t *testing.T) {
	t.Run("no user in context -> CodeUnauthorized", func(t *testing.T) {
		got, err := RequireUser(context.Background())
		if got != nil {
			t.Fatalf("user = %v, want nil", got)
		}
		var te *trpcgo.Error
		if !errors.As(err, &te) {
			t.Fatalf("err = %T (%v), want *trpcgo.Error", err, err)
		}
		if te.Code != trpcgo.CodeUnauthorized {
			t.Fatalf("code = %v, want CodeUnauthorized", te.Code)
		}
	})

	t.Run("user present -> returned with no error", func(t *testing.T) {
		want := &repository.User{ID: "u1", Role: "owner"}
		got, err := RequireUser(WithUser(context.Background(), want))
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if got != want {
			t.Fatalf("user = %v, want %v", got, want)
		}
	})
}
