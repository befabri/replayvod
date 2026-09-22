package pgadapter

import (
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
