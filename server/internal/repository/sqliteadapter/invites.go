package sqliteadapter

import (
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/repository/sqliteadapter/sqlitegen"
)

func sqliteInviteToDomain(row sqlitegen.Invite) *repository.Invite {
	return &repository.Invite{
		ID:         row.ID,
		TokenHash:  row.TokenHash,
		Role:       row.Role,
		Note:       fromNullString(row.Note),
		CreatedBy:  row.CreatedBy,
		ExpiresAt:  row.ExpiresAt.Time,
		RedeemedAt: timePtrFromSQLite(row.RedeemedAt),
		RedeemedBy: fromNullString(row.RedeemedBy),
		CreatedAt:  row.CreatedAt.Time,
	}
}
