package pgadapter

import (
	"context"
	"fmt"

	"github.com/befabri/replayvod/server/internal/repository"
)

func (a *PGAdapter) EnsureSettings(ctx context.Context, userID string) (*repository.Settings, error) {
	if err := a.queries.EnsureSettings(ctx, userID); err != nil {
		return nil, fmt.Errorf("pg ensure settings: %w", mapErr(err))
	}
	return a.GetSettings(ctx, userID)
}
