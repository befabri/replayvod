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
