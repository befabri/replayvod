package sqliteadapter

import (
	"context"
	"fmt"

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

func (a *SQLiteAdapter) CreateInvite(ctx context.Context, input *repository.InviteInput) (*repository.Invite, error) {
	row, err := a.queries.CreateInvite(ctx, sqlitegen.CreateInviteParams{
		TokenHash: input.TokenHash,
		Role:      input.Role,
		Note:      toNullString(input.Note),
		CreatedBy: input.CreatedBy,
		ExpiresAt: sqliteTime(input.ExpiresAt),
	})
	if err != nil {
		return nil, fmt.Errorf("sqlite create invite: %w", err)
	}
	return sqliteInviteToDomain(row), nil
}
