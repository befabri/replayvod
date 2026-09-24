package auth

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/invite"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// loginReadHookRepo changes the stored role after a read to exercise stale
// authorization deterministically.
type loginReadHookRepo struct {
	repository.Repository
	afterRead func(context.Context, repository.Repository) error
}

func (r loginReadHookRepo) GetUser(ctx context.Context, id string) (*repository.User, error) {
	u, err := r.Repository.GetUser(ctx, id)
	if hookErr := r.afterRead(ctx, r.Repository); hookErr != nil {
		return nil, hookErr
	}
	return u, err
}

func (r loginReadHookRepo) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		return fn(loginReadHookRepo{Repository: tx, afterRead: r.afterRead})
	})
}

func TestHandleOAuthCallback_OrdinaryLoginPreservesConcurrentRoleChange(t *testing.T) {
	for _, tc := range []struct {
		name   string
		before string
		after  string
	}{
		{"demotion", "admin", "viewer"},
		{"promotion", "viewer", "owner"},
		{"concurrent first login", "", "admin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
			user := &repository.User{ID: "twitch-1", Login: "streamer", DisplayName: "Old Name", Role: tc.before}
			if tc.before != "" {
				if _, err := repo.UpsertUser(ctx, user); err != nil {
					t.Fatal(err)
				}
			}
			hooked := loginReadHookRepo{Repository: repo, afterRead: func(ctx context.Context, repo repository.Repository) error {
				if tc.before == "" {
					user.Role = tc.after
					_, err := repo.UpsertUser(ctx, user)
					return err
				}
				return repo.UpdateUserRole(ctx, user.ID, tc.after)
			}}
			svc := New(hooked, nil, newStubbedTwitch(t, stubUserJSON), Config{}, discardLog())
			result, err := svc.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", "")
			if err != nil {
				t.Fatal(err)
			}
			stored, err := repo.GetUser(ctx, user.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Role != tc.after || result.User.Role != tc.after {
				t.Errorf("login overwrote the committed role: stored=%q, returned=%q, want %q", stored.Role, result.User.Role, tc.after)
			}
			if stored.DisplayName != "Streamer" {
				t.Errorf("login did not refresh the profile: %+v", stored)
			}
		})
	}
}

func TestHandleOAuthCallback_InviteUsesRoleReturnedByUpsert(t *testing.T) {
	for _, tc := range []struct {
		before string
		after  string
		invite string
	}{
		{"viewer", "owner", "admin"},
		{"admin", "viewer", "viewer"},
	} {
		t.Run(tc.before+" to "+tc.after, func(t *testing.T) {
			ctx := context.Background()
			repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
			if _, err := repo.UpsertUser(ctx, &repository.User{ID: "twitch-1", Login: "streamer", DisplayName: "Streamer", Role: tc.before}); err != nil {
				t.Fatal(err)
			}
			raw := seedInvite(t, repo, tc.invite, time.Hour)
			hooked := loginReadHookRepo{Repository: repo, afterRead: func(ctx context.Context, repo repository.Repository) error {
				return repo.UpdateUserRole(ctx, "twitch-1", tc.after)
			}}
			svc := New(hooked, nil, newStubbedTwitch(t, stubUserJSON), Config{}, discardLog())
			result, err := svc.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", raw)
			if err != nil {
				t.Fatal(err)
			}
			stored, err := repo.GetUser(ctx, "twitch-1")
			if err != nil || stored.Role != tc.after || result.User.Role != tc.after {
				t.Errorf("invite used a stale role: stored=%+v, returned=%+v, err=%v; want %s", stored, result.User, err, tc.after)
			}
		})
	}
}

// upsertFailureRepo fails user saves while fail is set, including inside
// transactions.
type upsertFailureRepo struct {
	repository.Repository
	fail *atomic.Bool
}

func (r upsertFailureRepo) UpsertUser(ctx context.Context, u *repository.User) (*repository.User, error) {
	if r.fail.Load() {
		return nil, errors.New("user save unavailable")
	}
	return r.Repository.UpsertUser(ctx, u)
}

func (r upsertFailureRepo) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		return fn(upsertFailureRepo{Repository: tx, fail: r.fail})
	})
}

