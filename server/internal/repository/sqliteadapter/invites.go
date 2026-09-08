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

func (a *SQLiteAdapter) RedeemInvite(ctx context.Context, tokenHash, redeemedBy string) (bool, error) {
	affected, err := a.queries.RedeemInvite(ctx, sqlitegen.RedeemInviteParams{
		RedeemedBy: toNullString(&redeemedBy),
		TokenHash:  tokenHash,
	})
	if err != nil {
		return false, fmt.Errorf("sqlite redeem invite: %w", err)
	}
	return affected > 0, nil
}

func (a *SQLiteAdapter) ListInvites(ctx context.Context) ([]repository.Invite, error) {
	rows, err := a.queries.ListInvites(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlite list invites: %w", err)
	}
	invites := make([]repository.Invite, len(rows))
	for i, row := range rows {
		invites[i] = *sqliteInviteToDomain(row)
	}
	return invites, nil
}

func (a *SQLiteAdapter) DeleteInvite(ctx context.Context, id int64) (bool, error) {
	affected, err := a.queries.DeleteInvite(ctx, id)
	if err != nil {
		return false, fmt.Errorf("sqlite delete invite %d: %w", id, err)
	}
	return affected > 0, nil
}

func (a *SQLiteAdapter) RotateInviteToken(ctx context.Context, id int64, tokenHash string) (*repository.Invite, error) {
	row, err := a.queries.RotateInviteToken(ctx, sqlitegen.RotateInviteTokenParams{TokenHash: tokenHash, ID: id})
	if err != nil {
		return nil, mapErr(err)
	}
	return sqliteInviteToDomain(row), nil
}
