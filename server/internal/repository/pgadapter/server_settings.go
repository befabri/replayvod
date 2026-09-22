package pgadapter

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (a *PGAdapter) GetServerHMACSecret(ctx context.Context) (string, error) {
	secret, err := a.queries.GetServerHMACSecret(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", mapErr(err)
	}
	return secret, nil
}