func TestHandleOAuthCallback_InviteSignupFailureRollsBackAndCanRetry(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "new user"
		if existing {
			name = "existing user promotion"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
			raw := seedInvite(t, repo, "admin", time.Hour)
			if existing {
				if _, err := repo.UpsertUser(ctx, &repository.User{ID: "twitch-1", Login: "old-login", DisplayName: "Old Name", Role: "viewer"}); err != nil {
					t.Fatal(err)
				}
			}
			var fail atomic.Bool
			fail.Store(true)
			svc := New(upsertFailureRepo{Repository: repo, fail: &fail}, nil, newStubbedTwitch(t, stubUserJSON), Config{WhitelistEnabled: true}, discardLog())
			if result, err := svc.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", raw); err == nil || result != nil {
				t.Fatalf("failed signup = (%+v, %v), want failure", result, err)
			}
			inv, err := repo.GetInviteByTokenHash(ctx, invite.HashToken(raw))
			if err != nil {
				t.Fatal(err)
			}
			if inv.RedeemedAt != nil || inv.RedeemedBy != nil {
				t.Errorf("failed signup consumed the invite: %+v", inv)
			}
			if allowed, err := repo.IsWhitelisted(ctx, "twitch-1"); err != nil || allowed {
				t.Errorf("failed signup granted whitelist access: %v, %v", allowed, err)
			}
			stored, err := repo.GetUser(ctx, "twitch-1")
			if existing {
				if err != nil || stored.Role != "viewer" || stored.Login != "old-login" {
					t.Errorf("failed signup changed existing user: %+v, %v", stored, err)
				}
			} else if !errors.Is(err, repository.ErrNotFound) {
				t.Errorf("failed signup left a user: %+v, %v", stored, err)
			}
			fail.Store(false)
			result, err := svc.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", raw)
			if err != nil {
				t.Fatalf("retry with the same invite: %v", err)
			}
			if result.User.Role != "admin" {
				t.Errorf("retry role = %q, want admin", result.User.Role)
			}
			if allowed, err := repo.IsWhitelisted(ctx, "twitch-1"); err != nil || !allowed {
				t.Errorf("successful retry did not whitelist the user: %v, %v", allowed, err)
			}
			inv, err = repo.GetInviteByTokenHash(ctx, invite.HashToken(raw))
			if err != nil || inv.RedeemedAt == nil || inv.RedeemedBy == nil || *inv.RedeemedBy != "twitch-1" {
				t.Errorf("successful retry did not redeem the invite: %+v, %v", inv, err)
			}
		})
	}
}

// A Twitch rename frees the old login for someone else, while the renamed
// account's row keeps holding it until that account signs in again. The
// login's new owner must still be able to sign in.
func TestHandleOAuthCallback_LoginFreedByTwitchRenameCanSignIn(t *testing.T) {
	for _, withInvite := range []bool{false, true} {
		name := "ordinary login"
		if withInvite {
			name = "invited signup"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
			if _, err := repo.UpsertUser(ctx, &repository.User{ID: "renamed-account", Login: "streamer", DisplayName: "Previous", Role: "admin"}); err != nil {
				t.Fatal(err)
			}
			raw := ""
			cfg := Config{}
			if withInvite {
				raw = seedInvite(t, repo, "viewer", time.Hour)
				cfg.WhitelistEnabled = true
			}
			svc := New(repo, nil, newStubbedTwitch(t, stubUserJSON), cfg, discardLog())
			result, err := svc.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", raw)
			if err != nil {
				t.Fatalf("sign-in with a login a rename freed: %v", err)
			}
			if result.User.ID != "twitch-1" || result.User.Login != "streamer" {
				t.Errorf("signed-in user = %+v, want twitch-1 holding streamer", result.User)
			}
			stored, err := repo.GetUser(ctx, "renamed-account")
			if err != nil || stored.Login != "~renamed-account" || stored.Role != "admin" || stored.DisplayName != "Previous" {
				t.Fatalf("renamed account = %+v, %v; want only its login parked", stored, err)
			}

			renamed := New(repo, nil, newStubbedTwitch(t, `{"data":[{"id":"renamed-account","login":"new-name","display_name":"New Name"}]}`), Config{}, discardLog())
			if _, err := renamed.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", ""); err != nil {
				t.Fatalf("renamed account signs in again: %v", err)
			}
			stored, err = repo.GetUser(ctx, "renamed-account")
			if err != nil || stored.Login != "new-name" || stored.Role != "admin" {
				t.Fatalf("renamed account after its sign-in = %+v, %v; want its current login back", stored, err)
			}
		})
	}
}
