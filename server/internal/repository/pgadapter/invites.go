package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/pgadapter/pggen"
)

func pgInviteToDomain(row pggen.Invite) *repository.Invite {
	return &repository.Invite{
		ID:         row.ID,
		TokenHash:  row.TokenHash,
		Role:       row.Role,
		Note:       row.Note,
		CreatedBy:  row.CreatedBy,
		ExpiresAt:  row.ExpiresAt,
		RedeemedAt: row.RedeemedAt,
		RedeemedBy: row.RedeemedBy,
		CreatedAt:  row.CreatedAt,
	}
}

func (a *PGAdapter) CreateInvite(ctx context.Context, input *repository.InviteInput) (*repository.Invite, error) {
	row, err := a.queries.CreateInvite(ctx, pggen.CreateInviteParams{
		TokenHash: input.TokenHash,
		Role:      input.Role,
		Note:      input.Note,
		CreatedBy: input.CreatedBy,
		ExpiresAt: input.ExpiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("pg create invite: %w", err)
	}
	return pgInviteToDomain(row), nil
}

func (a *PGAdapter) RedeemInvite(ctx context.Context, tokenHash, redeemedBy string) (bool, error) {
	affected, err := a.queries.RedeemInvite(ctx, pggen.RedeemInviteParams{
		TokenHash:  tokenHash,
		RedeemedBy: &redeemedBy,
	})
	if err != nil {
		return false, fmt.Errorf("pg redeem invite: %w", err)
	}
	return affected > 0, nil
}

func (a *PGAdapter) ListInvites(ctx context.Context) ([]repository.Invite, error) {
	rows, err := a.queries.ListInvites(ctx)
	if err != nil {
		return nil, fmt.Errorf("pg list invites: %w", err)
	}
	invites := make([]repository.Invite, len(rows))
	for i, row := range rows {
		invites[i] = *pgInviteToDomain(row)
	}
	return invites, nil
}

func (a *PGAdapter) DeleteInvite(ctx context.Context, id int64) (bool, error) {
	affected, err := a.queries.DeleteInvite(ctx, id)
	if err != nil {
		return false, fmt.Errorf("pg delete invite %d: %w", id, err)
	}
	return affected > 0, nil
}

func (a *PGAdapter) RotateInviteToken(ctx context.Context, id int64, tokenHash string) (*repository.Invite, error) {
	row, err := a.queries.RotateInviteToken(ctx, pggen.RotateInviteTokenParams{ID: id, TokenHash: tokenHash})
	if err != nil {
		return nil, mapErr(err)
	}
	return pgInviteToDomain(row), nil
}
