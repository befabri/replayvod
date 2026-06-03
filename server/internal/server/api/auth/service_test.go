package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/befabri/replayvod/server/internal/repository"
)

// fakeRoleRepo satisfies repository.Repository by embedding the interface
// (every method panics if called) and overrides only ListUsers, the sole
// method resolveRole touches.
type fakeRoleRepo struct {
	repository.Repository
	users []repository.User
	err   error
}

func (f fakeRoleRepo) ListUsers(context.Context) ([]repository.User, error) {
	return f.users, f.err
}

// TestResolveRole pins the privilege-resolution truth table. A regression here
// is direct privilege escalation or owner lockout, so the policy is locked
// case-by-case.
func TestResolveRole(t *testing.T) {
	oneUser := []repository.User{{}}

	cases := []struct {
		name          string
		ownerTwitchID string
		twitchID      string
		users         []repository.User
		listErr       error
		want          string
	}{
		{"configured owner matches → owner", "owner-123", "owner-123", oneUser, nil, "owner"},
		{"configured owner, other user, users exist → viewer", "owner-123", "rando-456", oneUser, nil, "viewer"},
		{"no owner configured, first-ever user → owner", "", "first-1", nil, nil, "owner"},
		{"no owner configured, later user → viewer", "", "later-2", oneUser, nil, "viewer"},
		// Sharp edge worth locking: with an owner configured but not yet logged
		// in, a *different* first user still wins owner via the bootstrap. The
		// whitelist is the gate that normally prevents a stranger logging in
		// first; this test makes the behavior explicit so any change is deliberate.
		{"configured owner absent, other first user still bootstraps → owner", "owner-123", "rando-456", nil, nil, "owner"},
		// A ListUsers error must NOT be read as "no users" (which would hand out
		// owner); fall through to viewer.
		{"ListUsers error → viewer (not owner)", "", "x", nil, errors.New("db down"), "viewer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{
				repo: fakeRoleRepo{users: tc.users, err: tc.listErr},
				cfg:  Config{OwnerTwitchID: tc.ownerTwitchID},
				log:  slog.New(slog.NewTextHandler(io.Discard, nil)),
			}
			if got := s.resolveRole(context.Background(), tc.twitchID); got != tc.want {
				t.Fatalf("resolveRole(%q) = %q, want %q", tc.twitchID, got, tc.want)
			}
		})
	}
}
