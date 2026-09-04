package auth

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/invite"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter"
	"github.com/befabri/replayvod/server/internal/testdb"
)

// inviteWriteFailureRepo fails a late write inside the real transaction to
// expose incomplete rollback.
type inviteWriteFailureRepo struct {
	repository.Repository
	stage string
	err   error
}

func (r inviteWriteFailureRepo) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		return fn(inviteWriteFailureRepo{Repository: tx, stage: r.stage, err: r.err})
	})
}

func (r inviteWriteFailureRepo) UpdateUserRole(ctx context.Context, id, role string) error {
	if r.stage == "promotion" {
		return r.err
	}
	return r.Repository.UpdateUserRole(ctx, id, role)
}

func (r inviteWriteFailureRepo) AddToWhitelist(ctx context.Context, id string) error {
	if r.stage == "whitelist" {
		return r.err
	}
	return r.Repository.AddToWhitelist(ctx, id)
}

func TestHandleOAuthCallback_LateInviteFailureRollsBackAndCanRetry(t *testing.T) {
	for _, stage := range []string{"promotion", "whitelist"} {
		for _, existing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/existing=%t", stage, existing), func(t *testing.T) {
				ctx := context.Background()
				repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
				raw := seedInvite(t, repo, "admin", time.Hour)
				if existing {
					if _, err := repo.UpsertUser(ctx, &repository.User{ID: "twitch-1", Login: "old-login", DisplayName: "Old Name", Role: "viewer"}); err != nil {
						t.Fatal(err)
					}
					if err := repo.AddToWhitelist(ctx, "twitch-1"); err != nil {
						t.Fatal(err)
					}
				}
				wantErr := errors.New("late invite write failed")
				svc := New(inviteWriteFailureRepo{Repository: repo, stage: stage, err: wantErr}, nil, newStubbedTwitch(t, stubUserJSON), Config{WhitelistEnabled: true}, discardLog())
				result, err := svc.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", raw)
				if !errors.Is(err, wantErr) || result != nil {
					t.Fatalf("login = (%+v, %v), want the write error and no result", result, err)
				}
				inv, err := repo.GetInviteByTokenHash(ctx, invite.HashToken(raw))
				if err != nil || inv.RedeemedAt != nil || inv.RedeemedBy != nil {
					t.Errorf("failed login consumed the invite: %+v, %v", inv, err)
				}
				if allowed, err := repo.IsWhitelisted(ctx, "twitch-1"); err != nil || allowed != existing {
					t.Errorf("failed login changed whitelist access: %v, %v", allowed, err)
				}
				stored, err := repo.GetUser(ctx, "twitch-1")
				if existing {
					if err != nil || stored.Role != "viewer" || stored.Login != "old-login" || stored.DisplayName != "Old Name" {
						t.Errorf("failed login changed the account: %+v, %v", stored, err)
					}
				} else if !errors.Is(err, repository.ErrNotFound) {
					t.Errorf("failed login left a user behind: %+v, %v", stored, err)
				}
				svc = New(repo, nil, newStubbedTwitch(t, stubUserJSON), Config{WhitelistEnabled: true}, discardLog())
				result, err = svc.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", raw)
				if err != nil || result.User.Role != "admin" {
					t.Fatalf("retry = (%+v, %v), want a successful admin login", result, err)
				}
			})
		}
	}
}

func TestHandleOAuthCallback_ConcurrentInviteRedemption(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
	raw := seedInvite(t, repo, "admin", time.Hour)
	type outcome struct {
		id     string
		result *LoginResult
		err    error
	}
	const contenders = 8
	start := make(chan struct{})
	results := make(chan outcome, contenders)
	for i := range contenders {
		id := fmt.Sprintf("redeemer-%d", i)
		usersJSON := fmt.Sprintf(`{"data":[{"id":%q,"login":%q,"display_name":"Redeemer"}]}`, id, id)
		svc := New(repo, nil, newStubbedTwitch(t, usersJSON), Config{WhitelistEnabled: true}, discardLog())
		go func() {
			<-start
			result, err := svc.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", raw)
			results <- outcome{id, result, err}
		}()
	}
	close(start)
	winner := ""
	for range contenders {
		r := <-results
		if r.err == nil {
			if winner != "" {
				t.Errorf("multiple successful logins: %s and %s", winner, r.id)
			}
			winner = r.id
			if r.result.User.Role != "admin" {
				t.Errorf("winner role = %q, want admin", r.result.User.Role)
			}
		} else {
			var denied *ErrLoginDenied
			if !errors.As(r.err, &denied) || denied.Reason != "invite_invalid" || r.result != nil {
				t.Errorf("losing login = (%+v, %v), want invite_invalid", r.result, r.err)
			}
			if user, err := repo.GetUser(ctx, r.id); !errors.Is(err, repository.ErrNotFound) {
				t.Errorf("losing login left a user: %+v, %v", user, err)
			}
		}
		if allowed, err := repo.IsWhitelisted(ctx, r.id); err != nil || allowed != (r.err == nil) {
			t.Errorf("whitelist for %s = %v, %v", r.id, allowed, err)
		}
	}
	inv, err := repo.GetInviteByTokenHash(ctx, invite.HashToken(raw))
	if err != nil || winner == "" || inv.RedeemedBy == nil || *inv.RedeemedBy != winner {
		t.Fatalf("winner=%q, invite=%+v, err=%v; want matching redemption audit", winner, inv, err)
	}
}

func TestHandleOAuthCallback_ConcurrentInvitesNeverLowerRole(t *testing.T) {
	for _, initialRole := range []string{"viewer", "owner"} {
		t.Run(initialRole, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			repo := sqliteadapter.New(testdb.NewSQLiteDB(t))
			if _, err := repo.UpsertUser(ctx, &repository.User{ID: "twitch-1", Login: "streamer", DisplayName: "Streamer", Role: initialRole}); err != nil {
				t.Fatal(err)
			}
			tokens := []string{seedInvite(t, repo, "viewer", time.Hour), seedInvite(t, repo, "admin", time.Hour)}
			svc := New(repo, nil, newStubbedTwitch(t, stubUserJSON), Config{}, discardLog())
			start := make(chan struct{})
			results := make(chan error, len(tokens))
			for _, raw := range tokens {
				go func() {
					<-start
					_, err := svc.HandleOAuthCallback(ctx, "code", "https://app/callback", "verifier", raw)
					results <- err
				}()
			}
			close(start)
			for range tokens {
				if err := <-results; err != nil {
					t.Errorf("concurrent invite login: %v", err)
				}
			}
			wantRole := "admin"
			if initialRole == "owner" {
				wantRole = "owner"
			}
			stored, err := repo.GetUser(ctx, "twitch-1")
			if err != nil || stored.Role != wantRole {
				t.Fatalf("role after concurrent invites = %+v, %v; want %s", stored, err, wantRole)
			}
			for _, raw := range tokens {
				inv, err := repo.GetInviteByTokenHash(ctx, invite.HashToken(raw))
				if err != nil || inv.RedeemedBy == nil || *inv.RedeemedBy != stored.ID {
					t.Errorf("invite not consumed for the successful login: %+v, %v", inv, err)
				}
			}
		})
	}
}
