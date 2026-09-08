package system

import (
	"context"
	"time"

	"github.com/befabri/replayvod/server/internal/invite"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/apierr"
	"github.com/befabri/replayvod/server/internal/server/api/middleware"
	"github.com/befabri/trpcgo"
)

type CreateInviteInput struct {
	Role       string  `json:"role" validate:"required,oneof=viewer admin"`
	TTLMinutes int     `json:"ttl_minutes" validate:"required,min=5,max=43200"`
	Note       *string `json:"note,omitempty" validate:"omitempty,max=60"`
}

// InviteCreatedInfo includes the redemption URL, which cannot be retrieved
// again.
type InviteCreatedInfo struct {
	ID        int64           `json:"id"`
	URL       string          `json:"url"`
	Role      middleware.Role `json:"role"`
	ExpiresAt time.Time       `json:"expires_at"`
}

func (h *Handler) CreateInvite(ctx context.Context, input CreateInviteInput) (InviteCreatedInfo, error) {
	caller, err := middleware.RequireUser(ctx)
	if err != nil {
		return InviteCreatedInfo{}, err
	}
	raw, inv, err := h.invites.Create(ctx, caller.ID, input.Role, time.Duration(input.TTLMinutes)*time.Minute, input.Note)
	if err != nil {
		return InviteCreatedInfo{}, apierr.Map(h.log, err, "create invite")
	}
	return InviteCreatedInfo{
		ID:        inv.ID,
		URL:       h.invites.URL(raw),
		Role:      middleware.Role(inv.Role),
		ExpiresAt: inv.ExpiresAt,
	}, nil
}

type InviteInfo struct {
	ID         int64           `json:"id"`
	Role       middleware.Role `json:"role"`
	Note       *string         `json:"note,omitempty"`
	CreatedBy  string          `json:"created_by"`
	ExpiresAt  time.Time       `json:"expires_at"`
	RedeemedAt *time.Time      `json:"redeemed_at,omitempty"`
	RedeemedBy *string         `json:"redeemed_by,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

func toInviteInfo(inv *repository.Invite) InviteInfo {
	return InviteInfo{
		ID:         inv.ID,
		Role:       middleware.Role(inv.Role),
		Note:       inv.Note,
		CreatedBy:  inv.CreatedBy,
		ExpiresAt:  inv.ExpiresAt,
		RedeemedAt: inv.RedeemedAt,
		RedeemedBy: inv.RedeemedBy,
		CreatedAt:  inv.CreatedAt,
	}
}

func (h *Handler) ListInvites(ctx context.Context) ([]InviteInfo, error) {
	invites, err := h.invites.List(ctx)
	if err != nil {
		return nil, apierr.Map(h.log, err, "list invites")
	}
	out := make([]InviteInfo, len(invites))
	for i := range invites {
		out[i] = toInviteInfo(&invites[i])
	}
	return out, nil
}

type RevokeInviteInput struct {
	ID int64 `json:"id" validate:"required"`
}

func (h *Handler) RevokeInvite(ctx context.Context, input RevokeInviteInput) (OK, error) {
	if err := h.invites.Revoke(ctx, input.ID); err != nil {
		return OK{}, apierr.Map(h.log, err, "revoke invite",
			apierr.On(invite.ErrNotFound, trpcgo.CodeNotFound, "invite not found"))
	}
	return OK{OK: true}, nil
}

type RotateInviteInput struct {
	ID int64 `json:"id" validate:"required"`
}

// RotateInvite issues a fresh redemption URL for a pending invitation; the
// previous link stops working.
func (h *Handler) RotateInvite(ctx context.Context, input RotateInviteInput) (InviteCreatedInfo, error) {
	raw, inv, err := h.invites.Rotate(ctx, input.ID)
	if err != nil {
		return InviteCreatedInfo{}, apierr.Map(h.log, err, "rotate invite",
			apierr.On(invite.ErrNotFound, trpcgo.CodeNotFound, "invite not found"))
	}
	return InviteCreatedInfo{
		ID:        inv.ID,
		URL:       h.invites.URL(raw),
		Role:      middleware.Role(inv.Role),
		ExpiresAt: inv.ExpiresAt,
	}, nil
}
