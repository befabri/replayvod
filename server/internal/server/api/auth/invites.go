package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/befabri/replayvod/server/internal/invite"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
)

// redeemInvite atomically claims the token, updates the account, and grants
// whitelist access.
func (s *Service) redeemInvite(ctx context.Context, raw string, profile *repository.User) (*repository.User, error) {
	tokenHash := invite.HashToken(raw)
	var inv *repository.Invite
	var user *repository.User
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		// Claim before reading to serialize redemption and acquire
		// SQLite's write lock before its read snapshot.
		ok, err := tx.RedeemInvite(ctx, tokenHash, profile.ID)
		if err != nil {
			return err
		}
		if !ok {
			return &ErrLoginDenied{Reason: "invite_invalid"}
		}
		inv, err = tx.GetInviteByTokenHash(ctx, tokenHash)
		if err != nil {
			return err
		}
		// PostgreSQL NOW() uses transaction start time; lock waits can
		// outlive the invite.
		if !inv.ExpiresAt.After(time.Now()) {
			return &ErrLoginDenied{Reason: "invite_invalid"}
		}
		if inv.CreatedBy == profile.ID {
			return &ErrLoginDenied{Reason: "invite_self"}
		}
		user, err = s.upsertOAuthUser(ctx, tx, profile)
		if err != nil {
			return err
		}
		// Use the upsert's locked role so a concurrent owner promotion
		// cannot be overwritten.
		if !middleware.HasMinRole(user.Role, inv.Role) {
			if err := tx.UpdateUserRole(ctx, user.ID, inv.Role); err != nil {
				return fmt.Errorf("apply invite role: %w", err)
			}
			user.Role = inv.Role
		}
		if err := tx.AddToWhitelist(ctx, user.ID); err != nil {
			return fmt.Errorf("whitelist invited user: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("redeem invite: %w", err)
	}
	s.log.Info("invite redeemed", "invite_id", inv.ID, "twitch_id", user.ID, "role", user.Role)
	return user, nil
}
